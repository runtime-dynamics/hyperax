//go:build !noguard

package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// PluginDispatchFunc is the function signature for dispatching tool calls to plugins.
type PluginDispatchFunc func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)

// PluginGuard adapts a guard plugin's MCP tool into the Guard interface.
// It proxies Evaluate() calls to the plugin's subprocess via an MCP tool call.
type PluginGuard struct {
	pluginName string
	toolName   string
	dispatch   PluginDispatchFunc
	timeout    time.Duration
}

// NewPluginGuard creates a guard that delegates evaluation to a plugin MCP tool.
func NewPluginGuard(pluginName, toolName string, dispatch PluginDispatchFunc, timeout time.Duration) *PluginGuard {
	return &PluginGuard{
		pluginName: pluginName,
		toolName:   toolName,
		dispatch:   dispatch,
		timeout:    timeout,
	}
}

func (g *PluginGuard) Name() string {
	return g.pluginName
}

func (g *PluginGuard) Evaluate(ctx context.Context, req *EvalRequest) (bool, error) {
	params, _ := json.Marshal(map[string]any{
		"tool_name":   req.ToolName,
		"tool_action": req.ToolAction,
		"tool_params": req.ToolParams,
	})

	result, err := g.dispatch(ctx, g.toolName, params)
	if err != nil {
		return false, fmt.Errorf("guard.PluginGuard.Evaluate: plugin %q: %w", g.pluginName, err)
	}

	if result == nil || len(result.Content) == 0 {
		return false, fmt.Errorf("guard.PluginGuard.Evaluate: plugin %q returned empty result", g.pluginName)
	}

	var resp struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		return false, fmt.Errorf("guard.PluginGuard.Evaluate: plugin %q: parse result: %w", g.pluginName, err)
	}

	return resp.Approved, nil
}

func (g *PluginGuard) Timeout() time.Duration {
	return g.timeout
}
