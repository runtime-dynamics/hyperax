package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConsolidationEngine merges old episodic memories into semantic summaries
// and manages capacity-based eviction.
//
// Consolidation groups episodic memories by scope and workspace, summarises
// them into a single semantic memory, and marks the originals as consolidated.
// The originals remain in the database (soft-deleted via consolidated_into)
// and can be hard-deleted after a configurable retention period.
type ConsolidationEngine struct {
	repo   repo.MemoryRepo
	bus    *nervous.EventBus
	logger *slog.Logger
	config ConsolidationConfig
}

// NewConsolidationEngine creates a ConsolidationEngine.
func NewConsolidationEngine(memRepo repo.MemoryRepo, bus *nervous.EventBus, logger *slog.Logger, cfg ConsolidationConfig) *ConsolidationEngine {
	if cfg.OlderThanDays == 0 {
		cfg.OlderThanDays = 30
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.ProjectCapacity == 0 {
		cfg.ProjectCapacity = 10000
	}
	if cfg.GlobalCapacity == 0 {
		cfg.GlobalCapacity = 50000
	}
	return &ConsolidationEngine{
		repo:   memRepo,
		bus:    bus,
		logger: logger,
		config: cfg,
	}
}

// Run executes a consolidation pass. If olderThanDays is 0, uses the config default.
// If dryRun is true, returns the candidate count without modifying anything.
// Returns the number of memories consolidated.
func (e *ConsolidationEngine) Run(ctx context.Context, scope types.MemoryScope, olderThanDays int, dryRun bool) (int, error) {
	if olderThanDays <= 0 {
		olderThanDays = e.config.OlderThanDays
	}

	candidates, err := e.repo.ListConsolidationCandidates(ctx, scope, olderThanDays, e.config.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("memory.ConsolidationEngine.Run: %w", err)
	}

	if len(candidates) == 0 {
		return 0, nil
	}

	if dryRun {
		return len(candidates), nil
	}

	// Group candidates by workspace for scoped consolidation.
	groups := groupByWorkspace(candidates)
	total := 0

	for wsID, group := range groups {
		consolidated, err := e.consolidateGroup(ctx, scope, wsID, group)
		if err != nil {
			e.logger.Error("consolidation failed",
				"scope", scope,
				"workspace_id", wsID,
				"error", err,
			)
			continue
		}
		total += consolidated
	}

	// Publish consolidation event.
	if e.bus != nil && total > 0 {
		e.bus.Publish(nervous.NewEvent(
			types.EventMemoryConsolidate,
			"memory.consolidation",
			string(scope),
			map[string]any{
				"consolidated_count": total,
				"scope":              string(scope),
				"older_than_days":    olderThanDays,
			},
		))
	}

	return total, nil
}

// consolidateGroup merges a group of episodic memories into a single semantic memory.
func (e *ConsolidationEngine) consolidateGroup(ctx context.Context, scope types.MemoryScope, workspaceID string, memories []*types.Memory) (int, error) {
	if len(memories) == 0 {
		return 0, nil
	}

	// Build a consolidated summary from the episodic content.
	// In a future iteration, this will use an LLM for intelligent summarisation.
	// For now, we use a rule-based concatenation with deduplication.
	summary := summariseMemories(memories)

	// Collect source IDs for marking.
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
	}

	// Store the consolidated semantic memory.
	consolidated := &types.Memory{
		Scope:       scope,
		Type:        types.MemoryTypeSemantic,
		Content:     summary,
		WorkspaceID: workspaceID,
		Metadata: map[string]any{
			"source":           "consolidation",
			"source_count":     len(memories),
			"consolidation_of": ids,
		},
	}

	// Preserve persona_id if all source memories share the same persona.
	if allSamePersona(memories) {
		consolidated.PersonaID = memories[0].PersonaID
	}

	targetID, err := e.repo.Store(ctx, consolidated)
	if err != nil {
		return 0, fmt.Errorf("memory.ConsolidationEngine.consolidateGroup: %w", err)
	}

	// Mark originals as consolidated.
	if err := e.repo.MarkConsolidated(ctx, ids, targetID); err != nil {
		return 0, fmt.Errorf("memory.ConsolidationEngine.consolidateGroup: %w", err)
	}

	e.logger.Info("memories consolidated",
		"target_id", targetID,
		"source_count", len(memories),
		"scope", scope,
		"workspace_id", workspaceID,
	)

	return len(memories), nil
}

// summariseMemories creates a rule-based summary of episodic memories.
// Future: this will be replaced with LLM-assisted summarisation.
func summariseMemories(memories []*types.Memory) string {
	if len(memories) == 1 {
		return memories[0].Content
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Consolidated from %d episodic memories:\n", len(memories))

	// Deduplicate content.
	seen := make(map[string]bool)
	for _, m := range memories {
		content := strings.TrimSpace(m.Content)
		if content == "" || seen[content] {
			continue
		}
		seen[content] = true
		sb.WriteString("- ")
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// groupByWorkspace groups memories by their workspace ID.
func groupByWorkspace(memories []*types.Memory) map[string][]*types.Memory {
	groups := make(map[string][]*types.Memory)
	for _, m := range memories {
		key := m.WorkspaceID
		if key == "" {
			key = "_global"
		}
		groups[key] = append(groups[key], m)
	}
	return groups
}

// allSamePersona returns true if all memories share the same non-empty persona ID.
func allSamePersona(memories []*types.Memory) bool {
	if len(memories) == 0 {
		return false
	}
	first := memories[0].PersonaID
	if first == "" {
		return false
	}
	for _, m := range memories[1:] {
		if m.PersonaID != first {
			return false
		}
	}
	return true
}
