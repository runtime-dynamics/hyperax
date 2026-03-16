package sqlite

import (
	"testing"
)

func TestSecretRepo_SetAndGet(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	if err := r.Set(ctx, "api_key", "sk-12345", "global"); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := r.Get(ctx, "api_key", "global")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "sk-12345" {
		t.Errorf("value = %q, want %q", val, "sk-12345")
	}
}

func TestSecretRepo_SetOverwrite(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	if err := r.Set(ctx, "token", "old-value", "global"); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := r.Set(ctx, "token", "new-value", "global"); err != nil {
		t.Fatalf("set second: %v", err)
	}

	val, err := r.Get(ctx, "token", "global")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "new-value" {
		t.Errorf("value = %q, want %q", val, "new-value")
	}
}

func TestSecretRepo_ScopedSecrets(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	// Same key, different scopes.
	if err := r.Set(ctx, "db_password", "global-pass", "global"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := r.Set(ctx, "db_password", "agent-pass", "agent:agent-1"); err != nil {
		t.Fatalf("set agent: %v", err)
	}

	globalVal, err := r.Get(ctx, "db_password", "global")
	if err != nil {
		t.Fatalf("get global: %v", err)
	}
	if globalVal != "global-pass" {
		t.Errorf("global value = %q, want %q", globalVal, "global-pass")
	}

	agentVal, err := r.Get(ctx, "db_password", "agent:agent-1")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agentVal != "agent-pass" {
		t.Errorf("agent value = %q, want %q", agentVal, "agent-pass")
	}
}

func TestSecretRepo_GetNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	_, err := r.Get(ctx, "nonexistent", "global")
	if err == nil {
		t.Fatal("expected error for nonexistent secret")
	}
}

func TestSecretRepo_Delete(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	if err := r.Set(ctx, "temp_key", "temp_val", "global"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := r.Delete(ctx, "temp_key", "global"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should no longer exist.
	_, err := r.Get(ctx, "temp_key", "global")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestSecretRepo_DeleteNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	err := r.Delete(ctx, "nonexistent", "global")
	if err == nil {
		t.Fatal("expected error for nonexistent secret delete")
	}
}

func TestSecretRepo_List(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SecretRepo{db: db.db}

	// Empty initially.
	keys, err := r.List(ctx, "global")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}

	// Add secrets in global scope.
	if err := r.Set(ctx, "z_key", "val", "global"); err != nil {
		t.Fatalf("set z: %v", err)
	}
	if err := r.Set(ctx, "a_key", "val", "global"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	// Add one in a different scope (should not appear).
	if err := r.Set(ctx, "other_key", "val", "agent:x"); err != nil {
		t.Fatalf("set other: %v", err)
	}

	keys, err = r.List(ctx, "global")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// Should be ordered alphabetically.
	if keys[0] != "a_key" {
		t.Errorf("first key = %q, want %q", keys[0], "a_key")
	}
	if keys[1] != "z_key" {
		t.Errorf("second key = %q, want %q", keys[1], "z_key")
	}
}
