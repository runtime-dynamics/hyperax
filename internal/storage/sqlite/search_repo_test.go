package sqlite

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// ---------- helper ----------

// seedSymbols inserts file_hashes and symbols for testing search queries.
func seedSymbols(t *testing.T, ctx context.Context, symRepo *SymbolRepo) {
	t.Helper()

	fileID1, err := symRepo.UpsertFileHash(ctx, "ws-1", "main.go", "h1")
	if err != nil {
		t.Fatalf("upsert file hash main.go: %v", err)
	}
	fileID2, err := symRepo.UpsertFileHash(ctx, "ws-1", "server.go", "h2")
	if err != nil {
		t.Fatalf("upsert file hash server.go: %v", err)
	}

	for _, s := range []*repo.Symbol{
		{FileID: fileID1, WorkspaceID: "ws-1", Name: "main", Kind: "function", StartLine: 1, EndLine: 5, Signature: "func main()"},
		{FileID: fileID1, WorkspaceID: "ws-1", Name: "init", Kind: "function", StartLine: 7, EndLine: 10},
		{FileID: fileID2, WorkspaceID: "ws-1", Name: "Server", Kind: "struct", StartLine: 1, EndLine: 8, Signature: "type Server struct"},
		{FileID: fileID2, WorkspaceID: "ws-1", Name: "StartServer", Kind: "function", StartLine: 10, EndLine: 30},
	} {
		if err := symRepo.Upsert(ctx, s); err != nil {
			t.Fatalf("upsert %s: %v", s.Name, err)
		}
	}
}

// seedDocChunks inserts documentation chunks for testing.
func seedDocChunks(t *testing.T, ctx context.Context, r *SearchRepo) {
	t.Helper()
	for _, c := range []*repo.DocChunk{
		{WorkspaceID: "ws-1", FilePath: "docs/arch.md", FileHash: "h1", ChunkIndex: 0, SectionHeader: "Architecture", Content: "The system uses a layered architecture.", TokenCount: 7},
		{WorkspaceID: "ws-1", FilePath: "docs/arch.md", FileHash: "h1", ChunkIndex: 1, SectionHeader: "Data Flow", Content: "Data flows from ingestion to storage.", TokenCount: 7},
		{WorkspaceID: "ws-1", FilePath: "docs/guide.md", FileHash: "h2", ChunkIndex: 0, SectionHeader: "User Guide", Content: "This guide covers common workflows.", TokenCount: 6},
	} {
		if err := r.UpsertDocChunk(ctx, c); err != nil {
			t.Fatalf("upsert %s/%d: %v", c.FilePath, c.ChunkIndex, err)
		}
	}
}

