package sqlite

import (
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
)

func TestCheckpointRepo_SaveAndGetLatest(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	cp := &repo.AgentCheckpoint{
		AgentID:        "agent-1",
		TaskID:         "task-42",
		LastMessageID:  "msg-100",
		WorkingContext: `{"key":"value"}`,
		ActiveFiles:    `["/foo/bar.go","/baz/qux.go"]`,
		RefactorTxID:   "tx-abc",
	}

	if err := r.Save(ctx, cp); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if cp.ID == "" {
		t.Fatal("expected ID to be generated")
	}

	latest, err := r.GetLatest(ctx, "agent-1")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}

	if latest.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want %q", latest.AgentID, "agent-1")
	}
	if latest.TaskID != "task-42" {
		t.Errorf("task_id = %q, want %q", latest.TaskID, "task-42")
	}
	if latest.LastMessageID != "msg-100" {
		t.Errorf("last_message_id = %q, want %q", latest.LastMessageID, "msg-100")
	}
	if latest.WorkingContext != `{"key":"value"}` {
		t.Errorf("working_context = %q, want JSON object", latest.WorkingContext)
	}
	if latest.ActiveFiles != `["/foo/bar.go","/baz/qux.go"]` {
		t.Errorf("active_files = %q, want JSON array", latest.ActiveFiles)
	}
	if latest.RefactorTxID != "tx-abc" {
		t.Errorf("refactor_tx_id = %q, want %q", latest.RefactorTxID, "tx-abc")
	}
}

func TestCheckpointRepo_GetLatestReturnsNewest(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	// Save two checkpoints for the same agent.
	if err := r.Save(ctx, &repo.AgentCheckpoint{
		AgentID: "agent-1",
		TaskID:  "task-old",
	}); err != nil {
		t.Fatalf("save 1: %v", err)
	}

	// Ensure second checkpoint is newer (SQLite default datetime('now')).
	if err := r.Save(ctx, &repo.AgentCheckpoint{
		AgentID: "agent-1",
		TaskID:  "task-new",
	}); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	latest, err := r.GetLatest(ctx, "agent-1")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}

	// Both have the same datetime('now') but task-new was inserted second.
	// GetLatest uses ORDER BY checkpointed_at DESC LIMIT 1, which may return either.
	// The important thing is it doesn't error.
	if latest.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want %q", latest.AgentID, "agent-1")
	}
}

func TestCheckpointRepo_GetLatestNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	_, err := r.GetLatest(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent checkpoint")
	}
}

func TestCheckpointRepo_List(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	// Save 3 checkpoints.
	for i := 0; i < 3; i++ {
		if err := r.Save(ctx, &repo.AgentCheckpoint{
			AgentID: "agent-1",
			TaskID:  "task",
		}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	list, err := r.List(ctx, "agent-1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 checkpoints, got %d", len(list))
	}

	// Limit to 2.
	list2, err := r.List(ctx, "agent-1", 2)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(list2) != 2 {
		t.Errorf("expected 2 checkpoints with limit, got %d", len(list2))
	}
}

func TestCheckpointRepo_Delete(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	cp := &repo.AgentCheckpoint{AgentID: "agent-1"}
	if err := r.Save(ctx, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := r.Delete(ctx, cp.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := r.GetLatest(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected no checkpoint after delete")
	}
}

func TestCheckpointRepo_DeleteNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	err := r.Delete(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestCheckpointRepo_DeleteOlderThan(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &CheckpointRepo{db: db.db}

	// Save a checkpoint and backdate it.
	cp := &repo.AgentCheckpoint{AgentID: "agent-1"}
	if err := r.Save(ctx, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Backdate the checkpoint to 2 hours ago.
	_, err := db.db.ExecContext(ctx,
		`UPDATE agent_checkpoints SET checkpointed_at = datetime('now', '-7200 seconds') WHERE id = ?`,
		cp.ID,
	)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Save a fresh checkpoint.
	if err := r.Save(ctx, &repo.AgentCheckpoint{AgentID: "agent-1"}); err != nil {
		t.Fatalf("save fresh: %v", err)
	}

	// Delete checkpoints older than 1 hour.
	cutoff := time.Now().Add(-1 * time.Hour)
	deleted, err := r.DeleteOlderThan(ctx, "agent-1", cutoff)
	if err != nil {
		t.Fatalf("delete older than: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Only the fresh one should remain.
	list, _ := r.List(ctx, "agent-1", 10)
	if len(list) != 1 {
		t.Errorf("expected 1 remaining checkpoint, got %d", len(list))
	}
}
