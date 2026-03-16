package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestABACMiddleware_AllowUnauthenticatedClearance0(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("list_workspaces", "list workspaces", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return types.NewToolResult("ok"), nil
		},
	)
	// Default clearance is 0, so unauthenticated callers should pass.

	abac := NewABACMiddleware(registry, slog.Default())
	ctx := context.Background() // No auth context.

	err := abac.CheckAccess(ctx, "list_workspaces")
	if err != nil {
		t.Fatalf("expected access granted for clearance-0 tool, got: %v", err)
	}
}

func TestABACMiddleware_DenyInsufficientClearance(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("set_config", "set config", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return types.NewToolResult("ok"), nil
		},
	)
	registry.SetToolABAC("set_config", 2, "admin")

	abac := NewABACMiddleware(registry, slog.Default())

	// Authenticated with clearance 1 — should be denied.
	ac := types.AuthContext{
		PersonaID:      "p1",
		ClearanceLevel: 1,
		Authenticated:  true,
	}
	ctx := withAuthContext(context.Background(), ac)

	err := abac.CheckAccess(ctx, "set_config")
	if err == nil {
		t.Fatal("expected access denied for clearance 1 on admin tool")
	}
}

func TestABACMiddleware_AllowSufficientClearance(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("set_config", "set config", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return types.NewToolResult("ok"), nil
		},
	)
	registry.SetToolABAC("set_config", 2, "admin")

	abac := NewABACMiddleware(registry, slog.Default())

	ac := types.AuthContext{
		PersonaID:      "p1",
		ClearanceLevel: 2,
		Authenticated:  true,
	}
	ctx := withAuthContext(context.Background(), ac)

	err := abac.CheckAccess(ctx, "set_config")
	if err != nil {
		t.Fatalf("expected access granted for clearance 2, got: %v", err)
	}
}

func TestABACMiddleware_UnauthenticatedDeniedHighClearance(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("install_plugin", "install plugin", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return types.NewToolResult("ok"), nil
		},
	)
	registry.SetToolABAC("install_plugin", 2, "admin")

	abac := NewABACMiddleware(registry, slog.Default())
	ctx := context.Background() // No auth — clearance 0.

	err := abac.CheckAccess(ctx, "install_plugin")
	if err == nil {
		t.Fatal("expected unauthenticated caller denied admin tool")
	}
}

func TestABACMiddleware_UnknownToolPassesThrough(t *testing.T) {
	registry := NewToolRegistry()
	abac := NewABACMiddleware(registry, slog.Default())
	ctx := context.Background()

	// Unknown tools should not be blocked by ABAC (Dispatch handles the error).
	err := abac.CheckAccess(ctx, "nonexistent_tool")
	if err != nil {
		t.Fatalf("expected no ABAC error for unknown tool, got: %v", err)
	}
}

func TestABACMiddleware_WrapDispatch(t *testing.T) {
	registry := NewToolRegistry()
	called := false
	registry.Register("admin_tool", "admin only", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			called = true
			return types.NewToolResult("ok"), nil
		},
	)
	registry.SetToolABAC("admin_tool", 2, "admin")

	abac := NewABACMiddleware(registry, slog.Default())
	wrapped := abac.WrapDispatch(registry.Dispatch)

	// Call without sufficient clearance.
	ctx := withAuthContext(context.Background(), types.AuthContext{
		PersonaID:      "p1",
		ClearanceLevel: 0,
		Authenticated:  true,
	})
	_, err := wrapped(ctx, "admin_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from ABAC denial")
	}
	if called {
		t.Fatal("handler should not have been called after ABAC denial")
	}

	// Call with sufficient clearance.
	ctx = withAuthContext(context.Background(), types.AuthContext{
		PersonaID:      "p1",
		ClearanceLevel: 2,
		Authenticated:  true,
	})
	result, err := wrapped(ctx, "admin_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !called {
		t.Fatal("handler should have been called after ABAC approval")
	}
}

func TestSetToolABAC(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("my_tool", "test", json.RawMessage(`{}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return nil, nil
		},
	)

	// Default should be clearance 0, action "view".
	schema := registry.GetSchema("my_tool")
	if schema == nil {
		t.Fatal("expected schema")
	}
	if schema.MinClearanceLevel != 0 {
		t.Errorf("expected default clearance 0, got %d", schema.MinClearanceLevel)
	}
	if schema.RequiredAction != "view" {
		t.Errorf("expected default action 'view', got %q", schema.RequiredAction)
	}

	// Update ABAC.
	ok := registry.SetToolABAC("my_tool", 2, "admin")
	if !ok {
		t.Fatal("SetToolABAC returned false")
	}

	schema = registry.GetSchema("my_tool")
	if schema.MinClearanceLevel != 2 {
		t.Errorf("expected clearance 2, got %d", schema.MinClearanceLevel)
	}
	if schema.RequiredAction != "admin" {
		t.Errorf("expected action 'admin', got %q", schema.RequiredAction)
	}

	// Unknown tool.
	ok = registry.SetToolABAC("nonexistent", 1, "write")
	if ok {
		t.Fatal("SetToolABAC should return false for unknown tool")
	}
}

func TestApplyDefaultABACLevels(t *testing.T) {
	registry := NewToolRegistry()

	// Register consolidated tools from the default ABAC map.
	for _, name := range []string{"config", "workspace", "plugin", "secret"} {
		registry.Register(name, "test", json.RawMessage(`{}`),
			func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
				return nil, nil
			},
		)
	}

	ApplyDefaultABACLevels(registry)

	tests := []struct {
		name      string
		clearance int
		action    string
	}{
		{"config", 0, "view"},
		{"workspace", 0, "view"},
		{"plugin", 0, "view"},
		{"secret", 2, "admin"},
	}

	for _, tt := range tests {
		schema := registry.GetSchema(tt.name)
		if schema == nil {
			t.Errorf("no schema for %s", tt.name)
			continue
		}
		if schema.MinClearanceLevel != tt.clearance {
			t.Errorf("%s: expected clearance %d, got %d", tt.name, tt.clearance, schema.MinClearanceLevel)
		}
		if schema.RequiredAction != tt.action {
			t.Errorf("%s: expected action %q, got %q", tt.name, tt.action, schema.RequiredAction)
		}
	}
}
