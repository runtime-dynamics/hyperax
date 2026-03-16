package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/repo"
)

// maxPatternResults limits the number of symbols fetched per search query.
const maxPatternResults = 20

// PatternsProvider returns hints about code patterns by querying the symbol
// and search stores for related symbols.
type PatternsProvider struct {
	symbols repo.SymbolRepo
	search  repo.SearchRepo
}

// NewPatternsProvider creates a PatternsProvider. Nil repos are tolerated.
func NewPatternsProvider(symbols repo.SymbolRepo, search repo.SearchRepo) *PatternsProvider {
	return &PatternsProvider{symbols: symbols, search: search}
}

// Name returns the provider identifier.
func (p *PatternsProvider) Name() string { return "patterns" }

// GetHints looks for code symbols related to the query or file path.
//
// When a file path is given the provider also looks up symbols in that file
// to surface related patterns. When only a query is provided it uses the
// search repo's FTS5 index.
func (p *PatternsProvider) GetHints(ctx context.Context, req *hints.HintRequest) ([]hints.Hint, error) {
	var results []hints.Hint

	// Strategy 1: search symbols via FTS5 using the query.
	if p.search != nil && req.Query != "" {
		wsIDs := buildWorkspaceFilter(req.WorkspaceID)
		symbols, err := p.search.SearchSymbols(ctx, wsIDs, req.Query, "", maxPatternResults)
		if err == nil {
			for _, sym := range symbols {
				relevance := scoreSymbol(sym, req.Query)
				results = append(results, hints.Hint{
					Provider:  "patterns",
					Category:  "pattern",
					Content:   formatSymbolHint(sym),
					Relevance: relevance,
					Source:    sym.Name,
				})
			}
		}
	}

	// Strategy 2: if a file path is given, fetch its symbols for context.
	if p.symbols != nil && req.FilePath != "" && req.WorkspaceID != "" {
		fileSymbols, err := p.symbols.GetFileSymbols(ctx, req.WorkspaceID, req.FilePath)
		if err == nil {
			for _, sym := range fileSymbols {
				relevance := 0.3 // contextual presence in the active file
				if req.Query != "" && strings.Contains(strings.ToLower(sym.Name), strings.ToLower(req.Query)) {
					relevance = 0.6
				}
				results = append(results, hints.Hint{
					Provider:  "patterns",
					Category:  "pattern",
					Content:   formatSymbolHint(sym),
					Relevance: relevance,
					Source:    req.FilePath,
				})
			}
		}
	}

	return results, nil
}

// buildWorkspaceFilter returns a slice suitable for SearchSymbols' workspaceIDs
// parameter. An empty workspace ID returns nil (search all).
func buildWorkspaceFilter(wsID string) []string {
	if wsID == "" {
		return nil
	}
	return []string{wsID}
}

// formatSymbolHint creates a human-readable description of a symbol.
func formatSymbolHint(sym *repo.Symbol) string {
	desc := fmt.Sprintf("%s %s", sym.Kind, sym.Name)
	if sym.Signature != "" {
		desc += " — " + sym.Signature
	}
	if sym.StartLine > 0 {
		desc += fmt.Sprintf(" (lines %d-%d)", sym.StartLine, sym.EndLine)
	}
	return desc
}

// scoreSymbol computes a relevance score for a search-returned symbol.
func scoreSymbol(sym *repo.Symbol, query string) float64 {
	nameLower := strings.ToLower(sym.Name)
	queryLower := strings.ToLower(query)

	// Exact match on symbol name is highest relevance.
	if nameLower == queryLower {
		return 1.0
	}

	// Symbol name contains the query as a substring.
	if strings.Contains(nameLower, queryLower) {
		return 0.7
	}

	// File extension / kind heuristic: if the query appears in the signature,
	// that is a moderate match.
	if sym.Signature != "" && strings.Contains(strings.ToLower(sym.Signature), queryLower) {
		return 0.5
	}

	// BM25 score from FTS5 (lower is more relevant, negative values).
	if sym.Score < 0 {
		// Normalize: typical BM25 range is roughly -25 .. 0.
		normalized := (-sym.Score) / 25.0
		if normalized > 1.0 {
			normalized = 1.0
		}
		return 0.3 + normalized*0.3 // range [0.3, 0.6]
	}

	return 0.3 // baseline for any search-returned symbol
}
