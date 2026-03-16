package memory

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// RetrievalEngine implements the scope-cascaded hybrid recall algorithm:
//
//  1. Scope Resolution: determine queryable scopes from persona + workspace
//  2. BM25 Query: FTS5 search per scope tier
//  3. Temporal Decay: exponential half-life weighting
//  4. Scope Cascade: persona(3) → project(5) → global(2), max 10 total
//  5. Return: []MemoryContext with fused scores
//
// Vector search + RRF fusion will be added when an Embedder is available.
// Until then, the engine operates in BM25-only mode with temporal decay.
type RetrievalEngine struct {
	repo   repo.MemoryRepo
	config RetrievalConfig
}

// NewRetrievalEngine creates a RetrievalEngine.
func NewRetrievalEngine(memRepo repo.MemoryRepo, cfg RetrievalConfig) *RetrievalEngine {
	if cfg.FusionK == 0 {
		cfg.FusionK = 60
	}
	if cfg.HalfLifeDays == 0 {
		cfg.HalfLifeDays = 30
	}
	if cfg.AnchorPenalty == 0 {
		cfg.AnchorPenalty = 0.3
	}
	if cfg.ShadowThreshold == 0 {
		cfg.ShadowThreshold = 0.85
	}
	if cfg.PersonaLimit == 0 {
		cfg.PersonaLimit = 3
	}
	if cfg.ProjectLimit == 0 {
		cfg.ProjectLimit = 5
	}
	if cfg.GlobalLimit == 0 {
		cfg.GlobalLimit = 2
	}
	if cfg.TotalLimit == 0 {
		cfg.TotalLimit = 10
	}
	return &RetrievalEngine{repo: memRepo, config: cfg}
}

// Recall performs the full scope-cascaded recall pipeline.
func (e *RetrievalEngine) Recall(ctx context.Context, query types.MemoryQuery) ([]types.MemoryContext, error) {
	if query.Query == "" {
		return nil, nil
	}
	if query.MaxResults <= 0 {
		query.MaxResults = e.config.TotalLimit
	}

	var allResults []types.MemoryContext

	// Stage 1: Scope cascade — persona → project → global.
	// Each tier is queried independently with its own limit.

	// Persona scope (most specific).
	if query.PersonaID != "" {
		persona, err := e.recallScope(ctx, query.Query, types.MemoryScopePersona, query.WorkspaceID, query.PersonaID, e.config.PersonaLimit)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, persona...)
	}

	// Project scope.
	if query.WorkspaceID != "" {
		project, err := e.recallScope(ctx, query.Query, types.MemoryScopeProject, query.WorkspaceID, "", e.config.ProjectLimit)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, project...)
	}

	// Global scope (broadest).
	global, err := e.recallScope(ctx, query.Query, types.MemoryScopeGlobal, "", "", e.config.GlobalLimit)
	if err != nil {
		return nil, err
	}
	allResults = append(allResults, global...)

	// Deduplicate by memory ID (in case overlapping scopes returned the same memory).
	allResults = dedup(allResults)

	// Sort by score descending.
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// Cap at total limit.
	if len(allResults) > query.MaxResults {
		allResults = allResults[:query.MaxResults]
	}

	// Assign final ranks.
	for i := range allResults {
		allResults[i].Rank = i + 1
	}

	return allResults, nil
}

// recallScope queries a single scope tier and applies temporal decay.
func (e *RetrievalEngine) recallScope(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]types.MemoryContext, error) {
	memories, err := e.repo.Recall(ctx, query, scope, workspaceID, personaID, limit*2) // fetch extra for decay reranking
	if err != nil {
		return nil, err
	}

	now := time.Now()
	results := make([]types.MemoryContext, 0, len(memories))

	for rank, m := range memories {
		// Base score from BM25 rank position: 1/(k+rank+1).
		baseScore := 1.0 / float64(e.config.FusionK+rank+1)

		// Apply temporal decay: decay(t) = e^(-λ·t) where λ = ln(2)/half_life.
		daysSinceAccess := now.Sub(m.AccessedAt).Hours() / 24.0
		if daysSinceAccess < 0 {
			daysSinceAccess = 0
		}
		lambda := math.Ln2 / float64(e.config.HalfLifeDays)
		decay := math.Exp(-lambda * daysSinceAccess)

		score := baseScore * decay

		// Anchored memories get a boost: they are never demoted.
		if m.IsAnchored() {
			score *= 1.5
		}

		results = append(results, types.MemoryContext{
			Memory: *m,
			Score:  score,
			Source: "recall",
		})
	}

	// Re-sort by decayed score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Cap to the scope limit.
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// TemporalDecay computes the exponential decay factor for a given number of
// days since last access. Exported for testing.
func TemporalDecay(daysSinceAccess float64, halfLifeDays int) float64 {
	if halfLifeDays <= 0 {
		halfLifeDays = 30
	}
	if daysSinceAccess < 0 {
		daysSinceAccess = 0
	}
	lambda := math.Ln2 / float64(halfLifeDays)
	return math.Exp(-lambda * daysSinceAccess)
}

// dedup removes duplicate MemoryContext entries by memory ID,
// keeping the one with the highest score.
func dedup(results []types.MemoryContext) []types.MemoryContext {
	seen := make(map[string]int) // id → index in out
	out := make([]types.MemoryContext, 0, len(results))

	for _, mc := range results {
		if idx, ok := seen[mc.Memory.ID]; ok {
			if mc.Score > out[idx].Score {
				out[idx] = mc
			}
			continue
		}
		seen[mc.Memory.ID] = len(out)
		out = append(out, mc)
	}
	return out
}