// ---------- sanitizeFTSQuery ----------

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single word", input: "Server", want: "Server*"},
		{name: "two words", input: "Start Server", want: "Start* Server*"},
		{name: "special chars stripped", input: `foo+bar-baz"qux`, want: "foo* bar* baz* qux*"},
		{name: "parentheses and colons", input: "name:(value)", want: "name* value*"},
		{name: "asterisks removed", input: "func*", want: "func*"},
		{name: "only specials", input: "+-:\"()", want: ""},
		{name: "empty string", input: "", want: ""},
		{name: "underscore preserved", input: "my_func", want: "my_func*"},
		{name: "extra spaces collapsed", input: "  foo   bar  ", want: "foo* bar*"},
		{name: "carets and braces", input: "^test{}", want: "test*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTSQuery(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------- FTS5 SearchSymbols ----------

func TestSearchRepo_SearchSymbols_FTS(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.SearchSymbols(ctx, []string{"ws-1"}, "Server", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result for 'Server', got %d", len(results))
	}
	// FTS5 should have populated Score.
	for _, sym := range results {
		if sym.Score == 0 {
			// Score might be zero if FTS is not available; only fail if FTS was detected.
			if r.ftsAvailable != nil && *r.ftsAvailable {
				t.Errorf("expected non-zero BM25 score for %q via FTS", sym.Name)
			}
		}
	}
}

func TestSearchRepo_SearchSymbols_FTS_RankOrder(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}

	// "Server" should rank the exact name match (Server) highest.
	// FTS5 tokenizes by word boundaries so "StartServer" may not match
	// a prefix query for "Server" — that's expected. LIKE fallback handles
	// substring matching; FTS5 handles ranked full-word matches.
	results, err := r.SearchSymbols(ctx, []string{"ws-1"}, "Server", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 1 {
		t.Fatalf("expected >=1 results, got %d", len(results))
	}
	// The first result should be "Server" (exact match in name column).
	if results[0].Name != "Server" {
		t.Errorf("expected best-ranked result to be 'Server', got %q", results[0].Name)
	}
}

func TestSearchRepo_SearchSymbols_FTS_KindFilter(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.SearchSymbols(ctx, []string{"ws-1"}, "Server", "struct", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 struct result, got %d", len(results))
	}
	if results[0].Name != "Server" {
		t.Errorf("name = %q, want %q", results[0].Name, "Server")
	}
}

// ---------- LIKE fallback SearchSymbols ----------

func TestSearchRepo_SearchSymbols_LIKE(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.searchSymbolsLIKE(ctx, []string{"ws-1"}, "Server", "", 10)
	if err != nil {
		t.Fatalf("like search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (Server, StartServer), got %d", len(results))
	}
	// LIKE results should have Score == 0 (no ranking).
	for _, sym := range results {
		if sym.Score != 0 {
			t.Errorf("expected Score=0 for LIKE result %q, got %f", sym.Name, sym.Score)
		}
	}
}

func TestSearchRepo_SearchSymbols_LIKE_KindFilter(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.searchSymbolsLIKE(ctx, []string{"ws-1"}, "Server", "struct", 10)
	if err != nil {
		t.Fatalf("like search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 struct result, got %d", len(results))
	}
}

func TestSearchRepo_SearchSymbols_EmptyWorkspaces(t *testing.T) {
	db, ctx := setupTestDB(t)
	_ = db
	r := &SearchRepo{db: db.db}

	results, err := r.SearchSymbols(ctx, nil, "anything", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty workspace list, got %v", results)
	}
}

func TestSearchRepo_SearchSymbols_Limit(t *testing.T) {
	db, ctx := setupTestDB(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.SearchSymbols(ctx, []string{"ws-1"}, "", "", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 (limited), got %d", len(results))
	}
}

// ---------- LIKE fallback when FTS5 tables missing ----------

func TestSearchRepo_SearchSymbols_FallbackWhenNoFTS(t *testing.T) {
	// Create a DB with only the core migration (no FTS5 tables).
	db, ctx := setupTestDBNoFTS(t)
	symRepo := &SymbolRepo{db: db.db}
	seedSymbols(t, ctx, symRepo)

	r := &SearchRepo{db: db.db}
	results, err := r.SearchSymbols(ctx, []string{"ws-1"}, "Server", "", 10)
	if err != nil {
		t.Fatalf("search (fallback): %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (Server, StartServer), got %d", len(results))
	}
	// Verify FTS was detected as unavailable.
	if r.ftsAvailable == nil || *r.ftsAvailable {
		t.Error("expected ftsAvailable to be false for DB without FTS tables")
	}
}

// ---------- UpsertDocChunk / SearchDocs ----------

func TestSearchRepo_UpsertDocChunk(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SearchRepo{db: db.db}

	chunk := &repo.DocChunk{
		WorkspaceID:   "ws-1",
		FilePath:      "docs/README.md",
		FileHash:      "dochash1",
		ChunkIndex:    0,
		SectionHeader: "Getting Started",
		Content:       "This is the getting started section.",
		TokenCount:    7,
	}

	if err := r.UpsertDocChunk(ctx, chunk); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Upsert again with updated content (same unique key).
	chunk.Content = "Updated getting started content."
	chunk.TokenCount = 5
	if err := r.UpsertDocChunk(ctx, chunk); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	results, err := r.SearchDocs(ctx, []string{"ws-1"}, "getting started", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Content != "Updated getting started content." {
		t.Errorf("content not updated: %q", results[0].Content)
	}
	if results[0].TokenCount != 5 {
		t.Errorf("token_count = %d, want 5", results[0].TokenCount)
	}
}

func TestSearchRepo_SearchDocs_FTS(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SearchRepo{db: db.db}
	seedDocChunks(t, ctx, r)

	// Search by content match.
	results, err := r.SearchDocs(ctx, []string{"ws-1"}, "architecture", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 1 {
		t.Fatalf("expected >=1 result for 'architecture', got %d", len(results))
	}
	// Verify FTS score is populated.
	if r.ftsAvailable != nil && *r.ftsAvailable {
		if results[0].Score == 0 {
			t.Error("expected non-zero BM25 score for FTS doc result")
		}
	}
}

func TestSearchRepo_SearchDocs_FTS_SectionHeader(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SearchRepo{db: db.db}
	seedDocChunks(t, ctx, r)

	// Search by section_header match.
	results, err := r.SearchDocs(ctx, []string{"ws-1"}, "Data Flow", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'Data Flow', got %d", len(results))
	}
}

func TestSearchRepo_SearchDocs_LIKE(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SearchRepo{db: db.db}
	seedDocChunks(t, ctx, r)

	// Direct LIKE search.
	results, err := r.searchDocsLIKE(ctx, []string{"ws-1"}, "architecture", "doc", 10)
	if err != nil {
		t.Fatalf("like search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'architecture', got %d", len(results))
	}
	if results[0].Score != 0 {
		t.Errorf("expected Score=0 for LIKE result, got %f", results[0].Score)
	}
}

func TestSearchRepo_SearchDocs_FallbackWhenNoFTS(t *testing.T) {
	db, ctx := setupTestDBNoFTS(t)
	r := &SearchRepo{db: db.db}

	// Seed data directly (no FTS triggers, so only the base tables are populated).
	for _, c := range []*repo.DocChunk{
		{WorkspaceID: "ws-1", FilePath: "docs/arch.md", FileHash: "h1", ChunkIndex: 0, SectionHeader: "Architecture", Content: "The system uses a layered architecture."},
	} {
		if err := r.UpsertDocChunk(ctx, c); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	results, err := r.SearchDocs(ctx, []string{"ws-1"}, "architecture", 10)
	if err != nil {
		t.Fatalf("search (fallback): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestSearchRepo_SearchDocs_EmptyWorkspaces(t *testing.T) {
	db, ctx := setupTestDB(t)
	_ = db
	r := &SearchRepo{db: db.db}

	results, err := r.SearchDocs(ctx, nil, "anything", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty workspace list, got %v", results)
	}
}

// ---------- FTS availability caching ----------

func TestSearchRepo_FTSAvailabilityCaching(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &SearchRepo{db: db.db}

	// First call should probe and cache.
	avail1 := r.checkFTSAvailable(ctx)
	if !avail1 {
		t.Fatal("expected FTS to be available after full migration")
	}
	if r.ftsAvailable == nil {
		t.Fatal("expected ftsAvailable to be cached")
	}
	// Second call should return cached value without re-probing.
	avail2 := r.checkFTSAvailable(ctx)
	if avail1 != avail2 {
		t.Errorf("cached result mismatch: %v vs %v", avail1, avail2)
	}
}

// ---------- setupTestDBNoFTS ----------

// setupTestDBNoFTS creates a test database with all migrations applied, then
// drops the FTS5 tables and their sync triggers to simulate the LIKE fallback path.
func setupTestDBNoFTS(t *testing.T) (*DB, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)

	// Drop FTS5 sync triggers first (they INSERT into the FTS tables).
	triggers := []string{
		"symbols_ai", "symbols_ad", "symbols_au",
		"doc_chunks_ai", "doc_chunks_ad", "doc_chunks_au",
		"memories_fts_ai", "memories_fts_ad", "memories_fts_au",
	}
	for _, trig := range triggers {
		if _, err := db.db.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trig); err != nil {
			t.Fatalf("drop trigger %s: %v", trig, err)
		}
	}

	// Drop FTS5 virtual tables to force LIKE fallback.
	for _, table := range []string{"symbols_fts", "doc_chunks_fts", "memory_fts"} {
		if _, err := db.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	return db, ctx
}
