package providers

import (
	"context"
	"strings"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// standardsKeyPrefix is the config key prefix used to store coding standards.
const standardsKeyPrefix = "standard."

// StandardsProvider returns hints derived from workspace coding standards
// stored as config values with keys prefixed by "standard.".
type StandardsProvider struct {
	config repo.ConfigRepo
}

// NewStandardsProvider creates a StandardsProvider. A nil config repo is
// tolerated; the provider will simply return no hints.
func NewStandardsProvider(config repo.ConfigRepo) *StandardsProvider {
	return &StandardsProvider{config: config}
}

// Name returns the provider identifier.
func (p *StandardsProvider) Name() string { return "standards" }

// GetHints queries the config store for standards scoped to the given workspace
// and language, then scores each standard against the request query.
func (p *StandardsProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	if p.config == nil {
		return nil, nil
	}

	// Determine the scope to query. Prefer workspace scope when a workspace ID
	// is provided; fall back to global scope.
	scope := types.ConfigScope{Type: "global"}
	if req.WorkspaceID != "" {
		scope = types.ConfigScope{Type: "workspace", ID: req.WorkspaceID}
	}

	values, err := p.config.ListValues(ctx, scope)
	if err != nil {
		return nil, nil // graceful degradation
	}

	queryLower := strings.ToLower(req.Query)
	langLower := strings.ToLower(req.Language)

	var results []hints.Hint
	for _, v := range values {
		if !strings.HasPrefix(v.Key, standardsKeyPrefix) {
			continue
		}

		relevance := scoreStandard(v.Key, v.Value, queryLower, langLower)
		if relevance <= 0.0 {
			continue
		}

		results = append(results, hints.Hint{
			Provider:  "standards",
			Category:  "standard",
			Content:   v.Value,
			Relevance: relevance,
			Source:    v.Key,
		})
	}

	return results, nil
}

// scoreStandard assigns a relevance score based on keyword overlap between the
// standard's key/value and the query or language filter.
func scoreStandard(key, value, queryLower, langLower string) float64 {
	keyLower := strings.ToLower(key)
	valueLower := strings.ToLower(value)

	var score float64

	// Language match is a strong signal.
	if langLower != "" {
		if strings.Contains(keyLower, langLower) || strings.Contains(valueLower, langLower) {
			score += 0.4
		}
	}

	// Check each query word for presence in key or value.
	if queryLower != "" {
		words := strings.Fields(queryLower)
		matched := 0
		for _, w := range words {
			if strings.Contains(keyLower, w) || strings.Contains(valueLower, w) {
				matched++
			}
		}
		if len(words) > 0 {
			ratio := float64(matched) / float64(len(words))
			score += ratio * 0.5
		}
	}

	// Every standard gets a small baseline score so it is discoverable.
	if score == 0.0 {
		score = 0.1
	}

	// Clamp to [0.0, 1.0].
	if score > 1.0 {
		score = 1.0
	}

	return score
}
