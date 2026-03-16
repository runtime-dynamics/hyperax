package mcp

import (
	"context"
	"encoding/json"

	"github.com/hyperax/hyperax/pkg/types"
)

// Dispatcher provides a simple API for dispatching tool calls.
type Dispatcher struct {
	registry *ToolRegistry
}

// NewDispatcher creates a Dispatcher from a ToolRegistry.
func NewDispatcher(registry *ToolRegistry) *Dispatcher {
	return &Dispatcher{registry: registry}
}

// Call invokes a tool by name with the given params.
func (d *Dispatcher) Call(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
	return d.registry.Dispatch(ctx, name, params)
}

// ToolNames returns the names of all registered tools.
func (d *Dispatcher) ToolNames() []string {
	schemas := d.registry.Schemas()
	names := make([]string, len(schemas))
	for i, s := range schemas {
		names[i] = s.Name
	}
	return names
}
