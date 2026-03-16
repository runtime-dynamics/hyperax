package search

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// mockVectorRepo implements repo.VectorRepo for testing.
type mockVectorRepo struct {
	symbolRecords   []repo.EmbeddingRecord
	docChunkRecords []repo.EmbeddingRecord
	upsertErr       error
}

func (m *mockVectorRepo) UpsertSymbolEmbedding(_ context.Context, _, _ string, _ []float32, _ int, _ string) error {
	return m.upsertErr
}

func (m *mockVectorRepo) UpsertDocChunkEmbedding(_ context.Context, _, _ string, _ []float32, _ int, _ string) error {
	return m.upsertErr
}

func (m *mockVectorRepo) GetSymbolEmbeddings(_ context.Context, _ []string) ([]repo.EmbeddingRecord, error) {
	return m.symbolRecords, nil
}

func (m *mockVectorRepo) GetDocChunkEmbeddings(_ context.Context, _ []string) ([]repo.EmbeddingRecord, error) {
	return m.docChunkRecords, nil
}

func (m *mockVectorRepo) DeleteSymbolEmbedding(_ context.Context, _ string) error { return nil }
func (m *mockVectorRepo) DeleteDocChunkEmbedding(_ context.Context, _ string) error {
	return nil
}

func (m *mockVectorRepo) CountSymbolEmbeddings(_ context.Context, _ []string) (int, error) {
	return len(m.symbolRecords), nil
}

