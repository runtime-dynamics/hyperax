package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// setupNervousTestDB opens a test DB, runs all standard migrations, then
// manually applies the 006_nervous migration (which is not yet wired into
// the main Migrate path). This allows nervous repo tests to run without
// modifying db.go.
func setupNervousTestDB(t *testing.T) (*NervousRepo, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)

	// Apply the 006_nervous migration that creates domain_events and event_handlers.
	data, err := migrationsFS.ReadFile("migrations/006_nervous.up.sql")
	if err != nil {
		t.Fatalf("read migration 006: %v", err)
	}
	_, err = db.db.ExecContext(ctx, string(data))
	if err != nil {
		t.Fatalf("exec migration 006: %v", err)
	}

	return &NervousRepo{db: db.db}, ctx
}

// --------------------------------------------------------------------------
// Domain events
// --------------------------------------------------------------------------

func TestNervousRepo_PersistEvent(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	event := &types.DomainEvent{
		EventType:  "pipeline.start",
		Source:     "test",
		Scope:      "global",
		Payload:    `{"name":"build"}`,
		TraceID:    "trace-001",
		SequenceID: 42,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(7 * 24 * time.Hour),
	}

	if err := r.PersistEvent(ctx, event); err != nil {
		t.Fatalf("persist: %v", err)
	}

	if event.ID == "" {
		t.Error("expected non-empty ID after persist")
	}
}

func TestNervousRepo_QueryEvents_All(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	now := time.Now()
	for i := 0; i < 3; i++ {
		event := &types.DomainEvent{
			EventType: "pipeline.start",
			Source:    "test",
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		}
		if err := r.PersistEvent(ctx, event); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}

	events, err := r.QueryEvents(ctx, "", time.Time{}, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestNervousRepo_QueryEvents_ByType(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	now := time.Now()
	for _, et := range []types.EventType{"pipeline.start", "pipeline.start", "cron.fire"} {
		event := &types.DomainEvent{
			EventType: et,
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		}
		if err := r.PersistEvent(ctx, event); err != nil {
			t.Fatalf("persist: %v", err)
		}
	}

	events, err := r.QueryEvents(ctx, "pipeline.start", time.Time{}, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 pipeline.start events, got %d", len(events))
	}
}

func TestNervousRepo_QueryEvents_WithLimit(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	now := time.Now()
	for i := 0; i < 10; i++ {
		event := &types.DomainEvent{
			EventType: "test.event",
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		}
		if err := r.PersistEvent(ctx, event); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}

	events, err := r.QueryEvents(ctx, "", time.Time{}, 3)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 (limited), got %d", len(events))
	}
}

func TestNervousRepo_PurgeExpired(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	now := time.Now()

	// One expired event.
	expired := &types.DomainEvent{
		EventType: "old.event",
		CreatedAt: now.Add(-8 * 24 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour), // already expired
	}
	if err := r.PersistEvent(ctx, expired); err != nil {
		t.Fatalf("persist expired: %v", err)
	}

	// One active event.
	active := &types.DomainEvent{
		EventType: "new.event",
		CreatedAt: now,
		ExpiresAt: now.Add(7 * 24 * time.Hour),
	}
	if err := r.PersistEvent(ctx, active); err != nil {
		t.Fatalf("persist active: %v", err)
	}

	count, err := r.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if count != 1 {
		t.Errorf("purged %d, want 1", count)
	}

	// Only the active event should remain.
	events, err := r.QueryEvents(ctx, "", time.Time{}, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 remaining event, got %d", len(events))
	}
}

func TestNervousRepo_GetEventStats(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	now := time.Now()
	types_ := []types.EventType{"pipeline.start", "pipeline.start", "cron.fire", "cron.fire", "cron.fire"}
	for _, et := range types_ {
		event := &types.DomainEvent{
			EventType: et,
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		}
		if err := r.PersistEvent(ctx, event); err != nil {
			t.Fatalf("persist: %v", err)
		}
	}

	stats, err := r.GetEventStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats["pipeline.start"] != 2 {
		t.Errorf("pipeline.start = %d, want 2", stats["pipeline.start"])
	}
	if stats["cron.fire"] != 3 {
		t.Errorf("cron.fire = %d, want 3", stats["cron.fire"])
	}
}

func TestNervousRepo_GetEventStats_Empty(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	stats, err := r.GetEventStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d entries", len(stats))
	}
}

