package sqlite

import (
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
)

func TestLifecycleRepo_LogTransition(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	entry := &repo.LifecycleTransition{
		AgentID:   "agent-1",
		FromState: "idle",
		ToState:   "working",
		Reason:    "task assigned",
	}

	if err := r.LogTransition(ctx, entry); err != nil {
		t.Fatalf("log transition: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected non-empty ID after log transition")
	}

	// State should be updated in heartbeats.
	state, err := r.GetState(ctx, "agent-1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state != "working" {
		t.Errorf("state = %q, want %q", state, "working")
	}
}

func TestLifecycleRepo_LogTransitionMultiple(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	// First transition: idle -> working
	if err := r.LogTransition(ctx, &repo.LifecycleTransition{
		AgentID: "agent-1", FromState: "idle", ToState: "working",
	}); err != nil {
		t.Fatalf("transition 1: %v", err)
	}

	// Second transition: working -> paused
	if err := r.LogTransition(ctx, &repo.LifecycleTransition{
		AgentID: "agent-1", FromState: "working", ToState: "paused",
	}); err != nil {
		t.Fatalf("transition 2: %v", err)
	}

	// State should reflect the latest transition.
	state, err := r.GetState(ctx, "agent-1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state != "paused" {
		t.Errorf("state = %q, want %q", state, "paused")
	}
}

func TestLifecycleRepo_GetStateNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	_, err := r.GetState(ctx, "nonexistent-agent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestLifecycleRepo_WriteHeartbeat(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	// WriteHeartbeat on a new agent creates an entry with "idle" state.
	if err := r.WriteHeartbeat(ctx, "agent-hb"); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	state, err := r.GetState(ctx, "agent-hb")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state != "idle" {
		t.Errorf("state = %q, want %q", state, "idle")
	}

	// Transition to working, then heartbeat should preserve state.
	if err := r.LogTransition(ctx, &repo.LifecycleTransition{
		AgentID: "agent-hb", FromState: "idle", ToState: "working",
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}

	if err := r.WriteHeartbeat(ctx, "agent-hb"); err != nil {
		t.Fatalf("write heartbeat 2: %v", err)
	}

	state, err = r.GetState(ctx, "agent-hb")
	if err != nil {
		t.Fatalf("get state after heartbeat: %v", err)
	}
	if state != "working" {
		t.Errorf("state after heartbeat = %q, want %q (should preserve state)", state, "working")
	}
}

func TestLifecycleRepo_GetStaleAgents(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	// Write heartbeat for two agents.
	if err := r.WriteHeartbeat(ctx, "fresh-agent"); err != nil {
		t.Fatalf("heartbeat fresh: %v", err)
	}
	if err := r.WriteHeartbeat(ctx, "stale-agent"); err != nil {
		t.Fatalf("heartbeat stale: %v", err)
	}

	// Manually backdate stale-agent's heartbeat.
	_, err := db.db.ExecContext(ctx,
		`UPDATE agent_heartbeats SET updated_at = datetime('now', '-120 seconds') WHERE agent_id = ?`,
		"stale-agent",
	)
	if err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}

	// With a 60-second TTL, stale-agent should be stale.
	stale, err := r.GetStaleAgents(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("get stale agents: %v", err)
	}

	if len(stale) != 1 {
		t.Fatalf("expected 1 stale agent, got %d", len(stale))
	}
	if stale[0] != "stale-agent" {
		t.Errorf("stale agent = %q, want %q", stale[0], "stale-agent")
	}
}

func TestLifecycleRepo_GetStaleAgentsEmpty(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &LifecycleRepo{db: db.db}

	// No agents registered.
	stale, err := r.GetStaleAgents(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("get stale agents: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("expected 0 stale agents, got %d", len(stale))
	}
}
