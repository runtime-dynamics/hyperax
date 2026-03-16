package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
)

// maxErrorMetrics is the number of recent error metrics to inspect.
const maxErrorMetrics = 20

// ErrorsProvider returns hints about recent error patterns derived from the
// metrics store.
type ErrorsProvider struct {
	metrics repo.MetricsRepo
}

// NewErrorsProvider creates an ErrorsProvider. A nil metrics repo is tolerated.
func NewErrorsProvider(metrics repo.MetricsRepo) *ErrorsProvider {
	return &ErrorsProvider{metrics: metrics}
}

// Name returns the provider identifier.
func (p *ErrorsProvider) Name() string { return "errors" }

// GetHints scans recent tool metrics for high call-count entries that may
// indicate repeated failures or hot-spots, then returns hints about those
// patterns.
func (p *ErrorsProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	if p.metrics == nil {
		return nil, nil
	}

	metrics, err := p.metrics.GetToolMetrics(ctx)
	if err != nil {
		return nil, nil // graceful degradation
	}

	queryLower := strings.ToLower(req.Query)

	var results []hints.Hint
	count := 0

	for _, m := range metrics {
		if count >= maxErrorMetrics {
			break
		}
		count++

		// Build a descriptive hint about the tool's usage pattern.
		content := fmt.Sprintf(
			"Tool %q: %d calls, total duration %d ms",
			m.ToolName, m.CallCount, m.TotalDurationMS,
		)
		if m.LastUsed != nil {
			content += fmt.Sprintf(", last used %s", m.LastUsed.Format("2006-01-02 15:04:05"))
		}

		relevance := scoreErrorMetric(m, queryLower)
		if relevance <= 0.0 {
			continue
		}

		results = append(results, hints.Hint{
			Provider:  "errors",
			Category:  "error",
			Content:   content,
			Relevance: relevance,
			Source:    m.ToolName,
		})
	}

	return results, nil
}

// scoreErrorMetric assigns a relevance score for a tool metric based on
// call frequency and query keyword overlap.
func scoreErrorMetric(m *repo.ToolMetric, queryLower string) float64 {
	var score float64

	// High call counts suggest a hot-spot worth knowing about.
	switch {
	case m.CallCount > 100:
		score += 0.5
	case m.CallCount > 50:
		score += 0.3
	case m.CallCount > 10:
		score += 0.2
	default:
		score += 0.1
	}

	// Query keyword match.
	if queryLower != "" && strings.Contains(strings.ToLower(m.ToolName), queryLower) {
		score += 0.4
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}
