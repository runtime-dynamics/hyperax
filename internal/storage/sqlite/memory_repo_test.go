package sqlite

import (
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestMemoryRepo_Store(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	mem := &types.Memory{
		Scope:       types.MemoryScopeProject,
		Type:        types.MemoryTypeEpisodic,
		Content:     "The user prefers dark mode.",
		WorkspaceID: "ws-1",
		Metadata:    map[string]any{"tags": []string{"preference", "ui"}},
	}

	id, err := r.Store(ctx, mem)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestMemoryRepo_Get(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	mem := &types.Memory{
		Scope:       types.MemoryScopeGlobal,
		Type:        types.MemoryTypeSemantic,
		Content:     "Always use Go 1.22+",
		Metadata:    map[string]any{"source": "human"},
	}

	id, err := r.Store(ctx, mem)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != mem.Content {
		t.Errorf("content = %q, want %q", got.Content, mem.Content)
	}
	if got.Scope != types.MemoryScopeGlobal {
		t.Errorf("scope = %q, want %q", got.Scope, types.MemoryScopeGlobal)
	}
	if got.Type != types.MemoryTypeSemantic {
		t.Errorf("type = %q, want %q", got.Type, types.MemoryTypeSemantic)
	}
	if got.AccessCount != 0 {
		t.Errorf("access_count = %d, want 0", got.AccessCount)
	}
}

func TestMemoryRepo_GetNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	_, err := r.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for missing memory")
	}
}

func TestMemoryRepo_Delete(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	id, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic, Content: "to be deleted",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := r.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = r.Get(ctx, id)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestMemoryRepo_Recall(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	_, _ = r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic,
		Content: "User likes Go.", WorkspaceID: "ws-1",
	})
	_, _ = r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic,
		Content: "User prefers Rust over Python.", WorkspaceID: "ws-1",
	})
	_, _ = r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeSemantic,
		Content: "User likes Go a lot.",
	})

	// Recall from project scope for "Go".
	results, err := r.Recall(ctx, "Go", types.MemoryScopeProject, "ws-1", "", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for project/Go, got %d", len(results))
	}
	if results[0].Content != "User likes Go." {
		t.Errorf("content = %q, want %q", results[0].Content, "User likes Go.")
	}
}

func TestMemoryRepo_RecallWithLimit(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	for i := 0; i < 5; i++ {
		_, _ = r.Store(ctx, &types.Memory{
			Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic,
			Content: "entry with data", WorkspaceID: "ws-1",
		})
	}

	results, err := r.Recall(ctx, "data", types.MemoryScopeProject, "ws-1", "", 3)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (limit), got %d", len(results))
	}
}

func TestMemoryRepo_RecallEmpty(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	results, err := r.Recall(ctx, "nothing", types.MemoryScopeProject, "ws-1", "", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestMemoryRepo_RecallExcludesConsolidated(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	id, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic,
		Content: "old episodic fact", WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Mark as consolidated.
	_ = r.MarkConsolidated(ctx, []string{id}, "target-id")

	results, err := r.Recall(ctx, "old", types.MemoryScopeProject, "ws-1", "", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (consolidated excluded), got %d", len(results))
	}
}

func TestMemoryRepo_RecallExcludesContested(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	id, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeSemantic,
		Content: "contested fact", WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	_ = r.MarkContested(ctx, id, "other-memory-id")

	results, err := r.Recall(ctx, "contested", types.MemoryScopeProject, "ws-1", "", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (contested excluded), got %d", len(results))
	}
}

func TestMemoryRepo_TouchAccess(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	id, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeProcedural,
		Content: "important procedure",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Touch access twice.
	_ = r.TouchAccess(ctx, id)
	_ = r.TouchAccess(ctx, id)

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AccessCount != 2 {
		t.Errorf("access_count = %d, want 2", got.AccessCount)
	}
}

func TestMemoryRepo_Count(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeEpisodic, Content: "a"})
	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeSemantic, Content: "b"})
	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic, Content: "c", WorkspaceID: "ws-1"})

	count, err := r.Count(ctx, types.MemoryScopeGlobal, "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("global count = %d, want 2", count)
	}
}

func TestMemoryRepo_CountByType(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeEpisodic, Content: "a"})
	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeEpisodic, Content: "b"})
	_, _ = r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeSemantic, Content: "c"})

	byType, err := r.CountByType(ctx, types.MemoryScopeGlobal, "")
	if err != nil {
		t.Fatalf("count by type: %v", err)
	}
	if byType[types.MemoryTypeEpisodic] != 2 {
		t.Errorf("episodic = %d, want 2", byType[types.MemoryTypeEpisodic])
	}
	if byType[types.MemoryTypeSemantic] != 1 {
		t.Errorf("semantic = %d, want 1", byType[types.MemoryTypeSemantic])
	}
}

func TestMemoryRepo_Annotations(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	memID, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeSemantic, Content: "fact",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	ann := &types.MemoryAnnotation{
		MemoryID:       memID,
		Annotation:     "This fact was updated in v2.0",
		AnnotationType: "correction",
		CreatedBy:      "agent-1",
	}

	annID, err := r.StoreAnnotation(ctx, ann)
	if err != nil {
		t.Fatalf("store annotation: %v", err)
	}
	if annID == "" {
		t.Fatal("expected non-empty annotation ID")
	}

	anns, err := r.GetAnnotations(ctx, memID)
	if err != nil {
		t.Fatalf("get annotations: %v", err)
	}
	if len(anns) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(anns))
	}
	if anns[0].Annotation != "This fact was updated in v2.0" {
		t.Errorf("annotation = %q, want %q", anns[0].Annotation, "This fact was updated in v2.0")
	}
	if anns[0].AnnotationType != "correction" {
		t.Errorf("type = %q, want %q", anns[0].AnnotationType, "correction")
	}
}

func TestMemoryRepo_ListConsolidationCandidates(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	// Store a memory and manually set old accessed_at.
	id, err := r.Store(ctx, &types.Memory{
		Scope: types.MemoryScopeProject, Type: types.MemoryTypeEpisodic,
		Content: "old fact", WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Force the accessed_at to be old.
	_, _ = r.db.ExecContext(ctx,
		`UPDATE memories SET accessed_at = datetime('now', '-60 days') WHERE id = ?`, id)

	candidates, err := r.ListConsolidationCandidates(ctx, types.MemoryScopeProject, 30, 100)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
}

func TestMemoryRepo_MarkConsolidated(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MemoryRepo{db: db.db}

	id1, err := r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeEpisodic, Content: "a"})
	if err != nil {
		t.Fatalf("store 1: %v", err)
	}
	id2, err := r.Store(ctx, &types.Memory{Scope: types.MemoryScopeGlobal, Type: types.MemoryTypeEpisodic, Content: "b"})
	if err != nil {
		t.Fatalf("store 2: %v", err)
	}

	err = r.MarkConsolidated(ctx, []string{id1, id2}, "target-id")
	if err != nil {
		t.Fatalf("mark consolidated: %v", err)
	}

	got1, err := r.Get(ctx, id1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got1.ConsolidatedInto != "target-id" {
		t.Errorf("consolidated_into = %q, want %q", got1.ConsolidatedInto, "target-id")
	}
}
