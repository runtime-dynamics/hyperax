package providers

import (
	"context"
	"strings"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// maxMemoryResults limits the number of memories fetched per query.
const maxMemoryResults = 10

// MemoryProvider returns hints drawn from the agent memory store, surfacing
// previously stored memories that are relevant to the current query.
type MemoryProvider struct {
	memory repo.MemoryRepo
}

// NewMemoryProvider creates a MemoryProvider. A nil memory repo is tolerated.
func NewMemoryProvider(memory repo.MemoryRepo) *MemoryProvider {
	return &MemoryProvider{memory: memory}
}

// Name returns the provider identifier.
func (p *MemoryProvider) Name() string { return "memory" }

// GetHints recalls memories matching the query, using the workspace ID as the
// project scope filter.
func (p *MemoryProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	if p.memory == nil {
		return nil, nil
	}
	if req.Query == "" {
		return nil, nil
	}

	// Recall across all scopes for the workspace.
	memories, err := p.memory.Recall(ctx, req.Query, "", req.WorkspaceID, "", maxMemoryResults)
	if err != nil {
		return nil, nil // graceful degradation
	}

	queryLower := strings.ToLower(req.Query)

	var results []hints.Hint
	for _, m := range memories {
		relevance := scoreMemory(m, queryLower)
		results = append(results, hints.Hint{
			Provider:  "memory",
			Category:  "memory",
			Content:   m.Content,
			Relevance: relevance,
			Source:    string(m.Scope),
		})
	}

	return results, nil
}

// scoreMemory assigns a relevance score based on content overlap with the query.
func scoreMemory(m *types.Memory, queryLower string) float64 {
	contentLower := strings.ToLower(m.Content)

	// Exact query substring match.
	if strings.Contains(contentLower, queryLower) {
		return 0.8
	}

	// Partial word overlap.
	words := strings.Fields(queryLower)
	if len(words) == 0 {
		return 0.3
	}
	matched := 0
	for _, w := range words {
		if strings.Contains(contentLower, w) {
			matched++
		}
	}
	ratio := float64(matched) / float64(len(words))
	return 0.3 + ratio*0.4 // range [0.3, 0.7]
}
