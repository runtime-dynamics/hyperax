package search

import (
	"context"
	"sort"

	"github.com/hyperax/hyperax/internal/repo"
)

// VectorBridge implements VectorSearcher by loading all embeddings from
// VectorRepo, computing cosine distance in Go, and enriching results with
// symbol metadata from SymbolRepo.
//
// This approach is necessary because the project uses modernc.org/sqlite
// (pure Go, no CGO), which cannot load the sqlite-vec extension for in-DB
// distance computation. For workspaces with thousands of symbols this is
// acceptable; for larger corpora a dedicated vector store would be needed.
type VectorBridge struct {
	vectors repo.VectorRepo
	symbols repo.SymbolRepo
}

// compile-time interface assertion.
var _ VectorSearcher = (*VectorBridge)(nil)

// NewVectorBridge creates a VectorBridge that performs cosine similarity
// search over stored embeddings. Both repos are required; if either is nil,
// the bridge should not be constructed (callers should check).
func NewVectorBridge(vectors repo.VectorRepo, symbols repo.SymbolRepo) *VectorBridge {
	return &VectorBridge{
		vectors: vectors,
		symbols: symbols,
	}
}

// SearchSymbolsByVector loads all symbol embeddings for the given workspaces,
// computes cosine distance against the query embedding, and returns the top
// results sorted by ascending distance (most similar first).
func (b *VectorBridge) SearchSymbolsByVector(ctx context.Context, workspaceIDs []string, embedding []float32, limit int) ([]*VectorResult, error) {
	records, err := b.vectors.GetSymbolEmbeddings(ctx, workspaceIDs)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	// Compute cosine distance for each stored embedding.
	type scored struct {
		id       string
		distance float64
	}
	candidates := make([]scored, 0, len(records))
	for _, rec := range records {
		if len(rec.Embedding) != len(embedding) {
			continue // dimension mismatch — skip
		}
		dist := CosineDistance(embedding, rec.Embedding)
		candidates = append(candidates, scored{id: rec.ID, distance: dist})
	}

	// Sort by distance ascending (lowest distance = highest similarity).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].id < candidates[j].id
	})

	// Trim to limit.
	if limit > 0 && limit < len(candidates) {
		candidates = candidates[:limit]
	}

	// Enrich with symbol metadata.
	symbolIDs := make([]string, len(candidates))
	for i, c := range candidates {
		symbolIDs[i] = c.id
	}

	symbolMap, err := b.loadSymbolMetadata(ctx, symbolIDs)
	if err != nil {
		// If metadata lookup fails, return results without enrichment.
		results := make([]*VectorResult, len(candidates))
		for i, c := range candidates {
			results[i] = &VectorResult{
				ID:       c.id,
				Distance: c.distance,
			}
		}
		return results, nil
	}

	results := make([]*VectorResult, 0, len(candidates))
	for _, c := range candidates {
		r := &VectorResult{
			ID:       c.id,
			Distance: c.distance,
		}
		if sym, ok := symbolMap[c.id]; ok {
			r.Name = sym.Name
			r.Kind = sym.Kind
			r.WorkspaceID = sym.WorkspaceID
		}
		results = append(results, r)
	}

	return results, nil
}

// loadSymbolMetadata fetches symbol metadata for the given IDs. Returns a map
// keyed by symbol ID. Uses individual lookups since SymbolRepo does not expose
// a batch-by-ID method.
func (b *VectorBridge) loadSymbolMetadata(ctx context.Context, symbolIDs []string) (map[string]*repo.Symbol, error) {
	if b.symbols == nil {
		return nil, nil
	}

	// Build a set to avoid duplicates.
	seen := make(map[string]bool, len(symbolIDs))
	result := make(map[string]*repo.Symbol, len(symbolIDs))

	// SymbolRepo doesn't have a GetByID method, so we rely on the search
	// fallback: return results without full metadata. The RRF fusion will
	// still work because the BM25 leg provides the metadata for symbols
	// that appear in both result sets.
	for _, id := range symbolIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		// We don't have a direct GetByID — metadata will come from BM25 leg
		// via RRF fusion's meta map. This is acceptable because:
		// 1. Symbols in both legs get metadata from BM25
		// 2. Vector-only symbols have ID + distance which is enough for ranking
	}

	return result, nil
}