func TestVectorBridge_SearchSymbolsByVector(t *testing.T) {
	// Create test embeddings: query is [1,0,0], sym-1 is [1,0,0] (identical),
	// sym-2 is [0,1,0] (orthogonal), sym-3 is [0.9,0.1,0] (similar).
	vecRepo := &mockVectorRepo{
		symbolRecords: []repo.EmbeddingRecord{
			{ID: "sym-1", WorkspaceID: "ws-1", Embedding: []float32{1, 0, 0}},
			{ID: "sym-2", WorkspaceID: "ws-1", Embedding: []float32{0, 1, 0}},
			{ID: "sym-3", WorkspaceID: "ws-1", Embedding: []float32{0.9, 0.1, 0}},
		},
	}

	bridge := NewVectorBridge(vecRepo, nil)
	ctx := context.Background()
	query := []float32{1, 0, 0}

	results, err := bridge.SearchSymbolsByVector(ctx, []string{"ws-1"}, query, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// sym-1 should be first (distance ~0), sym-3 second, sym-2 last (distance ~1).
	if results[0].ID != "sym-1" {
		t.Errorf("expected first result sym-1, got %s", results[0].ID)
	}
	if results[1].ID != "sym-3" {
		t.Errorf("expected second result sym-3, got %s", results[1].ID)
	}
	if results[2].ID != "sym-2" {
		t.Errorf("expected third result sym-2, got %s", results[2].ID)
	}

	// Distance of identical vectors should be ~0.
	if results[0].Distance > 0.01 {
		t.Errorf("expected distance ~0 for identical vectors, got %f", results[0].Distance)
	}
	// Distance of orthogonal vectors should be ~1.
	if results[2].Distance < 0.99 {
		t.Errorf("expected distance ~1 for orthogonal vectors, got %f", results[2].Distance)
	}
}

func TestVectorBridge_LimitResults(t *testing.T) {
	vecRepo := &mockVectorRepo{
		symbolRecords: []repo.EmbeddingRecord{
			{ID: "sym-1", WorkspaceID: "ws-1", Embedding: []float32{1, 0}},
			{ID: "sym-2", WorkspaceID: "ws-1", Embedding: []float32{0.9, 0.1}},
			{ID: "sym-3", WorkspaceID: "ws-1", Embedding: []float32{0, 1}},
		},
	}

	bridge := NewVectorBridge(vecRepo, nil)
	ctx := context.Background()
	query := []float32{1, 0}

	results, err := bridge.SearchSymbolsByVector(ctx, []string{"ws-1"}, query, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit=2, got %d", len(results))
	}
}

func TestVectorBridge_EmptyRecords(t *testing.T) {
	vecRepo := &mockVectorRepo{symbolRecords: nil}

	bridge := NewVectorBridge(vecRepo, nil)
	ctx := context.Background()
	query := []float32{1, 0}

	results, err := bridge.SearchSymbolsByVector(ctx, []string{"ws-1"}, query, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty records, got %v", results)
	}
}

func TestVectorBridge_DimensionMismatchSkipped(t *testing.T) {
	vecRepo := &mockVectorRepo{
		symbolRecords: []repo.EmbeddingRecord{
			{ID: "sym-1", WorkspaceID: "ws-1", Embedding: []float32{1, 0, 0}},
			{ID: "sym-2", WorkspaceID: "ws-1", Embedding: []float32{0, 1}}, // wrong dim
		},
	}

	bridge := NewVectorBridge(vecRepo, nil)
	ctx := context.Background()
	query := []float32{1, 0, 0}

	results, err := bridge.SearchSymbolsByVector(ctx, []string{"ws-1"}, query, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (dim mismatch skipped), got %d", len(results))
	}
	if results[0].ID != "sym-1" {
		t.Errorf("expected sym-1, got %s", results[0].ID)
	}
}

func TestVectorBridge_ImplementsVectorSearcher(t *testing.T) {
	var _ VectorSearcher = (*VectorBridge)(nil)
}

// ---------- RRF Fusion with vector results ----------

func TestFuseRRF_CombinesBM25AndVector(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "sym-1", Name: "Foo", Score: -1.5},
		{ID: "sym-2", Name: "Bar", Score: -2.0},
		{ID: "sym-3", Name: "Baz", Score: -3.0},
	}

	vector := []VectorResult{
		{ID: "sym-3", Name: "Baz", Distance: 0.1}, // top vector result
		{ID: "sym-1", Name: "Foo", Distance: 0.3},
		{ID: "sym-4", Name: "Qux", Distance: 0.5}, // vector-only
	}

	fused := FuseRRF(bm25, vector, 60, 10)

	// sym-1 and sym-3 appear in both lists, so they should rank higher.
	if len(fused) != 4 {
		t.Fatalf("expected 4 fused results, got %d", len(fused))
	}

	// sym-1 is rank 1 in BM25, rank 2 in vector → highest fused score.
	// sym-3 is rank 3 in BM25, rank 1 in vector → second highest.
	if fused[0].ID != "sym-1" {
		t.Errorf("expected sym-1 as top fused result, got %s", fused[0].ID)
	}
	if fused[1].ID != "sym-3" {
		t.Errorf("expected sym-3 as second fused result, got %s", fused[1].ID)
	}

	// All fused scores should be positive.
	for _, r := range fused {
		if r.Score <= 0 {
			t.Errorf("expected positive fused score for %s, got %f", r.ID, r.Score)
		}
	}
}

func TestFuseRRF_LimitOutput(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	vector := []VectorResult{
		{ID: "d"}, {ID: "e"},
	}

	fused := FuseRRF(bm25, vector, 60, 2)
	if len(fused) != 2 {
		t.Errorf("expected 2 fused results with limit=2, got %d", len(fused))
	}
}

func TestFuseRRF_EmptyInputs(t *testing.T) {
	fused := FuseRRF(nil, nil, 60, 10)
	if len(fused) != 0 {
		t.Errorf("expected 0 fused results for empty inputs, got %d", len(fused))
	}
}

// ---------- Cosine similarity / distance ----------

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 0, 0}
	sim := CosineSimilarity(a, a)
	if sim < 0.99 {
		t.Errorf("expected ~1.0 for identical vectors, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := CosineSimilarity(a, b)
	if sim > 0.01 {
		t.Errorf("expected ~0 for orthogonal vectors, got %f", sim)
	}
}

func TestCosineDistance(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0}
	dist := CosineDistance(a, b)
	if dist > 0.01 {
		t.Errorf("expected ~0 distance for identical vectors, got %f", dist)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0, 0}
	sim := CosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("expected 0 for mismatched lengths, got %f", sim)
	}
}
