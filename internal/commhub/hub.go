package commhub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// defaultInboxCapacity is the maximum number of messages buffered per agent inbox.
const defaultInboxCapacity = 1000

// recallTimeout is the maximum latency budget for proactive memory recall
// during message dispatch. If recall exceeds this, the message is delivered
// immediately and a follow-up MemoryContext envelope is sent asynchronously.
const recallTimeout = 50 * time.Millisecond

// AgentInbox holds pending messages for an agent.
// Messages are stored in a slice protected by a mutex. When the inbox reaches
// its capacity limit, the oldest message is dropped (backpressure).
type AgentInbox struct {
	AgentID  string
	Messages []*types.MessageEnvelope
	mu       sync.Mutex
	maxSize  int
}

// RecallFunc is the function signature for memory recall operations.
// It takes a context, query string, workspace ID, and persona ID, and returns
// matching memory contexts. This abstraction decouples CommHub from the
// concrete MemoryStore implementation.
type RecallFunc func(ctx context.Context, query string, workspaceID, personaID string) ([]types.MemoryContext, error)

// AgentResolver looks up the workspace and agent identity for a given agent ID.
// Returns workspaceID, agentID, and any error.
type AgentResolver func(ctx context.Context, agentID string) (workspaceID, resolvedAgentID string, err error)

// OverflowPersister persists a dropped message to the database for later retrieval.
// This is injected by the application wiring layer to decouple CommHub from the repo.
type OverflowPersister func(ctx context.Context, entry *types.OverflowEntry) error

// OverflowDrainer retrieves persisted overflow messages for an agent.
type OverflowDrainer func(ctx context.Context, agentID string, limit int) ([]*types.OverflowEntry, error)

// HintFunc is the function signature for contextual hint injection.
// It takes a context, query text, and workspace ID, returning relevant hints
// as JSON-serialisable data. This abstraction decouples CommHub from the
// concrete hints.Engine implementation.
type HintFunc func(ctx context.Context, query, workspaceID string) ([]map[string]any, error)

// RehydrationFunc is the function signature for triggering agent rehydration.
// It is injected by the application wiring layer to decouple CommHub from the
// lifecycle package and avoid circular imports. The function should transition
// the agent through the rehydration sequence and return a result summary.
type RehydrationFunc func(ctx context.Context, agentID string) (map[string]any, error)

// ZombieDetector checks whether an agent is in a zombie state (no recent
// heartbeat, not actively managed). It is injected by the application layer
// to enable pre-delivery rehydration without coupling CommHub to the lifecycle repo.
type ZombieDetector func(ctx context.Context, agentID string) bool

// CommHub routes messages between agents with trust-level enforcement.
// All messages pass through the Context Sieve before delivery. The hub
// maintains per-agent inboxes and publishes delivery events on the Nervous System.
// When a RecallFunc is configured, the hub performs proactive memory recall
// during dispatch, attaching related memories to the envelope metadata.
// When a HintFunc is configured, contextual tool hints are injected into
// the envelope metadata alongside memory context.
// When a RehydrationFunc is configured along with a ZombieDetector, the hub
// triggers agent rehydration before delivering messages to zombie agents.
type CommHub struct {
	sieve             *ContextSieve
	bus               *nervous.EventBus
	logger            *slog.Logger
	inboxes           map[string]*AgentInbox
	mu                sync.RWMutex
	recallFn          RecallFunc
	agentResolver     AgentResolver
	overflowPersist   OverflowPersister
	overflowDrain     OverflowDrainer
	onboardFn         OnboardFunc
	hintFn            HintFunc
	rehydrateFn       RehydrationFunc
	zombieDetector    ZombieDetector
}

// NewCommHub creates a new communication hub wired to the Nervous System event bus.
func NewCommHub(bus *nervous.EventBus, logger *slog.Logger) *CommHub {
	return &CommHub{
		sieve:   NewContextSieve(bus),
		bus:     bus,
		logger:  logger,
		inboxes: make(map[string]*AgentInbox),
	}
}

