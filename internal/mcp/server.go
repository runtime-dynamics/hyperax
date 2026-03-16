package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/observability"
	"github.com/hyperax/hyperax/pkg/types"
)

// ToolHandler is the function signature for all MCP tool implementations.
type ToolHandler func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error)

// ToolSchema describes a tool for MCP discovery (tools/list) and ABAC enforcement.
type ToolSchema struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	InputSchema       json.RawMessage `json:"inputSchema"`
	MinClearanceLevel int             `json:"minClearanceLevel,omitempty"` // ABAC: minimum clearance (default 0)
	RequiredAction    string          `json:"requiredAction,omitempty"`   // ABAC: action type (default "read")
	ExposedToLLM      bool            `json:"exposedToLLM,omitempty"`     // Whether this tool is visible to the LLM resolver
}

// ContextInjector is called after tool dispatch to enrich the result with
// contextual information (e.g., relevant memories). It receives the tool name,
// parameters, and the result, and may modify the result in place.
type ContextInjector func(ctx context.Context, toolName string, params json.RawMessage, result *types.ToolResult)

// ToolRegistry holds registered tool handlers and their schemas.
type ToolRegistry struct {
	mu              sync.RWMutex
	handlers        map[string]ToolHandler
	schemaMap       map[string]ToolSchema // canonical storage, keyed by name
	schemaOrder     []string              // insertion order for tools/list
	contextInjector ContextInjector
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers:  make(map[string]ToolHandler),
		schemaMap: make(map[string]ToolSchema),
	}
}

// Register adds a tool to the registry. Panics on duplicate names.
func (r *ToolRegistry) Register(name, description string, inputSchema json.RawMessage, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; exists {
		panic(fmt.Sprintf("duplicate tool registration: %s", name))
	}

	r.handlers[name] = handler
	r.schemaMap[name] = ToolSchema{
		Name:           name,
		Description:    description,
		InputSchema:    inputSchema,
		RequiredAction: "view", // Default action.
	}
	r.schemaOrder = append(r.schemaOrder, name)
}

// SetContextInjector configures a post-dispatch hook that enriches tool results
// with contextual information such as relevant memories. The injector is called
// only for successful tool calls (no error, result is not nil).
func (r *ToolRegistry) SetContextInjector(injector ContextInjector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextInjector = injector
}

// Dispatch looks up and invokes a tool by name.
func (r *ToolRegistry) Dispatch(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	injector := r.contextInjector
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mcp.ToolRegistry.Dispatch: unknown tool: %s", name)
	}

	start := time.Now()
	result, err := handler(ctx, params)
	elapsed := time.Since(start)

	if result != nil {
		result.ElapsedMS = elapsed.Milliseconds()
	}

	// Record Prometheus metrics for tool invocations.
	status := "ok"
	if err != nil {
		status = "error"
	} else if result != nil && result.IsError {
		status = "error"
	}
	observability.ToolInvocationDuration.WithLabelValues(name).Observe(elapsed.Seconds())
	observability.ToolInvocationsTotal.WithLabelValues(name, status).Inc()

	// Run context injection for successful tool calls.
	if injector != nil && err == nil && result != nil && !result.IsError {
		injector(ctx, name, params, result)
	}

	return result, err
}

// Schemas returns all registered tool schemas in insertion order.
func (r *ToolRegistry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSchema, 0, len(r.schemaOrder))
	for _, name := range r.schemaOrder {
		if s, ok := r.schemaMap[name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// GetSchema returns the ToolSchema for a tool by name, or nil if not found.
func (r *ToolRegistry) GetSchema(name string) *ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.schemaMap[name]
	if !ok {
		return nil
	}
	return &s
}

// SetToolABAC updates the ABAC fields on an already-registered tool.
// This allows bulk assignment of clearance levels after handler registration.
func (r *ToolRegistry) SetToolABAC(name string, minClearance int, action string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.schemaMap[name]
	if !ok {
		return false
	}
	s.MinClearanceLevel = minClearance
	s.RequiredAction = action
	r.schemaMap[name] = s
	return true
}

// SetToolExposedToLLM sets whether a tool is visible to the LLM resolver.
func (r *ToolRegistry) SetToolExposedToLLM(name string, exposed bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.schemaMap[name]
	if !ok {
		return false
	}
	s.ExposedToLLM = exposed
	r.schemaMap[name] = s
	return true
}

// Unregister removes a tool from the registry by name.
// Returns true if the tool was found and removed, false otherwise.
func (r *ToolRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; !exists {
		return false
	}

	delete(r.handlers, name)
	delete(r.schemaMap, name)

	// Remove from order slice.
	for i, n := range r.schemaOrder {
		if n == name {
			r.schemaOrder = append(r.schemaOrder[:i], r.schemaOrder[i+1:]...)
			break
		}
	}

	return true
}

// HasTool reports whether a tool with the given name is registered.
func (r *ToolRegistry) HasTool(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[name]
	return ok
}

// ToolCount returns the number of registered tools.
func (r *ToolRegistry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers)
}

// Handler is implemented by each tool domain.
type Handler interface {
	RegisterTools(registry *ToolRegistry)
}

// Server is the MCP protocol server.
type Server struct {
	Registry *ToolRegistry
	Logger   *slog.Logger
}

// NewServer creates an MCP server.
func NewServer(logger *slog.Logger) *Server {
	return &Server{
		Registry: NewToolRegistry(),
		Logger:   logger,
	}
}

// RegisterHandler calls RegisterTools on the given handler.
func (s *Server) RegisterHandler(h Handler) {
	h.RegisterTools(s.Registry)
}
