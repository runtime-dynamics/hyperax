package search

import "sort"

// FuseRRF combines BM25 and vector search results using Reciprocal Rank
// Fusion. Each result's score is computed as: sum(1 / (k + rank_i)) across
// all result lists where it appears. The k parameter (default 60) controls
// how much rank position matters -- higher k reduces the impact of rank
// differences, producing more uniform fusion.
//
// The returned slice is sorted by fused score descending (highest relevance
// first) and capped at limit entries.
func FuseRRF(bm25 []SearchResult, vector []VectorResult, k int, limit int) []SearchResult {
	return WeightedFuseRRF(bm25, vector, k, 0.0, limit)
}

// WeightedFuseRRF combines BM25 and vector search results using weighted
// Reciprocal Rank Fusion. The alpha parameter controls the balance between
// the two result sets:
//
//   - alpha = 0.0: classic RRF — equal weight for both lists (identical to FuseRRF)
//   - alpha = 0.6: semantic-aware — 60% vector weight, 40% BM25 weight
//   - alpha = 1.0: vector-only scoring (BM25 contributions are zeroed)
//
// When alpha > 0, the BM25 contribution is scaled by (1-alpha) and the vector
// contribution is scaled by alpha. When alpha == 0, both contributions use
// the standard unweighted 1/(k+rank) formula.
//
// The k parameter (default 60) controls how much rank position matters.
// The returned slice is sorted by fused score descending and capped at limit.
func WeightedFuseRRF(bm25 []SearchResult, vector []VectorResult, k int, alpha float64, limit int) []SearchResult {
	if k <= 0 {
		k = 60
	}
	// Clamp alpha to [0, 1].
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}

	// Compute weights. When alpha == 0, both are 1.0 (classic RRF).
	bm25Weight := 1.0
	vecWeight := 1.0
	if alpha > 0 {
		bm25Weight = 1.0 - alpha
		vecWeight = alpha
	}

	scores := make(map[string]float64)
	meta := make(map[string]SearchResult)

	// Score BM25 results by rank position.
	for rank, r := range bm25 {
		scores[r.ID] += bm25Weight * (1.0 / float64(k+rank+1))
		meta[r.ID] = r
	}

	// Score vector results by rank position.
	for rank, r := range vector {
		scores[r.ID] += vecWeight * (1.0 / float64(k+rank+1))
		if _, exists := meta[r.ID]; !exists {
			meta[r.ID] = SearchResult{
				ID:          r.ID,
				Name:        r.Name,
				Kind:        r.Kind,
				FilePath:    r.FilePath,
				WorkspaceID: r.WorkspaceID,
			}
		}
	}

	// Sort by fused score descending.
	type scored struct {
		id    string
		score float64
	}
	ranked := make([]scored, 0, len(scores))
	for id, s := range scores {
		ranked = append(ranked, scored{id, s})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// Tie-break by ID for deterministic ordering in tests.
		return ranked[i].id < ranked[j].id
	})

	// Build final results, capped at limit.
	if limit <= 0 || limit > len(ranked) {
		limit = len(ranked)
	}
	results := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		r := meta[ranked[i].id]
		r.Score = ranked[i].score
		results[i] = r
	}

	return results
}

// DynamicK computes an adaptive k value as min(limit/2, maxK).
// This gives better fusion quality for small result sets where a large
// static k would over-smooth rank differences.
func DynamicK(limit, maxK int) int {
	if limit <= 0 {
		return maxK
	}
	half := limit / 2
	if half < 1 {
		half = 1
	}
	if half < maxK {
		return half
	}
	return maxK
}
