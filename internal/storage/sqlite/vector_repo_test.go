package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
)

// insertTestSymbol creates a parent file_hashes row and a symbols row so that
// the FK constraint on symbol_embeddings is satisfied. Returns the symbol's
// string ID (the INTEGER PK formatted as text).
func insertTestSymbol(t *testing.T, db *sql.DB, ctx context.Context, symID int, workspaceID string) string {
	t.Helper()
	// Ensure a file_hashes row exists for this workspace.
	_, _ = db.ExecContext(ctx,
		`INSERT OR IGNORE INTO file_hashes (file_id, workspace_id, file_path, hash_value) VALUES (?, ?, ?, ?)`,
		symID, workspaceID, fmt.Sprintf("test_%d.go", symID), "hash")
	// Insert symbol referencing the file_hash.
	_, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO symbols (id, file_id, name, kind, start_line, end_line, workspace_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		symID, symID, fmt.Sprintf("sym_%d", symID), "function", 1, 10, workspaceID)
	if err != nil {
		t.Fatalf("insert test symbol %d: %v", symID, err)
	}
	return fmt.Sprintf("%d", symID)
}

// insertTestDocChunk creates a doc_chunks row so that the FK constraint on
// doc_chunk_embeddings is satisfied. Returns the chunk's string ID.
func insertTestDocChunk(t *testing.T, db *sql.DB, ctx context.Context, chunkID int, workspaceID string) string {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO doc_chunks (id, workspace_id, file_path, file_hash, chunk_index, content, token_count) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chunkID, workspaceID, fmt.Sprintf("doc_%d.md", chunkID), "hash", 0, "content", 10)
	if err != nil {
		t.Fatalf("insert test doc chunk %d: %v", chunkID, err)
	}
	return fmt.Sprintf("%d", chunkID)
}

func TestVectorRepo_UpsertAndGetSymbolEmbedding(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	symID := insertTestSymbol(t, tdb.db, ctx, 1, "ws-1")

	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	err := repo.UpsertSymbolEmbedding(ctx, symID, "ws-1", embedding, 4, "test-model")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	records, err := repo.GetSymbolEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ID != symID {
		t.Errorf("expected ID %s, got %s", symID, records[0].ID)
	}
	if records[0].WorkspaceID != "ws-1" {
		t.Errorf("expected workspace ws-1, got %s", records[0].WorkspaceID)
	}
	if len(records[0].Embedding) != 4 {
		t.Fatalf("expected 4-dim embedding, got %d", len(records[0].Embedding))
	}
	for i, v := range embedding {
		if math.Abs(float64(records[0].Embedding[i]-v)) > 1e-6 {
			t.Errorf("embedding[%d]: expected %f, got %f", i, v, records[0].Embedding[i])
		}
	}
}

func TestVectorRepo_UpsertUpdatesExisting(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	symID := insertTestSymbol(t, tdb.db, ctx, 1, "ws-1")

	err := repo.UpsertSymbolEmbedding(ctx, symID, "ws-1", []float32{0.1, 0.2}, 2, "model-v1")
	if err != nil {
		t.Fatalf("upsert v1: %v", err)
	}

	err = repo.UpsertSymbolEmbedding(ctx, symID, "ws-1", []float32{0.9, 0.8}, 2, "model-v2")
	if err != nil {
		t.Fatalf("upsert v2: %v", err)
	}

	records, err := repo.GetSymbolEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after upsert, got %d", len(records))
	}
	if math.Abs(float64(records[0].Embedding[0]-0.9)) > 1e-6 {
		t.Errorf("expected updated embedding[0]=0.9, got %f", records[0].Embedding[0])
	}
}

func TestVectorRepo_DocChunkEmbedding(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	chunkID := insertTestDocChunk(t, tdb.db, ctx, 1, "ws-1")

	embedding := []float32{0.5, 0.6, 0.7}
	err := repo.UpsertDocChunkEmbedding(ctx, chunkID, "ws-1", embedding, 3, "test-model")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	records, err := repo.GetDocChunkEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ID != chunkID {
		t.Errorf("expected ID %s, got %s", chunkID, records[0].ID)
	}
}

