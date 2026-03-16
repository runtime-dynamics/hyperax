package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// ---------- Mock SearchRepo ----------

// mockSearchRepo implements repo.SearchRepo for testing.
type mockSearchRepo struct {
	symbols []*repo.Symbol
	docs    []*repo.DocChunk
	err     error
}

func (m *mockSearchRepo) SearchSymbols(_ context.Context, _ []string, _ string, _ string, _ int) ([]*repo.Symbol, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.symbols, nil
}

func (m *mockSearchRepo) UpsertDocChunk(_ context.Context, _ *repo.DocChunk) error {
	return m.err
}

func (m *mockSearchRepo) SearchDocs(_ context.Context, _ []string, _ string, _ int) ([]*repo.DocChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.docs, nil
}

func (m *mockSearchRepo) SearchCodeContent(_ context.Context, _ []string, _ string, _ int) ([]*repo.DocChunk, error) {
	return nil, nil
}

func (m *mockSearchRepo) DeleteDocChunksByPath(_ context.Context, _, _ string) error {
	return nil
}

// ---------- Mock SearchRepo with VectorSearcher ----------

// mockVectorSearchRepo implements both repo.SearchRepo and VectorSearcher.
type mockVectorSearchRepo struct {
	mockSearchRepo
	vecResults []*VectorResult
	vecErr     error
}

func (m *mockVectorSearchRepo) SearchSymbolsByVector(_ context.Context, _ []string, _ []float32, _ int) ([]*VectorResult, error) {
	if m.vecErr != nil {
		return nil, m.vecErr
	}
	return m.vecResults, nil
}

// ---------- Mock Embedder ----------

// mockEmbedder returns a fixed embedding.
type mockEmbedder struct {
	embedding []float32
	err       error
}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.embedding, nil
}

func (m *mockEmbedder) Close() error { return nil }

// ---------- Tests ----------

func TestSearch_VectorDisabled_ReturnsFTS5(t *testing.T) {
	symbols := []*repo.Symbol{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1", Score: -1.5},
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1", Score: -0.8},
	}
	sr := &mockSearchRepo{symbols: symbols}
	cfg := DefaultConfig() // EnableVector=false
	h := NewHybridSearcher(sr, cfg)

	results, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "Foo" {
		t.Errorf("expected first result Name=Foo, got %s", results[0].Name)
	}
	if results[0].Score != -1.5 {
		t.Errorf("expected Score=-1.5, got %f", results[0].Score)
	}
}

func TestSearch_NoOpEmbedder_DegradesToFTS5(t *testing.T) {
	symbols := []*repo.Symbol{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1"},
	}
	vecResults := []*VectorResult{
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1", Distance: 0.1},
	}
	sr := &mockVectorSearchRepo{
		mockSearchRepo: mockSearchRepo{symbols: symbols},
		vecResults:     vecResults,
	}
	cfg := Config{
		EnableVector: true,
		FusionK:      60,
		EmbeddingDim: 384,
	}
	h := NewHybridSearcher(sr, cfg)
	// NoOpEmbedder is the default -- it returns an error.

	results, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should degrade to BM25 only since NoOpEmbedder fails.
	if len(results) != 1 {
		t.Fatalf("expected 1 result (BM25 only), got %d", len(results))
	}
	if results[0].Name != "Foo" {
		t.Errorf("expected Name=Foo, got %s", results[0].Name)
	}
}

func TestSearch_HybridMode(t *testing.T) {
	symbols := []*repo.Symbol{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1"},
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1"},
	}
	vecResults := []*VectorResult{
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1", Distance: 0.1},
		{ID: "3", Name: "Baz", Kind: "method", WorkspaceID: "ws1", Distance: 0.3},
	}
	sr := &mockVectorSearchRepo{
		mockSearchRepo: mockSearchRepo{symbols: symbols},
		vecResults:     vecResults,
	}
	cfg := Config{
		EnableVector: true,
		FusionK:      60,
		EmbeddingDim: 384,
	}
	h := NewHybridSearcher(sr, cfg)
	h.SetEmbedder(&mockEmbedder{embedding: make([]float32, 384)})

	results, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return fused results from both lists.
	if len(results) != 3 {
		t.Fatalf("expected 3 results (fused), got %d", len(results))
	}
	// ID 2 appears in both lists so should be first.
	if results[0].ID != "2" {
		t.Errorf("expected ID 2 (in both lists) first, got %s", results[0].ID)
	}
}

func TestSearch_VectorSearchError_DegradesToFTS5(t *testing.T) {
	symbols := []*repo.Symbol{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1"},
	}
	sr := &mockVectorSearchRepo{
		mockSearchRepo: mockSearchRepo{symbols: symbols},
		vecErr:         fmt.Errorf("vector index not ready"),
	}
	cfg := Config{
		EnableVector: true,
		FusionK:      60,
		EmbeddingDim: 384,
	}
	h := NewHybridSearcher(sr, cfg)
	h.SetEmbedder(&mockEmbedder{embedding: make([]float32, 384)})

	results, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (BM25 only), got %d", len(results))
	}
}

