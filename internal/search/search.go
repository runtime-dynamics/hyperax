package search

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/observability"
	"github.com/hyperax/hyperax/internal/repo"
)

// VectorSearcher is an optional interface for vector similarity search.
// If the underlying SearchRepo also implements this interface, the
// HybridSearcher enables the vector branch of hybrid search.
type VectorSearcher interface {
	SearchSymbolsByVector(ctx context.Context, workspaceIDs []string, embedding []float32, limit int) ([]*VectorResult, error)
}

// HybridSearcher orchestrates BM25 text search, optional vector similarity
// search, and RRF fusion. It wraps the existing repo.SearchRepo and adds
// graceful degradation through three levels:
//
//   - Level 1 ("like"):   FTS5 unavailable -- handled internally by SearchRepo
//   - Level 2 ("fts5"):   Vector search disabled or embedder unavailable -- BM25 only
//   - Level 3 ("hybrid"): Full hybrid with FTS5 + vector + RRF fusion
type HybridSearcher struct {
	searchRepo repo.SearchRepo
	config     Config
	embedder   Embedder
	vecSearch  VectorSearcher // nil if vector search not available
	mu         sync.RWMutex
}

// NewHybridSearcher creates a HybridSearcher wrapping the given SearchRepo.
// The config controls whether vector search is enabled and the RRF fusion
// parameters. If the SearchRepo also implements VectorSearcher, vector
// search is automatically available.
func NewHybridSearcher(searchRepo repo.SearchRepo, cfg Config) *HybridSearcher {
	if cfg.FusionK == 0 {
		cfg.FusionK = 60
	}
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = 384
	}
	if cfg.CandidatePoolSize == 0 {
		cfg.CandidatePoolSize = 100
	}

	h := &HybridSearcher{
		searchRepo: searchRepo,
		config:     cfg,
		embedder:   NoOpEmbedder{},
	}

	// Check if the search repo also supports vector search.
	if vs, ok := searchRepo.(VectorSearcher); ok {
		h.vecSearch = vs
	}

	return h
}

// SetEmbedder allows injecting an embedder after construction, for example
// once ONNX Runtime initialisation completes asynchronously. Thread-safe.
func (h *HybridSearcher) SetEmbedder(e Embedder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.embedder = e
}

// SetVectorSearcher allows injecting a VectorSearcher after construction.
// This is used to wire the VectorBridge once the VectorRepo is available.
// Thread-safe.
func (h *HybridSearcher) SetVectorSearcher(vs VectorSearcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vecSearch = vs
}

// Search performs a hybrid search with 3-level graceful degradation.
// It always calls SearchRepo.SearchSymbols for the BM25 leg. When vector
// search is enabled and the embedder succeeds, it additionally performs a
// vector similarity search and fuses both result sets with RRF.
func (h *HybridSearcher) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}

	start := time.Now()
	level := "fts5"
	defer func() {
		observability.SearchLatency.WithLabelValues(level).Observe(time.Since(start).Seconds())
	}()

	// Determine candidate pool size: fetch more items than requested from
	// each leg so that RRF fusion has a richer candidate set.
	candidateLimit := q.Limit
	if h.config.EnableVector && h.vecSearch != nil {
		candidateLimit = h.config.CandidatePoolSize
		if candidateLimit < q.Limit {
			candidateLimit = q.Limit
		}
	}

	// BM25 leg -- delegates to SearchRepo which handles FTS5 -> LIKE internally.
	symbols, err := h.searchRepo.SearchSymbols(ctx, q.Workspaces, q.Text, q.Kind, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("search.HybridSearcher.Search: %w", err)
	}

	bm25Results := symbolsToResults(symbols)

	// If vector search is not enabled or no vector searcher, return BM25 only.
	if !h.config.EnableVector || h.vecSearch == nil {
		level = "fts5"
		// Trim to requested limit when returning BM25-only results.
		if len(bm25Results) > q.Limit {
			bm25Results = bm25Results[:q.Limit]
		}
		return bm25Results, nil
	}

	// Attempt to embed the query text.
	h.mu.RLock()
	embedder := h.embedder
	h.mu.RUnlock()

	embedding, err := embedder.Embed(ctx, q.Text)
	if err != nil {
		// Embedder unavailable -- degrade gracefully to BM25 only.
		level = "fts5"
		if len(bm25Results) > q.Limit {
			bm25Results = bm25Results[:q.Limit]
		}
		return bm25Results, nil
	}

	// Vector search leg — use same expanded candidate pool.
	vecResults, err := h.vecSearch.SearchSymbolsByVector(ctx, q.Workspaces, embedding, candidateLimit)
	if err != nil {
		// Vector search failed -- degrade gracefully to BM25 only.
		level = "fts5"
		if len(bm25Results) > q.Limit {
			bm25Results = bm25Results[:q.Limit]
		}
		return bm25Results, nil
	}

	// Compute effective k: dynamic if configured, otherwise static.
	fusionK := h.config.FusionK
	if h.config.DynamicK {
		fusionK = DynamicK(q.Limit, h.config.FusionK)
	}

	// Full hybrid: fuse BM25 and vector results with Weighted RRF.
	level = "hybrid"
	fused := WeightedFuseRRF(bm25Results, derefVectorResults(vecResults), fusionK, h.config.AlphaWeight, q.Limit)
	return fused, nil
}

// SearchDocs delegates documentation search to the underlying repo.
// Vector fusion for doc chunks is not yet implemented.
func (h *HybridSearcher) SearchDocs(ctx context.Context, workspaces []string, query string, limit int) ([]*repo.DocChunk, error) {
	return h.searchRepo.SearchDocs(ctx, workspaces, query, limit)
}

// Config returns the current search configuration (read-only copy).
func (h *HybridSearcher) Config() Config {
	return h.config
}

// symbolsToResults converts repo.Symbol pointers to SearchResult values.
// Note: repo.Symbol does not carry FilePath, so that field will be empty.
// Future enhancements may add FilePath to the Symbol struct.
func symbolsToResults(symbols []*repo.Symbol) []SearchResult {
	results := make([]SearchResult, len(symbols))
	for i, s := range symbols {
		results[i] = SearchResult{
			ID:          s.ID,
			Name:        s.Name,
			Kind:        s.Kind,
			StartLine:   s.StartLine,
			EndLine:     s.EndLine,
			Signature:   s.Signature,
			WorkspaceID: s.WorkspaceID,
			Score:       s.Score,
		}
	}
	return results
}

// derefVectorResults converts a slice of *VectorResult to []VectorResult
// for use with FuseRRF which operates on value types.
func derefVectorResults(ptrs []*VectorResult) []VectorResult {
	results := make([]VectorResult, len(ptrs))
	for i, p := range ptrs {
		results[i] = *p
	}
	return results
}
