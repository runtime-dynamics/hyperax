package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestToolRegistry_Register(t *testing.T) {
	r := NewToolRegistry()

	r.Register("test_tool", "A test tool", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		return types.NewToolResult("ok"), nil
	})

	if r.ToolCount() != 1 {
		t.Errorf("count = %d, want 1", r.ToolCount())
	}

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("schemas = %d, want 1", len(schemas))
	}
	if schemas[0].Name != "test_tool" {
		t.Errorf("name = %q", schemas[0].Name)
	}
}

func TestToolRegistry_RegisterDuplicate(t *testing.T) {
	r := NewToolRegistry()

	r.Register("dup", "First", json.RawMessage(`{}`), func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		return nil, nil
	})

	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()

	r.Register("dup", "Second", json.RawMessage(`{}`), func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		return nil, nil
	})
}

func TestToolRegistry_Dispatch(t *testing.T) {
	r := NewToolRegistry()

	r.Register("echo", "Echo params", json.RawMessage(`{}`), func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		return types.NewToolResult(string(params)), nil
	})

	result, err := r.Dispatch(context.Background(), "echo", json.RawMessage(`{"msg":"hello"}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.IsError {
		t.Error("result should not be error")
	}
	if result.ElapsedMS < 0 {
		t.Error("elapsed should be >= 0")
	}
}

func TestToolRegistry_DispatchUnknown(t *testing.T) {
	r := NewToolRegistry()

	_, err := r.Dispatch(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestToolRegistry_Schemas_IsCopy(t *testing.T) {
	r := NewToolRegistry()

	r.Register("a", "Tool A", json.RawMessage(`{}`), func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		return nil, nil
	})

	s1 := r.Schemas()
	s2 := r.Schemas()

	// Modifying one should not affect the other
	s1[0].Name = "modified"
	if s2[0].Name == "modified" {
		t.Error("Schemas() should return a copy")
	}
}