func TestSearch_SearchRepoError_Propagates(t *testing.T) {
	sr := &mockSearchRepo{err: fmt.Errorf("database offline")}
	h := NewHybridSearcher(sr, DefaultConfig())

	_, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSetEmbedder(t *testing.T) {
	sr := &mockSearchRepo{}
	h := NewHybridSearcher(sr, DefaultConfig())

	// Default embedder is NoOpEmbedder.
	_, err := h.embedder.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected NoOpEmbedder to return error")
	}

	// Set a working embedder.
	h.SetEmbedder(&mockEmbedder{embedding: make([]float32, 384)})
	emb, err := h.embedder.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error after SetEmbedder: %v", err)
	}
	if len(emb) != 384 {
		t.Errorf("expected 384-dim embedding, got %d", len(emb))
	}
}

func TestSearchDocs_DelegatesToRepo(t *testing.T) {
	docs := []*repo.DocChunk{
		{ID: "1", WorkspaceID: "ws1", Content: "hello world"},
		{ID: "2", WorkspaceID: "ws1", Content: "foo bar"},
	}
	sr := &mockSearchRepo{docs: docs}
	h := NewHybridSearcher(sr, DefaultConfig())

	results, err := h.SearchDocs(context.Background(), []string{"ws1"}, "hello", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 doc chunks, got %d", len(results))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.EnableVector {
		t.Error("expected EnableVector=false by default")
	}
	if cfg.EmbeddingDim != 384 {
		t.Errorf("expected EmbeddingDim=384, got %d", cfg.EmbeddingDim)
	}
	if cfg.FusionK != 60 {
		t.Errorf("expected FusionK=60, got %d", cfg.FusionK)
	}
}

func TestNewHybridSearcher_DefaultsApplied(t *testing.T) {
	sr := &mockSearchRepo{}
	h := NewHybridSearcher(sr, Config{})

	if h.config.FusionK != 60 {
		t.Errorf("expected FusionK=60, got %d", h.config.FusionK)
	}
	if h.config.EmbeddingDim != 384 {
		t.Errorf("expected EmbeddingDim=384, got %d", h.config.EmbeddingDim)
	}
}

func TestNewHybridSearcher_DetectsVectorSearcher(t *testing.T) {
	// Plain SearchRepo -- no VectorSearcher.
	sr := &mockSearchRepo{}
	h := NewHybridSearcher(sr, DefaultConfig())
	if h.vecSearch != nil {
		t.Error("expected vecSearch=nil for plain SearchRepo")
	}

	// SearchRepo that also implements VectorSearcher.
	vsr := &mockVectorSearchRepo{}
	h2 := NewHybridSearcher(vsr, DefaultConfig())
	if h2.vecSearch == nil {
		t.Error("expected vecSearch to be detected for VectorSearcher-capable repo")
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	symbols := make([]*repo.Symbol, 60)
	for i := range symbols {
		symbols[i] = &repo.Symbol{
			ID:   fmt.Sprintf("%d", i+1),
			Name: fmt.Sprintf("Sym%d", i+1),
		}
	}
	sr := &mockSearchRepo{symbols: symbols}
	h := NewHybridSearcher(sr, DefaultConfig())

	results, err := h.Search(context.Background(), Query{
		Text:       "sym",
		Workspaces: []string{"ws1"},
		// Limit=0 should default to 50
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The mock returns all 60 symbols regardless of limit, but the searcher
	// trims BM25-only results to the default limit of 50.
	if len(results) != 50 {
		t.Fatalf("expected 50 results (default limit), got %d", len(results))
	}
}

func TestNoOpEmbedder(t *testing.T) {
	e := NoOpEmbedder{}

	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected NoOpEmbedder.Embed to return error")
	}

	if closeErr := e.Close(); closeErr != nil {
		t.Errorf("expected NoOpEmbedder.Close to return nil, got %v", closeErr)
	}
}

func TestSearch_EmbedderError_DegradesToFTS5(t *testing.T) {
	symbols := []*repo.Symbol{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1"},
	}
	sr := &mockVectorSearchRepo{
		mockSearchRepo: mockSearchRepo{symbols: symbols},
		vecResults:     []*VectorResult{{ID: "2", Name: "Bar"}},
	}
	cfg := Config{
		EnableVector: true,
		FusionK:      60,
		EmbeddingDim: 384,
	}
	h := NewHybridSearcher(sr, cfg)
	h.SetEmbedder(&mockEmbedder{err: fmt.Errorf("model load failed")})

	results, err := h.Search(context.Background(), Query{
		Text:       "foo",
		Workspaces: []string{"ws1"},
		Limit:      10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (BM25 only), got %d", len(results))
	}
}

func TestSearch_ConfigAccessor(t *testing.T) {
	sr := &mockSearchRepo{}
	cfg := Config{
		EnableVector:   true,
		EmbeddingModel: "/path/to/model.onnx",
		EmbeddingDim:   512,
		FusionK:        30,
	}
	h := NewHybridSearcher(sr, cfg)

	got := h.Config()
	if got.EnableVector != true {
		t.Error("expected EnableVector=true")
	}
	if got.EmbeddingModel != "/path/to/model.onnx" {
		t.Errorf("expected EmbeddingModel=/path/to/model.onnx, got %s", got.EmbeddingModel)
	}
	if got.EmbeddingDim != 512 {
		t.Errorf("expected EmbeddingDim=512, got %d", got.EmbeddingDim)
	}
	if got.FusionK != 30 {
		t.Errorf("expected FusionK=30, got %d", got.FusionK)
	}
}
