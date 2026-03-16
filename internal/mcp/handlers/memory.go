package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/memory"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceMemory maps each memory action to its minimum ABAC clearance.
var actionClearanceMemory = map[string]int{
	"store":       0, // was store_memory: clearance 0
	"recall":      0, // was recall_memory: clearance 0
	"forget":      1, // was forget_memory: clearance 1
	"stats":       0, // was get_memory_stats: clearance 0
	"consolidate": 1, // was consolidate_memories: clearance 1
}

// MemoryHandler implements the consolidated "memory" MCP tool.
type MemoryHandler struct {
	store *memory.MemoryStore
}

// NewMemoryHandler creates a MemoryHandler.
func NewMemoryHandler(store *memory.MemoryStore) *MemoryHandler {
	return &MemoryHandler{store: store}
}

// RegisterTools registers the consolidated memory tool.
func (h *MemoryHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"memory",
		"Memory management: store, recall, forget, get stats, and consolidate agent memories. "+
			"Actions: store | recall | forget | stats | consolidate",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":         {"type": "string", "enum": ["store", "recall", "forget", "stats", "consolidate"], "description": "Action to perform"},
				"scope":          {"type": "string", "description": "Memory scope: global, project, or persona", "enum": ["global", "project", "persona"]},
				"type":           {"type": "string", "description": "Memory type: episodic, semantic, or procedural", "enum": ["episodic", "semantic", "procedural"]},
				"content":        {"type": "string", "description": "Content to store (store action)"},
				"workspace_id":   {"type": "string", "description": "Workspace ID"},
				"persona_id":     {"type": "string", "description": "Persona ID"},
				"tags":           {"type": "array", "items": {"type": "string"}, "description": "Tags for categorization (store action)"},
				"source":         {"type": "string", "description": "Origin of the memory (store action)"},
				"anchored":       {"type": "boolean", "description": "Protect from eviction (store action, requires clearance >= 2)"},
				"query":          {"type": "string", "description": "Natural language search query (recall action)"},
				"max_results":    {"type": "integer", "description": "Maximum results (recall action, default 10)"},
				"id":             {"type": "string", "description": "Memory ID (forget action)"},
				"older_than_days": {"type": "integer", "description": "Minimum days since last access (consolidate action, default 30)"},
				"dry_run":        {"type": "boolean", "description": "Preview without modifying (consolidate action)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "memory" tool to the correct handler method.
func (h *MemoryHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceMemory); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "store":
		return h.storeMemory(ctx, params)
	case "recall":
		return h.recallMemory(ctx, params)
	case "forget":
		return h.forgetMemory(ctx, params)
	case "stats":
		return h.getMemoryStats(ctx, params)
	case "consolidate":
		return h.consolidateMemories(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown memory action %q: valid actions are store, recall, forget, stats, consolidate", envelope.Action)), nil
	}
}

func (h *MemoryHandler) storeMemory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope       string   `json:"scope"`
		Type        string   `json:"type"`
		Content     string   `json:"content"`
		WorkspaceID string   `json:"workspace_id"`
		PersonaID   string   `json:"persona_id"`
		Tags        []string `json:"tags"`
		Source      string   `json:"source"`
		Anchored    bool     `json:"anchored"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.storeMemory: %w", err)
	}
	if args.Content == "" {
		return types.NewErrorResult("content is required"), nil
	}

	if h.store == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	// Defaults.
	if args.Scope == "" {
		args.Scope = "project"
	}
	if args.Type == "" {
		args.Type = "episodic"
	}

	// Validate scope.
	switch args.Scope {
	case "global", "project", "persona":
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid scope %q: must be global, project, or persona", args.Scope)), nil
	}

	// Validate type.
	switch args.Type {
	case "episodic", "semantic", "procedural":
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid type %q: must be episodic, semantic, or procedural", args.Type)), nil
	}

	// Build metadata.
	metadata := make(map[string]any)
	if len(args.Tags) > 0 {
		metadata["tags"] = args.Tags
	}
	if args.Source != "" {
		metadata["source"] = args.Source
	}
	if args.Anchored {
		metadata["anchored"] = true
	}

	mem := &types.Memory{
		Scope:       types.MemoryScope(args.Scope),
		Type:        types.MemoryType(args.Type),
		Content:     args.Content,
		WorkspaceID: args.WorkspaceID,
		PersonaID:   args.PersonaID,
		Metadata:    metadata,
	}

	id, err := h.store.Store(ctx, mem)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("store memory: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Memory stored (scope=%s, type=%s).", args.Scope, args.Type),
	}), nil
}

func (h *MemoryHandler) recallMemory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Query       string `json:"query"`
		WorkspaceID string `json:"workspace_id"`
		PersonaID   string `json:"persona_id"`
		MaxResults  int    `json:"max_results"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.recallMemory: %w", err)
	}
	if args.Query == "" {
		return types.NewErrorResult("query is required"), nil
	}

	if h.store == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	if args.MaxResults <= 0 {
		args.MaxResults = 10
	}

	query := types.MemoryQuery{
		Query:       args.Query,
		WorkspaceID: args.WorkspaceID,
		PersonaID:   args.PersonaID,
		MaxResults:  args.MaxResults,
	}

	results, err := h.store.Recall(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.recallMemory: %w", err)
	}

	if len(results) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	type memorySummary struct {
		ID          string  `json:"id"`
		Scope       string  `json:"scope"`
		Type        string  `json:"type"`
		Content     string  `json:"content"`
		Score       float64 `json:"score"`
		Rank        int     `json:"rank"`
		WorkspaceID string  `json:"workspace_id,omitempty"`
		PersonaID   string  `json:"persona_id,omitempty"`
	}

	summaries := make([]memorySummary, len(results))
	for i, mc := range results {
		m := mc.Memory
		summaries[i] = memorySummary{
			ID:          m.ID,
			Scope:       string(m.Scope),
			Type:        string(m.Type),
			Content:     m.Content,
			Score:       mc.Score,
			Rank:        mc.Rank,
			WorkspaceID: m.WorkspaceID,
			PersonaID:   m.PersonaID,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *MemoryHandler) forgetMemory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.forgetMemory: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.store == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	if err := h.store.Delete(ctx, args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("forget memory: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":     args.ID,
		"status": "deleted",
	}), nil
}