// SetRecallFunc configures proactive memory recall during message dispatch.
// When set, the hub queries the recall function with message content scoped
// to the target agent's persona and workspace. Results are attached to the
// envelope's Metadata["related_memories"] field as a JSON-encoded array.
func (h *CommHub) SetRecallFunc(fn RecallFunc) {
	h.recallFn = fn
}

// SetAgentResolver configures the function used to resolve an agent's
// workspace and identity for scoped memory recall.
func (h *CommHub) SetAgentResolver(fn AgentResolver) {
	h.agentResolver = fn
}

// SetOverflowPersister configures the function used to persist messages that
// are dropped due to inbox backpressure. When set, dropped messages are written
// to the database instead of being silently discarded.
func (h *CommHub) SetOverflowPersister(fn OverflowPersister) {
	h.overflowPersist = fn
}

// SetOverflowDrainer configures the function used to retrieve persisted overflow
// messages. Used by DrainOverflow to pull back messages when inbox space is available.
func (h *CommHub) SetOverflowDrainer(fn OverflowDrainer) {
	h.overflowDrain = fn
}

// SetHintFunc configures contextual tool hint injection during message dispatch.
// When set, the hub queries relevant hints based on message content and attaches
// them to the envelope's Metadata["tool_hints"] field as a JSON-encoded array.
// Hints are fetched with a short latency budget and never block delivery.
func (h *CommHub) SetHintFunc(fn HintFunc) {
	h.hintFn = fn
}

// SetRehydrationFunc configures the function used to trigger full agent rehydration.
// When set alongside SetZombieDetector, the hub checks whether the target agent
// is in a zombie state before message delivery and triggers rehydration if needed.
func (h *CommHub) SetRehydrationFunc(fn RehydrationFunc) {
	h.rehydrateFn = fn
}

// SetZombieDetector configures the function used to detect zombie agents.
// A zombie agent has no recent heartbeat and needs rehydration before
// it can process messages.
func (h *CommHub) SetZombieDetector(fn ZombieDetector) {
	h.zombieDetector = fn
}

// BuildRecallFunc creates a RecallFunc from a MemoryRepo and the retrieval config.
// This is the standard factory used by the application wiring layer.
// The logger parameter is used for non-critical secondary recall failures
// (project/global scope) which are logged but do not fail the overall recall.
func BuildRecallFunc(memRepo repo.MemoryRepo, logger *slog.Logger) RecallFunc {
	if memRepo == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, query, workspaceID, personaID string) ([]types.MemoryContext, error) {
		mq := types.MemoryQuery{
			Query:       query,
			WorkspaceID: workspaceID,
			PersonaID:   personaID,
			MaxResults:  5,
		}
		memories, err := memRepo.Recall(ctx, mq.Query, types.MemoryScopePersona, workspaceID, personaID, 3)
		if err != nil {
			return nil, fmt.Errorf("commhub.BuildRecallFunc: %w", err)
		}
		// Also fetch project and global scope.
		projectMems, projectErr := memRepo.Recall(ctx, mq.Query, types.MemoryScopeProject, workspaceID, "", 5)
		if projectErr != nil {
			logger.Error("failed to recall project memories", "workspace", workspaceID, "error", projectErr)
		}
		globalMems, globalErr := memRepo.Recall(ctx, mq.Query, types.MemoryScopeGlobal, "", "", 2)
		if globalErr != nil {
			logger.Error("failed to recall global memories", "error", globalErr)
		}

		var results []types.MemoryContext
		for i, m := range memories {
			results = append(results, types.MemoryContext{
				Memory: *m,
				Score:  1.0 / float64(i+1),
				Rank:   i + 1,
				Source: "proactive",
			})
		}
		for i, m := range projectMems {
			results = append(results, types.MemoryContext{
				Memory: *m,
				Score:  0.5 / float64(i+1),
				Rank:   len(memories) + i + 1,
				Source: "proactive",
			})
		}
		for i, m := range globalMems {
			results = append(results, types.MemoryContext{
				Memory: *m,
				Score:  0.3 / float64(i+1),
				Rank:   len(memories) + len(projectMems) + i + 1,
				Source: "proactive",
			})
		}
		return results, nil
	}
}

