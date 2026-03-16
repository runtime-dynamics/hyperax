package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func setupTokenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Enable foreign keys.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	// Create minimal agents table for FK reference.
	if _, err := db.Exec(`
		CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			clearance_level INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'idle',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		t.Fatal(err)
	}

	// Create mcp_tokens table.
	if _, err := db.Exec(`
		CREATE TABLE mcp_tokens (
			id              TEXT PRIMARY KEY,
			agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			token_hash      TEXT NOT NULL UNIQUE,
			label           TEXT NOT NULL DEFAULT '',
			clearance_level INTEGER NOT NULL DEFAULT 0,
			scopes          TEXT NOT NULL DEFAULT '[]',
			expires_at      TEXT,
			created_at      TEXT NOT NULL DEFAULT (datetime('now')),
			revoked_at      TEXT
		)
	`); err != nil {
		t.Fatal(err)
	}

	// Insert a test agent.
	if _, err := db.Exec(`INSERT INTO agents (id, name, clearance_level) VALUES ('p1', 'test-agent', 2)`); err != nil {
		t.Fatal(err)
	}

	return db
}

func hashToken(t *testing.T, plaintext string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func TestTokenRepo_CreateAndValidate(t *testing.T) {
	db := setupTokenTestDB(t)
	repo := &TokenRepo{db: db}
	ctx := context.Background()

	plaintext := "test-token-abcdef1234567890abcdef1234567890abcdef1234567890abcdef12"
	hash := hashToken(t, plaintext)

	token := &types.MCPToken{
		ID:             "tok1",
		AgentID:      "p1",
		TokenHash:      hash,
		Label:          "CI token",
		ClearanceLevel: 1,
		Scopes:         []string{"read", "write"},
	}

	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Validate with correct plaintext.
	found, err := repo.ValidateToken(ctx, plaintext)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if found.ID != "tok1" {
		t.Errorf("expected token id tok1, got %s", found.ID)
	}
	if found.AgentID != "p1" {
		t.Errorf("expected agent p1, got %s", found.AgentID)
	}
	if found.ClearanceLevel != 1 {
		t.Errorf("expected clearance 1, got %d", found.ClearanceLevel)
	}
	if len(found.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(found.Scopes))
	}

	// Validate with wrong plaintext.
	_, err = repo.ValidateToken(ctx, "wrong-token")
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
}

func TestTokenRepo_Revoke(t *testing.T) {
	db := setupTokenTestDB(t)
	repo := &TokenRepo{db: db}
	ctx := context.Background()

	plaintext := "revoke-test-token"
	hash := hashToken(t, plaintext)

	token := &types.MCPToken{
		ID:        "tok-rev",
		AgentID: "p1",
		TokenHash: hash,
		Scopes:    []string{},
	}
	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Revoke.
	if err := repo.Revoke(ctx, "tok-rev"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Validate should fail after revocation.
	_, err := repo.ValidateToken(ctx, plaintext)
	if err == nil {
		t.Fatal("expected error after revocation")
	}

	// Double-revoke should fail.
	if err := repo.Revoke(ctx, "tok-rev"); err == nil {
		t.Fatal("expected error on double revoke")
	}
}

func TestTokenRepo_Expiry(t *testing.T) {
	db := setupTokenTestDB(t)
	repo := &TokenRepo{db: db}
	ctx := context.Background()

	plaintext := "expiry-test-token"
	hash := hashToken(t, plaintext)

	past := time.Now().Add(-1 * time.Hour)
	token := &types.MCPToken{
		ID:        "tok-exp",
		AgentID: "p1",
		TokenHash: hash,
		ExpiresAt: &past,
		Scopes:    []string{},
	}
	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Validate should fail for expired token.
	_, err := repo.ValidateToken(ctx, plaintext)
	if err == nil {
		t.Fatal("expected error for expired token")
	}

	// DeleteExpired should remove it.
	n, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
}

func TestTokenRepo_ListByAgent(t *testing.T) {
	db := setupTokenTestDB(t)
	repo := &TokenRepo{db: db}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		hash := hashToken(t, "list-test-"+string(rune('a'+i)))
		token := &types.MCPToken{
			ID:        "tok-list-" + string(rune('a'+i)),
			AgentID: "p1",
			TokenHash: hash,
			Label:     "token " + string(rune('a'+i)),
			Scopes:    []string{},
		}
		if err := repo.Create(ctx, token); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	tokens, err := repo.ListByAgent(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(tokens))
	}
}

func TestTokenRepo_GetByID(t *testing.T) {
	db := setupTokenTestDB(t)
	repo := &TokenRepo{db: db}
	ctx := context.Background()

	hash := hashToken(t, "getbyid-test")
	token := &types.MCPToken{
		ID:             "tok-get",
		AgentID:      "p1",
		TokenHash:      hash,
		Label:          "get test",
		ClearanceLevel: 2,
		Scopes:         []string{"admin"},
	}
	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.GetByID(ctx, "tok-get")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.Label != "get test" {
		t.Errorf("expected label 'get test', got %q", found.Label)
	}
	if found.ClearanceLevel != 2 {
		t.Errorf("expected clearance 2, got %d", found.ClearanceLevel)
	}

	// Not found.
	_, err = repo.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}
