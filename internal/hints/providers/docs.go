package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
)

// maxDocResults limits the number of doc chunks fetched per search query.
const maxDocResults = 20

// DocsProvider returns hints from indexed documentation chunks that match the
// caller's query.
type DocsProvider struct {
	search repo.SearchRepo
}

// NewDocsProvider creates a DocsProvider. A nil search repo is tolerated.
func NewDocsProvider(search repo.SearchRepo) *DocsProvider {
	return &DocsProvider{search: search}
}

// Name returns the provider identifier.
func (p *DocsProvider) Name() string { return "docs" }

// GetHints searches the document chunk index for content matching the query.
func (p *DocsProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	if p.search == nil {
		return nil, nil
	}
	if req.Query == "" {
		return nil, nil
	}

	wsIDs := buildDocWorkspaceFilter(req.WorkspaceID)
	chunks, err := p.search.SearchDocs(ctx, wsIDs, req.Query, maxDocResults)
	if err != nil {
		return nil, nil // graceful degradation
	}

	var results []hints.Hint
	for _, chunk := range chunks {
		relevance := scoreDocChunk(chunk, req.Query)
		content := chunk.Content
		if chunk.SectionHeader != "" {
			content = fmt.Sprintf("[%s] %s", chunk.SectionHeader, content)
		}

		results = append(results, hints.Hint{
			Provider:  "docs",
			Category:  "doc",
			Content:   content,
			Relevance: relevance,
			Source:    chunk.FilePath,
		})
	}

	return results, nil
}

// buildDocWorkspaceFilter returns a slice for SearchDocs' workspaceIDs.
func buildDocWorkspaceFilter(wsID string) []string {
	if wsID == "" {
		return nil
	}
	return []string{wsID}
}

// scoreDocChunk computes a relevance score for a documentation chunk.
func scoreDocChunk(chunk *repo.DocChunk, query string) float64 {
	queryLower := strings.ToLower(query)
	contentLower := strings.ToLower(chunk.Content)
	headerLower := strings.ToLower(chunk.SectionHeader)

	// Exact query in section header is a strong match.
	if headerLower != "" && strings.Contains(headerLower, queryLower) {
		return 0.9
	}

	// Exact query as substring of content.
	if strings.Contains(contentLower, queryLower) {
		return 0.7
	}

	// BM25 score from FTS5.
	if chunk.Score < 0 {
		normalized := (-chunk.Score) / 25.0
		if normalized > 1.0 {
			normalized = 1.0
		}
		return 0.3 + normalized*0.4 // range [0.3, 0.7]
	}

	return 0.3 // baseline
}