// Send routes a message through the Context Sieve and delivers it to the recipient's inbox.
// If a RecallFunc is configured, proactive memory recall is performed with a 50ms latency
// budget. If recall completes in time, results are attached to the envelope metadata.
// If recall exceeds the budget, the message is delivered immediately and a follow-up
// MemoryContext envelope is sent asynchronously.
// Returns an error if the sieve rejects the message.
func (h *CommHub) Send(ctx context.Context, env *types.MessageEnvelope) error {
	ctx, span := otel.Tracer("commhub").Start(ctx, "CommHub.Send")
	defer span.End()

	span.SetAttributes(
		attribute.String("commhub.from", env.From),
		attribute.String("commhub.to", env.To),
		attribute.String("commhub.content_type", env.ContentType),
		attribute.String("commhub.trust", env.Trust.String()),
	)

	if env.Timestamp == 0 {
		env.Timestamp = time.Now().UnixMilli()
	}

	// Compute and track trust lineage: max(parent trust_lineage, envelope trust level).
	// This ensures that data originating from external sources is tracked even when
	// forwarded by internal agents.
	if env.Metadata == nil {
		env.Metadata = make(map[string]string)
	}
	parentLineage := types.TrustLevel(0)
	if raw, ok := env.Metadata["trust_lineage"]; ok {
		if v, err := strconv.Atoi(raw); err == nil {
			parentLineage = types.TrustLevel(v)
		}
	}
	lineage := env.Trust
	if parentLineage > lineage {
		lineage = parentLineage
	}
	env.Metadata["trust_lineage"] = strconv.Itoa(int(lineage))

	// Recursive sifting: Internal messages with trust lineage >= TrustExternal
	// get a lightweight sieve pass (Pattern Filter + Metadata Stripping only).
	var (
		sanitized *types.MessageEnvelope
		err       error
	)
	if env.Trust == types.TrustInternal && lineage >= types.TrustExternal {
		h.logger.Debug("applying recursive lightweight sieve",
			"from", env.From,
			"to", env.To,
			"trust_lineage", lineage,
		)
		sanitized, err = h.sieve.ProcessLightweight(env)
	} else {
		sanitized, err = h.sieve.Process(env)
	}
	if err != nil {
		return fmt.Errorf("commhub.Hub.Send: %w", err)
	}

	// Proactive memory recall: query memories relevant to the message content
	// scoped to the target agent's persona and workspace.
	h.performRecall(ctx, sanitized)

	// Contextual tool hint injection: attach relevant hints to the envelope.
	h.injectHints(ctx, sanitized)

	// Pre-delivery zombie check: if the target agent has no recent heartbeat,
	// trigger rehydration before delivering the message. This ensures the
	// agent is in an active state and can process the incoming message.
	h.rehydrateIfZombie(ctx, sanitized.To)

	// Deliver to recipient inbox.
	h.deliver(sanitized)

	// Publish delivery event on the Nervous System.
	h.bus.Publish(nervous.NewEvent(
		types.EventCommMessage,
		"commhub",
		sanitized.To,
		map[string]interface{}{
			"from":         sanitized.From,
			"to":           sanitized.To,
			"trust":        sanitized.Trust.String(),
			"content_type": sanitized.ContentType,
		},
	))

	h.logger.Debug("message delivered",
		"from", sanitized.From,
		"to", sanitized.To,
		"trust", sanitized.Trust.String(),
	)

	return nil
}