// --------------------------------------------------------------------------
// Event handlers
// --------------------------------------------------------------------------

func TestNervousRepo_CreateHandler(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	handler := &types.EventHandler{
		Name:          "log-pipelines",
		EventFilter:   "pipeline.*",
		Action:        "log",
		ActionPayload: `{"level":"info"}`,
		Enabled:       true,
	}

	id, err := r.CreateHandler(ctx, handler)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}
}

func TestNervousRepo_GetHandler(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	handler := &types.EventHandler{
		Name:        "test-handler",
		EventFilter: "cron.*",
		Action:      "webhook",
		Enabled:     true,
	}

	id, err := r.CreateHandler(ctx, handler)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.GetHandler(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "test-handler" {
		t.Errorf("name = %q, want %q", got.Name, "test-handler")
	}
	if got.EventFilter != "cron.*" {
		t.Errorf("event_filter = %q, want %q", got.EventFilter, "cron.*")
	}
	if got.Action != "webhook" {
		t.Errorf("action = %q, want %q", got.Action, "webhook")
	}
	if !got.Enabled {
		t.Error("expected enabled = true")
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestNervousRepo_GetHandler_NotFound(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	_, err := r.GetHandler(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent handler")
	}
}

func TestNervousRepo_ListHandlers(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	// Empty initially.
	list, err := r.ListHandlers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}

	// Create two handlers.
	_, err = r.CreateHandler(ctx, &types.EventHandler{
		Name: "alpha", EventFilter: "*", Action: "log", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	_, err = r.CreateHandler(ctx, &types.EventHandler{
		Name: "beta", EventFilter: "pipeline.*", Action: "webhook", Enabled: false,
	})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}

	list, err = r.ListHandlers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}

	// Should be ordered by name.
	if list[0].Name != "alpha" {
		t.Errorf("first = %q, want alpha", list[0].Name)
	}
	if list[1].Name != "beta" {
		t.Errorf("second = %q, want beta", list[1].Name)
	}
}

func TestNervousRepo_UpdateHandler(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	id, err := r.CreateHandler(ctx, &types.EventHandler{
		Name:        "original",
		EventFilter: "*",
		Action:      "log",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated := &types.EventHandler{
		ID:            id,
		Name:          "renamed",
		EventFilter:   "pipeline.*",
		Action:        "webhook",
		ActionPayload: `{"url":"http://example.com"}`,
		Enabled:       false,
	}
	if err := r.UpdateHandler(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.GetHandler(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "renamed" {
		t.Errorf("name = %q, want renamed", got.Name)
	}
	if got.EventFilter != "pipeline.*" {
		t.Errorf("event_filter = %q, want pipeline.*", got.EventFilter)
	}
	if got.Action != "webhook" {
		t.Errorf("action = %q, want webhook", got.Action)
	}
	if got.Enabled {
		t.Error("expected enabled = false")
	}
}

func TestNervousRepo_UpdateHandler_NotFound(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	err := r.UpdateHandler(ctx, &types.EventHandler{
		ID:   "nonexistent",
		Name: "x",
	})
	if err == nil {
		t.Error("expected error for nonexistent handler update")
	}
}

func TestNervousRepo_DeleteHandler(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	id, err := r.CreateHandler(ctx, &types.EventHandler{
		Name: "doomed", EventFilter: "*", Action: "log", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.DeleteHandler(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should no longer be retrievable.
	_, err = r.GetHandler(ctx, id)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestNervousRepo_DeleteHandler_NotFound(t *testing.T) {
	r, ctx := setupNervousTestDB(t)

	err := r.DeleteHandler(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent handler delete")
	}
}