func TestVectorRepo_DeleteSymbolEmbedding(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	symID := insertTestSymbol(t, tdb.db, ctx, 1, "ws-1")
	_ = repo.UpsertSymbolEmbedding(ctx, symID, "ws-1", []float32{0.1}, 1, "m")

	err := repo.DeleteSymbolEmbedding(ctx, symID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	records, err := repo.GetSymbolEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records after delete, got %d", len(records))
	}
}

func TestVectorRepo_DeleteDocChunkEmbedding(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	chunkID := insertTestDocChunk(t, tdb.db, ctx, 1, "ws-1")
	_ = repo.UpsertDocChunkEmbedding(ctx, chunkID, "ws-1", []float32{0.1}, 1, "m")

	err := repo.DeleteDocChunkEmbedding(ctx, chunkID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	records, err := repo.GetDocChunkEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records after delete, got %d", len(records))
	}
}

func TestVectorRepo_CountSymbolEmbeddings(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	s1 := insertTestSymbol(t, tdb.db, ctx, 1, "ws-1")
	s2 := insertTestSymbol(t, tdb.db, ctx, 2, "ws-1")
	s3 := insertTestSymbol(t, tdb.db, ctx, 3, "ws-2")

	_ = repo.UpsertSymbolEmbedding(ctx, s1, "ws-1", []float32{0.1}, 1, "m")
	_ = repo.UpsertSymbolEmbedding(ctx, s2, "ws-1", []float32{0.2}, 1, "m")
	_ = repo.UpsertSymbolEmbedding(ctx, s3, "ws-2", []float32{0.3}, 1, "m")

	count, err := repo.CountSymbolEmbeddings(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2 for ws-1, got %d", count)
	}

	count, err = repo.CountSymbolEmbeddings(ctx, []string{"ws-1", "ws-2"})
	if err != nil {
		t.Fatalf("count all: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3 for all, got %d", count)
	}
}

func TestVectorRepo_EmptyWorkspaceIDs(t *testing.T) {
	tdb, _ := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}
	ctx := context.Background()

	records, err := repo.GetSymbolEmbeddings(ctx, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if records != nil {
		t.Errorf("expected nil for empty workspace IDs, got %v", records)
	}

	count, err := repo.CountSymbolEmbeddings(ctx, nil)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 count for empty workspace IDs, got %d", count)
	}
}

func TestVectorRepo_MultipleWorkspaces(t *testing.T) {
	tdb, ctx := setupTestDB(t)
	repo := &VectorRepo{db: tdb.db}

	s1 := insertTestSymbol(t, tdb.db, ctx, 1, "ws-1")
	s2 := insertTestSymbol(t, tdb.db, ctx, 2, "ws-2")
	s3 := insertTestSymbol(t, tdb.db, ctx, 3, "ws-3")

	_ = repo.UpsertSymbolEmbedding(ctx, s1, "ws-1", []float32{0.1, 0.2}, 2, "m")
	_ = repo.UpsertSymbolEmbedding(ctx, s2, "ws-2", []float32{0.3, 0.4}, 2, "m")
	_ = repo.UpsertSymbolEmbedding(ctx, s3, "ws-3", []float32{0.5, 0.6}, 2, "m")

	records, err := repo.GetSymbolEmbeddings(ctx, []string{"ws-1", "ws-3"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	ids := map[string]bool{}
	for _, r := range records {
		ids[r.ID] = true
	}
	if !ids[s1] || !ids[s3] {
		t.Errorf("expected %s and %s, got %v", s1, s3, ids)
	}
}

// ---------- BLOB encoding helpers ----------

func TestEncodeDecodeEmbedding(t *testing.T) {
	original := []float32{1.0, -0.5, 0.0, 3.14, -2.71}
	blob := encodeEmbedding(original)
	if len(blob) != len(original)*4 {
		t.Fatalf("expected %d bytes, got %d", len(original)*4, len(blob))
	}

	decoded := decodeEmbedding(blob, len(original))
	if decoded == nil {
		t.Fatal("decodeEmbedding returned nil")
	}
	for i, v := range original {
		if decoded[i] != v {
			t.Errorf("decoded[%d]: expected %f, got %f", i, v, decoded[i])
		}
	}
}

func TestDecodeEmbedding_DimensionMismatch(t *testing.T) {
	blob := encodeEmbedding([]float32{1.0, 2.0})
	decoded := decodeEmbedding(blob, 3) // wrong dimension
	if decoded != nil {
		t.Error("expected nil for dimension mismatch")
	}
}
