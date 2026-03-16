//go:build !noguard

package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// mockConfigResolver implements ConfigResolver for testing.
type mockConfigResolver struct {
	values map[string]string
}

func (m *mockConfigResolver) Resolve(_ context.Context, key, _, _ string) (string, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

// ---------------------------------------------------------------------------
// ActionManager tests
// ---------------------------------------------------------------------------

func TestCreatePendingAndApprove(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	req := &EvalRequest{
		ToolName:   "delete_project",
		ToolAction: "delete",
		ToolParams: json.RawMessage(`{"id":"proj-1"}`),
	}

	id, ch := mgr.CreatePending(req, "approve_writes", 5*time.Second)
	if id == "" {
		t.Fatal("expected non-empty action ID")
	}

	// Approve in background.
	go func() {
		_, err := mgr.Approve(id, "admin", "looks good")
		if err != nil {
			t.Errorf("approve error: %v", err)
		}
	}()

	select {
	case decision := <-ch:
		if !decision.Approved {
			t.Fatal("expected approved=true")
		}
		if decision.DecidedBy != "admin" {
			t.Errorf("expected decided_by=admin, got %q", decision.DecidedBy)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestCreatePendingAndReject(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	req := &EvalRequest{
		ToolName:   "delete_project",
		ToolAction: "delete",
		ToolParams: json.RawMessage(`{}`),
	}

	id, ch := mgr.CreatePending(req, "approve_writes", 5*time.Second)

	go func() {
		_, err := mgr.Reject(id, "reviewer", "not allowed")
		if err != nil {
			t.Errorf("reject error: %v", err)
		}
	}()

	select {
	case decision := <-ch:
		if decision.Approved {
			t.Fatal("expected approved=false")
		}
		if decision.Notes != "not allowed" {
			t.Errorf("expected notes='not allowed', got %q", decision.Notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestActionTimeout(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	req := &EvalRequest{
		ToolName:   "run_pipeline",
		ToolAction: "execute",
		ToolParams: json.RawMessage(`{}`),
	}

	_, ch := mgr.CreatePending(req, "approve_writes", 100*time.Millisecond)

	select {
	case decision := <-ch:
		if decision.Approved {
			t.Fatal("expected timeout to reject")
		}
		if decision.Notes != "timeout" {
			t.Errorf("expected notes='timeout', got %q", decision.Notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timeout decision")
	}
}

func TestPendingActions(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	req1 := &EvalRequest{ToolName: "tool_a", ToolAction: "write", ToolParams: json.RawMessage(`{}`)}
	req2 := &EvalRequest{ToolName: "tool_b", ToolAction: "delete", ToolParams: json.RawMessage(`{}`)}

	mgr.CreatePending(req1, "guard_a", 10*time.Second)
	mgr.CreatePending(req2, "guard_b", 10*time.Second)

	pending := mgr.PendingActions()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending actions, got %d", len(pending))
	}
}

func TestHistory(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	req1 := &EvalRequest{ToolName: "tool_a", ToolAction: "write", ToolParams: json.RawMessage(`{}`)}
	req2 := &EvalRequest{ToolName: "tool_b", ToolAction: "delete", ToolParams: json.RawMessage(`{}`)}

	id1, _ := mgr.CreatePending(req1, "guard_a", 10*time.Second)
	id2, _ := mgr.CreatePending(req2, "guard_b", 10*time.Second)

	mgr.Approve(id1, "admin", "ok")
	mgr.Reject(id2, "admin", "no")

	history := mgr.History(10)
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}

	// Most recent first.
	if history[0].Status != types.GuardStatusRejected {
		t.Errorf("expected first history entry to be rejected, got %s", history[0].Status)
	}
	if history[1].Status != types.GuardStatusApproved {
		t.Errorf("expected second history entry to be approved, got %s", history[1].Status)
	}
}

// ---------------------------------------------------------------------------
// ApproveWritesGuard tests
// ---------------------------------------------------------------------------

func TestApproveWritesGuard_DeniesWrites(t *testing.T) {
	g := NewApproveWritesGuard(5 * time.Minute)

	for _, action := range []string{"write", "delete", "admin", "execute"} {
		req := &EvalRequest{ToolAction: action}
		approved, err := g.Evaluate(context.Background(), req)
		if err != nil {
			t.Fatalf("evaluate(%s) error: %v", action, err)
		}
		if approved {
			t.Errorf("expected action %q to be denied, got approved", action)
		}
	}
}

func TestApproveWritesGuard_AllowsReads(t *testing.T) {
	g := NewApproveWritesGuard(5 * time.Minute)

	req := &EvalRequest{ToolAction: "read"}
	approved, err := g.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("evaluate(read) error: %v", err)
	}
	if !approved {
		t.Error("expected read action to be approved")
	}
}

// ---------------------------------------------------------------------------
// GuardMiddleware tests
// ---------------------------------------------------------------------------

func TestMiddleware_DisabledPassesThrough(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	cfg := &mockConfigResolver{values: map[string]string{
		"guard.enabled": "false",
	}}

	mw := NewGuardMiddleware(
		func(_ string) string { return "write" },
		func(_ context.Context) types.AuthContext { return types.AuthContext{} },
		mgr,
		cfg,
		testLogger(),
	)
	mw.RegisterGuard(NewApproveWritesGuard(5 * time.Minute))

	called := false
	original := func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		called = true
		return &types.ToolResult{Content: []types.ToolContent{{Text: "ok"}}}, nil
	}

	dispatch := mw.WrapDispatch(original)
	_, err := dispatch(context.Background(), "create_project", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if !called {
		t.Error("expected original dispatch to be called when guard is disabled")
	}
}

func TestMiddleware_SkipListPassesThrough(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	cfg := &mockConfigResolver{values: map[string]string{
		"guard.enabled": "true",
	}}

	mw := NewGuardMiddleware(
		func(_ string) string { return "write" },
		func(_ context.Context) types.AuthContext { return types.AuthContext{} },
		mgr,
		cfg,
		testLogger(),
	)
	mw.RegisterGuard(NewApproveWritesGuard(5 * time.Minute))

	called := false
	original := func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		called = true
		return &types.ToolResult{Content: []types.ToolContent{{Text: "ok"}}}, nil
	}

	dispatch := mw.WrapDispatch(original)
	_, err := dispatch(context.Background(), "approve_action", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if !called {
		t.Error("expected original dispatch to be called for skip-listed tool")
	}
}

func TestMiddleware_WriteDenied(t *testing.T) {
	bus := nervous.NewEventBus(16)
	mgr := NewActionManager(bus, testLogger())

	cfg := &mockConfigResolver{values: map[string]string{
		"guard.enabled":            "true",
		"guard.auto_approve_reads": "true",
	}}

	mw := NewGuardMiddleware(
		func(_ string) string { return "write" },
		func(_ context.Context) types.AuthContext { return types.AuthContext{PersonaID: "test-agent"} },
		mgr,
		cfg,
		testLogger(),
	)
	mw.RegisterGuard(NewApproveWritesGuard(5 * time.Minute))

	originalCalled := false
	original := func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		originalCalled = true
		return &types.ToolResult{Content: []types.ToolContent{{Text: "ok"}}}, nil
	}

	dispatch := mw.WrapDispatch(original)
	result, err := dispatch(context.Background(), "create_project", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if originalCalled {
		t.Error("expected original dispatch NOT to be called for write action")
	}

	// Should return pending_approval status.
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected non-empty result")
	}

	var pending struct {
		Status   string `json:"status"`
		ActionID string `json:"action_id"`
		Guard    string `json:"guard"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &pending); err != nil {
		t.Fatalf("unmarshal pending result: %v", err)
	}
	if pending.Status != "pending_approval" {
		t.Errorf("expected status=pending_approval, got %q", pending.Status)
	}
	if pending.ActionID == "" {
		t.Error("expected non-empty action_id")
	}
	if pending.Guard != "approve_writes" {
		t.Errorf("expected guard=approve_writes, got %q", pending.Guard)
	}
}
