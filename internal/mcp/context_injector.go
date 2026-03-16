package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// memoryInjectionTimeout is the maximum time allowed for memory recall
// during MCP tool response context injection. Recall that exceeds this
// budget is silently dropped to avoid degrading tool response latency.
const memoryInjectionTimeout = 30 * time.Millisecond

// toolsExcludedFromInjection lists tool name prefixes that should not have
// memory context injected. Memory tools themselves, system introspection, and
// config tools are excluded to avoid circular or noisy injection.
var toolsExcludedFromInjection = map[string]bool{
	"store_memory":      true,
	"recall_memories":   true,
	"delete_memory":     true,
	"consolidate":       true,
	"get_config":        true,
	"set_config":        true,
	"list_tools":        true,
	"get_pulse_status":  true,
	"get_session_telemetry": true,
}

// NewMemoryContextInjector creates a ContextInjector that enriches tool results
// with relevant memories from the MemoryRepo. The injector extracts a query from
// the tool parameters (looking for common fields like "query", "content", "path",
// "name") and runs a scoped BM25 recall with a tight latency budget.
//
// If memRepo is nil, returns nil (no injection configured).
func NewMemoryContextInjector(memRepo repo.MemoryRepo, logger *slog.Logger) ContextInjector {
	if memRepo == nil {
		return nil
	}

	return func(ctx context.Context, toolName string, params json.RawMessage, result *types.ToolResult) {
		// Skip excluded tools.
		if toolsExcludedFromInjection[toolName] {
			return
		}

		// Extract a query string from the tool parameters.
		query := extractQueryFromParams(params)
		if query == "" {
			return
		}

		// Run recall with a tight timeout.
		recallCtx, cancel := context.WithTimeout(ctx, memoryInjectionTimeout)
		defer cancel()

		memories, err := memRepo.Recall(recallCtx, query, types.MemoryScopeGlobal, "", "", 3)
		if err != nil || len(memories) == 0 {
			return
		}

		// Build memory context entries.
		contexts := make([]types.MemoryContext, 0, len(memories))
		for i, m := range memories {
			contexts = append(contexts, types.MemoryContext{
				Memory: *m,
				Score:  1.0 / float64(i+1),
				Rank:   i + 1,
				Source: "tool_injection",
			})
		}

		// Append as an additional content item in the tool result.
		data, err := json.Marshal(contexts)
		if err != nil {
			logger.Debug("failed to marshal memory context for injection", "error", err)
			return
		}

		result.Content = append(result.Content, types.ToolContent{
			Type: "text",
			Text: "---\n[Memory Context]\n" + string(data),
		})

		logger.Debug("memory context injected into tool response",
			"tool", toolName,
			"memory_count", len(contexts),
		)
	}
}

// extractQueryFromParams attempts to extract a meaningful search string from
// tool parameters by checking common field names. Returns empty string if
// no suitable field is found or the parameters cannot be parsed.
func extractQueryFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return ""
	}

	// Check fields in priority order.
	for _, key := range []string{"query", "content", "search", "name", "path", "symbol", "pattern"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var val string
		if err := json.Unmarshal(raw, &val); err == nil && val != "" {
			return val
		}
	}
	return ""
}