// performRecall runs proactive memory recall with a latency budget.
// If recall completes within recallTimeout, results are attached to the envelope
// metadata as JSON. If recall times out, a follow-up MemoryContext envelope is
// delivered asynchronously. If recall is not configured or the message content
// is empty, this is a no-op.
func (h *CommHub) performRecall(ctx context.Context, env *types.MessageEnvelope) {
	if h.recallFn == nil || env.Content == "" {
		return
	}

	// Resolve the target agent's workspace and identity for scoped recall.
	var workspaceID, agentID string
	if h.agentResolver != nil {
		var err error
		workspaceID, agentID, err = h.agentResolver(ctx, env.To)
		if err != nil {
			h.logger.Debug("agent resolver failed, using unscoped recall",
				"agent", env.To,
				"error", err,
			)
		}
	}

	// Run recall with latency budget.
	recallCtx, cancel := context.WithTimeout(ctx, recallTimeout)
	defer cancel()

	type recallResult struct {
		memories []types.MemoryContext
		err      error
	}
	ch := make(chan recallResult, 1)

	go func() {
		memories, err := h.recallFn(recallCtx, env.Content, workspaceID, agentID)
		ch <- recallResult{memories: memories, err: err}
	}()

	select {
	case result := <-ch:
		if result.err != nil {
			h.logger.Debug("proactive recall failed",
				"to", env.To,
				"error", result.err,
			)
			return
		}
		h.attachMemories(env, result.memories)

	case <-recallCtx.Done():
		// Recall exceeded the latency budget. Deliver the original message
		// immediately and send a follow-up with memory context asynchronously.
		h.logger.Debug("proactive recall exceeded latency budget, sending follow-up",
			"to", env.To,
			"timeout", recallTimeout,
		)
		go h.sendMemoryFollowUp(ctx, env, workspaceID, agentID)
	}
}

// attachMemories serialises recalled memories as JSON and attaches them
// to the envelope's Metadata under the "related_memories" key.
func (h *CommHub) attachMemories(env *types.MessageEnvelope, memories []types.MemoryContext) {
	if len(memories) == 0 {
		return
	}

	if env.Metadata == nil {
		env.Metadata = make(map[string]string)
	}

	data, err := json.Marshal(memories)
	if err != nil {
		h.logger.Warn("failed to marshal recall memories", "error", err)
		return
	}
	env.Metadata["related_memories"] = string(data)

	h.logger.Debug("proactive recall attached memories",
		"to", env.To,
		"count", len(memories),
	)
}

// hintTimeout is the maximum latency budget for tool hint injection.
// If hints exceed this, the message is delivered without hints.
const hintTimeout = 30 * time.Millisecond

// injectHints queries the configured HintFunc and attaches relevant tool hints
// to the envelope's Metadata["tool_hints"] field. This runs with a tight latency
// budget and never blocks message delivery.
func (h *CommHub) injectHints(ctx context.Context, env *types.MessageEnvelope) {
	if h.hintFn == nil || env.Content == "" {
		return
	}

	// Resolve workspace for scoped hints.
	var workspaceID string
	if h.agentResolver != nil {
		wsID, _, err := h.agentResolver(ctx, env.To)
		if err == nil {
			workspaceID = wsID
		}
	}

	hintCtx, cancel := context.WithTimeout(ctx, hintTimeout)
	defer cancel()

	type hintResult struct {
		hints []map[string]any
		err   error
	}
	ch := make(chan hintResult, 1)

	go func() {
		hints, err := h.hintFn(hintCtx, env.Content, workspaceID)
		ch <- hintResult{hints: hints, err: err}
	}()

	select {
	case result := <-ch:
		if result.err != nil || len(result.hints) == 0 {
			return
		}
		if env.Metadata == nil {
			env.Metadata = make(map[string]string)
		}
		data, err := json.Marshal(result.hints)
		if err != nil {
			h.logger.Error("failed to marshal tool hints", "to", env.To, "error", err)
			return
		}
		env.Metadata["tool_hints"] = string(data)
		h.logger.Debug("tool hints injected",
			"to", env.To,
			"count", len(result.hints),
		)

	case <-hintCtx.Done():
		h.logger.Debug("tool hint injection exceeded latency budget",
			"to", env.To,
			"timeout", hintTimeout,
		)
	}
}

