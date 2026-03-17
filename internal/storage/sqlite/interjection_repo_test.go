package sqlite

import (
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestInterjectionRepo_Create(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	ij := &types.Interjection{
		Scope:    "workspace:hyperax",
		Severity: "critical",
		Source:   "budget-monitor",
		Reason:   "Token budget exceeded",
	}

	id, err := r.Create(ctx, ij)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestInterjectionRepo_GetByID(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	ij := &types.Interjection{
		Scope:    "workspace:test",
		Severity: "warning",
		Source:   "agent-1",
		Reason:   "test reason",
	}
	id, err := r.Create(ctx, ij)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.ID != id {
		t.Errorf("expected ID %q, got %q", id, got.ID)
	}
	if got.Scope != "workspace:test" {
		t.Errorf("expected scope workspace:test, got %q", got.Scope)
	}
	if got.Status != "active" {
		t.Errorf("expected status active, got %q", got.Status)
	}
}

func TestInterjectionRepo_GetByIDNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	_, err := r.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestInterjectionRepo_GetActive(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	scope := "workspace:test"

	// No active interjections initially.
	active, err := r.GetActive(ctx, scope)
	if err != nil {
		t.Fatalf("get active (empty): %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected 0 active, got %d", len(active))
	}

	// Create two interjections in same scope.
	_, _ = r.Create(ctx, &types.Interjection{
		Scope: scope, Severity: "warning", Source: "agent-1", Reason: "slow response",
	})
	_, _ = r.Create(ctx, &types.Interjection{
		Scope: scope, Severity: "critical", Source: "agent-2", Reason: "memory overflow",
	})

	// Create one in a different scope (should not appear).
	_, _ = r.Create(ctx, &types.Interjection{
		Scope: "workspace:other", Severity: "warning", Source: "agent-3", Reason: "unrelated",
	})

	active, err = r.GetActive(ctx, scope)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}

	for _, ij := range active {
		if ij.Status != "active" {
			t.Errorf("expected status 'active', got %q", ij.Status)
		}
		if ij.CreatedAt.IsZero() {
			t.Error("expected non-zero created_at")
		}
	}
}

func TestInterjectionRepo_GetAllActive(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	_, _ = r.Create(ctx, &types.Interjection{
		Scope: "workspace:a", Severity: "warning", Source: "agent-1", Reason: "a",
	})
	_, _ = r.Create(ctx, &types.Interjection{
		Scope: "workspace:b", Severity: "critical", Source: "agent-2", Reason: "b",
	})

	all, err := r.GetAllActive(ctx)
	if err != nil {
		t.Fatalf("get all active: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestInterjectionRepo_Resolve(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	scope := "workspace:resolve-test"
	id, err := r.Create(ctx, &types.Interjection{
		Scope: scope, Severity: "warning", Source: "monitor", Reason: "high latency",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	action := &types.ResolutionAction{
		InterjectionID: id,
		Resolution:     "latency returned to normal",
		Action:         "resume",
		ResolvedBy:     "admin",
	}
	if err := r.Resolve(ctx, id, action); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Should no longer appear in active list.
	active, err := r.GetActive(ctx, scope)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active after resolve, got %d", len(active))
	}

	// Should appear in history.
	history, err := r.GetHistory(ctx, scope, 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 in history, got %d", len(history))
	}
	if history[0].Status != "resolved" {
		t.Errorf("expected resolved status, got %q", history[0].Status)
	}
	if history[0].Action != "resume" {
		t.Errorf("expected action resume, got %q", history[0].Action)
	}
}

func TestInterjectionRepo_ResolveNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	err := r.Resolve(ctx, "nonexistent-id", &types.ResolutionAction{
		Resolution: "test",
		Action:     "resume",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent interjection")
	}
}

func TestInterjectionRepo_Expire(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	id, err := r.Create(ctx, &types.Interjection{
		Scope: "workspace:expire", Severity: "warning", Source: "test", Reason: "expire test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.Expire(ctx, id); err != nil {
		t.Fatalf("expire: %v", err)
	}

	ij, err := r.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if ij.Status != "expired" {
		t.Errorf("expected expired, got %q", ij.Status)
	}
}

func TestInterjectionRepo_GetClearanceLevel(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	// Insert a persona with clearance_level=2 directly.
	_, err := db.db.ExecContext(ctx,
		`INSERT INTO personas (id, name, clearance_level) VALUES (?, ?, ?)`,
		"persona-1", "Admin Agent", 2)
	if err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	level, err := r.GetClearanceLevel(ctx, "persona-1")
	if err != nil {
		t.Fatalf("get clearance: %v", err)
	}
	if level != 2 {
		t.Errorf("expected clearance 2, got %d", level)
	}
}

func TestInterjectionRepo_CreateAndGetClearance(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	ij := &types.Interjection{
		Scope:           "workspace:test",
		Severity:        "critical",
		Source:          "agent",
		Reason:          "test with clearance",
		CreatedBy:       "admin",
		SourceClearance: 2,
		TraceID:         "trace-123",
	}

	id, err := r.Create(ctx, ij)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SourceClearance != 2 {
		t.Errorf("expected source_clearance 2, got %d", got.SourceClearance)
	}
	if got.TraceID != "trace-123" {
		t.Errorf("expected trace_id trace-123, got %q", got.TraceID)
	}
}

func TestInterjectionRepo_SieveBypass(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	bypass := &types.SieveBypass{
		Scope:     "workspace:test",
		Pattern:   "*.critical",
		GrantedBy: "admin",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Reason:    "testing",
	}

	id, err := r.CreateBypass(ctx, bypass)
	if err != nil {
		t.Fatalf("create bypass: %v", err)
	}

	active, err := r.GetActiveBypass(ctx, "workspace:test")
	if err != nil {
		t.Fatalf("get active bypasses: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active bypass, got %d", len(active))
	}
	if active[0].Pattern != "*.critical" {
		t.Errorf("expected pattern *.critical, got %q", active[0].Pattern)
	}

	if err := r.RevokeBypass(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	active, _ = r.GetActiveBypass(ctx, "workspace:test")
	if len(active) != 0 {
		t.Errorf("expected 0 active after revoke, got %d", len(active))
	}
}

func TestInterjectionRepo_DLQ(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	entry := &types.DLQEntry{
		InterjectionID: "ij-1",
		MessageType:    "comm.message",
		Payload:        `{"test": true}`,
		Source:         "agent-1",
		Scope:          "workspace:test",
	}

	id, err := r.EnqueueDLQ(ctx, entry)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	count, err := r.CountDLQ(ctx, "ij-1")
	if err != nil {
		t.Fatalf("count dlq: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	entries, err := r.ListDLQ(ctx, "ij-1", 10)
	if err != nil {
		t.Fatalf("list dlq: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].MessageType != "comm.message" {
		t.Errorf("expected message_type comm.message, got %q", entries[0].MessageType)
	}

	if err := r.ReplayDLQ(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}

	count, err = r.CountDLQ(ctx, "ij-1")
	if err != nil {
		t.Fatalf("count dlq: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0 after replay, got %d", count)
	}
}

func TestInterjectionRepo_DLQDismiss(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &InterjectionRepo{db: db.db}

	entry := &types.DLQEntry{
		InterjectionID: "ij-2",
		MessageType:    "pulse.fire",
		Payload:        `{}`,
		Source:         "pulse",
		Scope:          "global",
	}

	id, err := r.EnqueueDLQ(ctx, entry)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := r.DismissDLQ(ctx, id); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	count, err := r.CountDLQ(ctx, "ij-2")
	if err != nil {
		t.Fatalf("count dlq: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 after dismiss, got %d", count)
	}
}