func (h *MemoryHandler) getMemoryStats(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope       string `json:"scope"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.getMemoryStats: %w", err)
	}

	if h.store == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	result := make(map[string]any)
	if args.WorkspaceID != "" {
		result["workspace_id"] = args.WorkspaceID
	}

	if args.Scope != "" {
		scope := types.MemoryScope(args.Scope)
		count, err := h.store.Count(ctx, scope, args.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("handlers.MemoryHandler.getMemoryStats: %w", err)
		}
		byType, err := h.store.CountByType(ctx, scope, args.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("handlers.MemoryHandler.getMemoryStats: %w", err)
		}
		result["scope"] = args.Scope
		result["count"] = count
		result["by_type"] = byType
		return types.NewToolResult(result), nil
	}

	// Count per scope.
	scopes := []types.MemoryScope{types.MemoryScopeGlobal, types.MemoryScopeProject, types.MemoryScopePersona}
	total := 0
	scopeCounts := make(map[string]int)
	for _, s := range scopes {
		count, err := h.store.Count(ctx, s, args.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("handlers.MemoryHandler.getMemoryStats: %w", err)
		}
		scopeCounts[string(s)] = count
		total += count
	}
	result["by_scope"] = scopeCounts
	result["total"] = total

	return types.NewToolResult(result), nil
}

func (h *MemoryHandler) consolidateMemories(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope         string `json:"scope"`
		OlderThanDays int    `json:"older_than_days"`
		DryRun        bool   `json:"dry_run"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.MemoryHandler.consolidateMemories: %w", err)
	}

	if h.store == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	scope := types.MemoryScope(args.Scope) // empty string queries all scopes

	count, err := h.store.Consolidate(ctx, scope, args.OlderThanDays, args.DryRun)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("consolidation failed: %v", err)), nil
	}

	if args.DryRun {
		return types.NewToolResult(map[string]any{
			"dry_run":    true,
			"candidates": count,
			"message":    fmt.Sprintf("Found %d consolidation candidates.", count),
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"consolidated": count,
		"message":      fmt.Sprintf("Consolidated %d episodic memories into semantic summaries.", count),
	}), nil
}

