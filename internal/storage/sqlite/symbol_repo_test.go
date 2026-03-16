package sqlite

import (
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// ---------- UpsertFileHash ----------

func TestSymbolRepo_UpsertFileHash(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	fileID, err := r.UpsertFileHash(ctx, "ws-1", "main.go", "abc123")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if fileID == 0 {
		t.Error("expected non-zero file_id")
	}
}

func TestSymbolRepo_UpsertFileHash_Update(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	id1, err := r.UpsertFileHash(ctx, "ws-1", "main.go", "hash-v1")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	id2, err := r.UpsertFileHash(ctx, "ws-1", "main.go", "hash-v2")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if id1 != id2 {
		t.Errorf("file_id changed on upsert: %d != %d", id1, id2)
	}

	hash, err := r.GetFileHash(ctx, "ws-1", "main.go")
	if err != nil {
		t.Fatalf("get hash: %v", err)
	}
	if hash != "hash-v2" {
		t.Errorf("hash = %q, want %q", hash, "hash-v2")
	}
}

func TestSymbolRepo_UpsertFileHash_DifferentWorkspaces(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	id1, err := r.UpsertFileHash(ctx, "ws-a", "main.go", "aaa")
	if err != nil {
		t.Fatalf("upsert ws-a: %v", err)
	}

	id2, err := r.UpsertFileHash(ctx, "ws-b", "main.go", "bbb")
	if err != nil {
		t.Fatalf("upsert ws-b: %v", err)
	}

	if id1 == id2 {
		t.Error("same file_id for different workspaces")
	}
}

// ---------- GetFileHash ----------

func TestSymbolRepo_GetFileHash_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	_, err := r.GetFileHash(ctx, "ws-1", "nonexistent.go")
	if err == nil {
		t.Error("expected error for missing file hash")
	}
}

// ---------- Upsert (Symbol) ----------

func TestSymbolRepo_Upsert(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	fileID, err := r.UpsertFileHash(ctx, "ws-1", "main.go", "abc")
	if err != nil {
		t.Fatalf("upsert file hash: %v", err)
	}

	sym := &repo.Symbol{
		FileID:      fileID,
		WorkspaceID: "ws-1",
		Name:        "main",
		Kind:        "function",
		StartLine:   1,
		EndLine:     10,
		Signature:   "func main()",
	}

	if err := r.Upsert(ctx, sym); err != nil {
		t.Fatalf("upsert symbol: %v", err)
	}

	symbols, err := r.GetFileSymbols(ctx, "ws-1", "main.go")
	if err != nil {
		t.Fatalf("get file symbols: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "main" {
		t.Errorf("name = %q, want %q", symbols[0].Name, "main")
	}
	if symbols[0].Kind != "function" {
		t.Errorf("kind = %q, want %q", symbols[0].Kind, "function")
	}
	if symbols[0].Signature != "func main()" {
		t.Errorf("signature = %q, want %q", symbols[0].Signature, "func main()")
	}
}

// ---------- GetFileSymbols ----------

func TestSymbolRepo_GetFileSymbols_Empty(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	symbols, err := r.GetFileSymbols(ctx, "ws-1", "nonexistent.go")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(symbols))
	}
}

func TestSymbolRepo_GetFileSymbols_OrderedByLine(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	fileID, _ := r.UpsertFileHash(ctx, "ws-1", "server.go", "hash1")

	// Insert in reverse order to verify ORDER BY start_line.
	for _, s := range []*repo.Symbol{
		{FileID: fileID, WorkspaceID: "ws-1", Name: "Stop", Kind: "method", StartLine: 50, EndLine: 60},
		{FileID: fileID, WorkspaceID: "ws-1", Name: "Server", Kind: "struct", StartLine: 10, EndLine: 15},
		{FileID: fileID, WorkspaceID: "ws-1", Name: "Start", Kind: "method", StartLine: 20, EndLine: 40},
	} {
		if err := r.Upsert(ctx, s); err != nil {
			t.Fatalf("upsert %s: %v", s.Name, err)
		}
	}

	symbols, err := r.GetFileSymbols(ctx, "ws-1", "server.go")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("expected 3, got %d", len(symbols))
	}

	if symbols[0].Name != "Server" {
		t.Errorf("first symbol = %q, want Server", symbols[0].Name)
	}
	if symbols[1].Name != "Start" {
		t.Errorf("second symbol = %q, want Start", symbols[1].Name)
	}
	if symbols[2].Name != "Stop" {
		t.Errorf("third symbol = %q, want Stop", symbols[2].Name)
	}
}

// ---------- DeleteByFile ----------

func TestSymbolRepo_DeleteByFile(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	fileID, _ := r.UpsertFileHash(ctx, "ws-1", "old.go", "hash")
	_ = r.Upsert(ctx, &repo.Symbol{FileID: fileID, WorkspaceID: "ws-1", Name: "Foo", Kind: "function", StartLine: 1, EndLine: 5})
	_ = r.Upsert(ctx, &repo.Symbol{FileID: fileID, WorkspaceID: "ws-1", Name: "Bar", Kind: "function", StartLine: 10, EndLine: 15})

	if err := r.DeleteByFile(ctx, fileID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	symbols, err := r.GetFileSymbols(ctx, "ws-1", "old.go")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols after delete, got %d", len(symbols))
	}
}

func TestSymbolRepo_DeleteByFile_NoEffect(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SymbolRepo{db: db.db}

	// Deleting symbols for a non-existent file_id should not error.
	if err := r.DeleteByFile(ctx, 99999); err != nil {
		t.Fatalf("delete non-existent: %v", err)
	}
}