// sendMemoryFollowUp waits for a pending recall result and delivers it
// as a separate MemoryContext envelope to the recipient.
func (h *CommHub) sendMemoryFollowUp(ctx context.Context, original *types.MessageEnvelope, workspaceID, agentID string) {
	// Use a generous timeout for the follow-up — we already delivered the original.
	followCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try to receive from the pending channel, or re-run recall.
	memories, err := h.recallFn(followCtx, original.Content, workspaceID, agentID)
	if err != nil {
		h.logger.Error("failed to recall memories for follow-up", "to", original.To, "error", err)
		return
	}
	if len(memories) == 0 {
		return
	}

	data, err := json.Marshal(memories)
	if err != nil {
		h.logger.Error("failed to marshal memory follow-up", "to", original.To, "error", err)
		return
	}

	followUp := &types.MessageEnvelope{
		ID:          original.ID + "_memory_ctx",
		From:        "system:memory",
		To:          original.To,
		Trust:       types.TrustInternal,
		ContentType: "memory_context",
		Content:     string(data),
		Metadata: map[string]string{
			"related_to":   original.ID,
			"recall_type":  "proactive_followup",
			"workspace_id": workspaceID,
			"agent_id":     agentID,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	h.deliver(followUp)

	h.logger.Debug("memory follow-up delivered",
		"to", original.To,
		"related_to", original.ID,
		"count", len(memories),
	)
}

// rehydrateIfZombie checks whether the target agent is in a zombie state
// (no recent heartbeat) and triggers rehydration if both a ZombieDetector
// and RehydrationFunc are configured. This is a best-effort operation:
// rehydration failures are logged but do not block message delivery.
func (h *CommHub) rehydrateIfZombie(ctx context.Context, agentID string) {
	if h.zombieDetector == nil || h.rehydrateFn == nil {
		return
	}

	// Skip system-prefixed targets (e.g., "system:pulse", "system:memory").
	if len(agentID) > 7 && agentID[:7] == "system:" {
		return
	}

	if !h.zombieDetector(ctx, agentID) {
		return
	}

	h.logger.Info("zombie agent detected, triggering rehydration before delivery",
		"agent_id", agentID,
	)

	result, err := h.rehydrateFn(ctx, agentID)
	if err != nil {
		h.logger.Warn("pre-delivery rehydration failed",
			"agent_id", agentID,
			"error", err,
		)
		return
	}

	h.logger.Info("pre-delivery rehydration complete",
		"agent_id", agentID,
		"result", result,
	)
}

// Receive retrieves pending messages from an agent's inbox.
// Messages are removed from the inbox upon retrieval (consume semantics).
// If limit is <= 0 or exceeds the inbox size, all messages are returned.
func (h *CommHub) Receive(agentID string, limit int) []*types.MessageEnvelope {
	_, span := otel.Tracer("commhub").Start(context.Background(), "CommHub.Receive")
	defer span.End()

	span.SetAttributes(
		attribute.String("commhub.agent_id", agentID),
		attribute.Int("commhub.limit", limit),
	)

	h.mu.RLock()
	inbox, exists := h.inboxes[agentID]
	h.mu.RUnlock()

	if !exists {
		return nil
	}

	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if limit <= 0 || limit > len(inbox.Messages) {
		limit = len(inbox.Messages)
	}

	result := make([]*types.MessageEnvelope, limit)
	copy(result, inbox.Messages[:limit])
	inbox.Messages = inbox.Messages[limit:]
	return result
}

// deliver adds a message to the recipient's inbox, creating it if necessary.
func (h *CommHub) deliver(env *types.MessageEnvelope) {
	h.mu.Lock()
	inbox, exists := h.inboxes[env.To]
	if !exists {
		inbox = &AgentInbox{
			AgentID: env.To,
			maxSize: defaultInboxCapacity,
		}
		h.inboxes[env.To] = inbox
	}
	h.mu.Unlock()

	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	// Persist oldest to overflow storage if inbox is full (backpressure).
	if len(inbox.Messages) >= inbox.maxSize {
		oldest := inbox.Messages[0]
		inbox.Messages = inbox.Messages[1:]

		// Persist the dropped message if an overflow persister is configured.
		if h.overflowPersist != nil {
			entry := &types.OverflowEntry{
				AgentID:     oldest.To,
				From:        oldest.From,
				To:          oldest.To,
				ContentType: oldest.ContentType,
				Content:     oldest.Content,
				Trust:       int(oldest.Trust),
				Metadata:    oldest.Metadata,
				OriginalTS:  oldest.Timestamp,
			}
			if err := h.overflowPersist(context.Background(), entry); err != nil {
				h.logger.Error("failed to persist overflow message",
					"agent", env.To,
					"error", err,
				)
			}
		}

		h.bus.Publish(nervous.NewEvent(
			types.EventCommOverflow,
			"commhub",
			env.To,
			map[string]string{
				"agent":     env.To,
				"persisted": fmt.Sprintf("%t", h.overflowPersist != nil),
			},
		))

		h.logger.Warn("inbox overflow, oldest message persisted to overflow storage",
			"agent", env.To,
		)
	}

	inbox.Messages = append(inbox.Messages, env)
}

// DrainOverflow retrieves persisted overflow messages for an agent and delivers
// them back into the inbox. Call this when the inbox has space (e.g., after Receive).
// Returns the number of messages recovered, or 0 if no overflow drainer is configured.
func (h *CommHub) DrainOverflow(agentID string, limit int) int {
	if h.overflowDrain == nil {
		return 0
	}

	entries, err := h.overflowDrain(context.Background(), agentID, limit)
	if err != nil {
		h.logger.Error("failed to drain overflow messages", "agent", agentID, "error", err)
		return 0
	}
	if len(entries) == 0 {
		return 0
	}

	recovered := 0
	for _, entry := range entries {
		env := &types.MessageEnvelope{
			ID:          "overflow-" + entry.ID,
			From:        entry.From,
			To:          entry.To,
			ContentType: entry.ContentType,
			Content:     entry.Content,
			Trust:       types.TrustLevel(entry.Trust),
			Metadata:    entry.Metadata,
			Timestamp:   entry.OriginalTS,
		}
		h.deliver(env)
		recovered++
	}

	if recovered > 0 {
		h.logger.Info("recovered overflow messages",
			"agent", agentID,
			"count", recovered,
		)
	}

	return recovered
}

// OnboardFunc is the function signature for the lifecycle onboarding flow.
// It is injected by the application wiring layer to decouple CommHub from the
// lifecycle package and avoid circular imports.
type OnboardFunc func(ctx context.Context, agentID, personaID, parentAgentID, workspaceID string) (map[string]any, error)

// SetOnboardFunc configures the function used to trigger full agent onboarding.
// When set, CommHub.OnboardAgent delegates to the lifecycle Onboarder.
func (h *CommHub) SetOnboardFunc(fn OnboardFunc) {
	h.onboardFn = fn
}

// OnboardAgent triggers the full 4-step onboarding lifecycle for an agent,
// routing all step progression messages through the CommHub sieve. Returns
// the onboarding result summary or an error if onboarding fails.
//
// This method is the preferred entry point for agent onboarding because it
// ensures all onboarding messages pass through the Context Sieve and land
// in the agent's inbox with proper trust enforcement.
func (h *CommHub) OnboardAgent(ctx context.Context, agentID, personaID, parentAgentID, workspaceID string) (map[string]any, error) {
	if h.onboardFn == nil {
		return nil, fmt.Errorf("onboarding not configured")
	}
	return h.onboardFn(ctx, agentID, personaID, parentAgentID, workspaceID)
}

// InboxInfo holds summary stats about an agent's inbox.
type InboxInfo struct {
	AgentID string `json:"agent_id"`
	Size    int    `json:"size"`
}

// ListInboxes returns summary info for all active agent inboxes.
func (h *CommHub) ListInboxes() []InboxInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]InboxInfo, 0, len(h.inboxes))
	for id, inbox := range h.inboxes {
		inbox.mu.Lock()
		size := len(inbox.Messages)
		inbox.mu.Unlock()
		result = append(result, InboxInfo{AgentID: id, Size: size})
	}
	return result
}

// InboxSize returns the number of pending messages for an agent.
func (h *CommHub) InboxSize(agentID string) int {
	h.mu.RLock()
	inbox, exists := h.inboxes[agentID]
	h.mu.RUnlock()

	if !exists {
		return 0
	}

	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	return len(inbox.Messages)
}
