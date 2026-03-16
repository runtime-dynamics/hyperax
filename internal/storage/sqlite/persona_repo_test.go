package sqlite

import (
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

func TestPersonaRepo_Create(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	persona := &repo.Persona{
		Name:           "backend-guardian",
		Description:    "Reviews backend code",
		SystemPrompt:   "You are the backend guardian.",
		Team:           "engineering",
		Role:           "reviewer",
		ClearanceLevel: 3,
		IsActive:       true,
	}

	id, err := r.Create(ctx, persona)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}
}

func TestPersonaRepo_Get(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	persona := &repo.Persona{
		Name:           "planner",
		Description:    "Plans work",
		SystemPrompt:   "You plan tasks.",
		Team:           "pm",
		Role:           "lead",
		ClearanceLevel: 5,
		IsActive:       true,
	}

	id, err := r.Create(ctx, persona)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "planner" {
		t.Errorf("name = %q, want %q", got.Name, "planner")
	}
	if got.Team != "pm" {
		t.Errorf("team = %q, want %q", got.Team, "pm")
	}
	if got.ClearanceLevel != 5 {
		t.Errorf("clearance = %d, want %d", got.ClearanceLevel, 5)
	}
	if !got.IsActive {
		t.Error("expected is_active = true")
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestPersonaRepo_GetNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	_, err := r.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent persona")
	}
}

func TestPersonaRepo_List(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	// Empty initially
	list, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}

	// Add two personas
	_, err = r.Create(ctx, &repo.Persona{Name: "alpha", IsActive: true})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	_, err = r.Create(ctx, &repo.Persona{Name: "beta", IsActive: true})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}

	list, err = r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}

	// Should be ordered by name
	if list[0].Name != "alpha" {
		t.Errorf("first = %q, want %q", list[0].Name, "alpha")
	}
	if list[1].Name != "beta" {
		t.Errorf("second = %q, want %q", list[1].Name, "beta")
	}
}

func TestPersonaRepo_Update(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	id, err := r.Create(ctx, &repo.Persona{
		Name:           "original",
		Description:    "original desc",
		ClearanceLevel: 1,
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated := &repo.Persona{
		Name:           "renamed",
		Description:    "updated desc",
		SystemPrompt:   "new prompt",
		Team:           "new-team",
		Role:           "new-role",
		ClearanceLevel: 10,
		IsActive:       false,
	}
	if err := r.Update(ctx, id, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "renamed" {
		t.Errorf("name = %q, want %q", got.Name, "renamed")
	}
	if got.Description != "updated desc" {
		t.Errorf("description = %q, want %q", got.Description, "updated desc")
	}
	if got.SystemPrompt != "new prompt" {
		t.Errorf("system_prompt = %q, want %q", got.SystemPrompt, "new prompt")
	}
	if got.Team != "new-team" {
		t.Errorf("team = %q, want %q", got.Team, "new-team")
	}
	if got.ClearanceLevel != 10 {
		t.Errorf("clearance = %d, want %d", got.ClearanceLevel, 10)
	}
	if got.IsActive {
		t.Error("expected is_active = false")
	}
}

func TestPersonaRepo_UpdateNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	err := r.Update(ctx, "nonexistent", &repo.Persona{Name: "x"})
	if err == nil {
		t.Error("expected error for nonexistent persona update")
	}
}

func TestPersonaRepo_Delete(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	id, err := r.Create(ctx, &repo.Persona{Name: "doomed", IsActive: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should no longer be retrievable
	_, err = r.Get(ctx, id)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestPersonaRepo_DeleteNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &PersonaRepo{db: db.db}

	err := r.Delete(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent persona delete")
	}
}
