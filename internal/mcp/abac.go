package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/pkg/types"
)

// ABACMiddleware intercepts tool dispatch calls and enforces access control
// based on the authenticated caller's clearance level and the tool's required
// action and minimum clearance.
//
// When a caller lacks sufficient clearance, Dispatch returns a JSON-RPC
// -32003 "Forbidden" error without invoking the handler.
type ABACMiddleware struct {
	registry *ToolRegistry
	logger   *slog.Logger
}

// NewABACMiddleware creates an ABAC enforcement layer for the given registry.
func NewABACMiddleware(registry *ToolRegistry, logger *slog.Logger) *ABACMiddleware {
	return &ABACMiddleware{
		registry: registry,
		logger:   logger,
	}
}

// CheckAccess validates whether the authenticated caller in ctx is permitted
// to invoke the named tool. Returns nil if access is granted, or an error
// describing the denial reason.
func (m *ABACMiddleware) CheckAccess(ctx context.Context, toolName string) error {
	schema := m.registry.GetSchema(toolName)
	if schema == nil {
		// Unknown tool — let Dispatch handle the "unknown tool" error.
		return nil
	}

	// If tool requires no clearance, allow unconditionally.
	if schema.MinClearanceLevel == 0 {
		return nil
	}

	auth := AuthFromContext(ctx)

	// Unauthenticated callers are treated as clearance 0.
	callerClearance := auth.ClearanceLevel

	if callerClearance < schema.MinClearanceLevel {
		m.logger.Warn("abac access denied",
			"tool", toolName,
			"required_clearance", schema.MinClearanceLevel,
			"required_action", schema.RequiredAction,
			"caller_clearance", callerClearance,
			"caller_persona", auth.PersonaID,
			"authenticated", auth.Authenticated,
		)
		return fmt.Errorf(
			"forbidden: tool %q requires clearance level %d (action: %s), caller has %d",
			toolName, schema.MinClearanceLevel, schema.RequiredAction, callerClearance,
		)
	}

	return nil
}

// WrapDispatch returns a new Dispatch function that checks ABAC permissions
// before delegating to the registry's Dispatch. This is injected into the
// transport layer's processRequest to enforce access control on every tools/call.
func (m *ABACMiddleware) WrapDispatch(
	original func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error),
) func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
	return func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		if err := m.CheckAccess(ctx, name); err != nil {
			return nil, fmt.Errorf("mcp.ABACMiddleware.WrapDispatch: %w", err)
		}
		return original(ctx, name, params)
	}
}
