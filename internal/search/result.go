package search

import "strings"

// CompactResult is the standardised search result returned by all search
// operations (docs, code, memory). It carries enough context for an agent
// to decide whether to fetch the full content, without blowing up the
// context window.
type CompactResult struct {
	// Source identifies the origin: file path, memory scope, etc.
	Source string `json:"source"`
	// Location is a human-readable position: "lines 10-25", section header, etc.
	Location string `json:"location,omitempty"`
	// Snippet is a short extract around the match (max ~150 chars).
	Snippet string `json:"snippet"`
	// Score is the relevance score (BM25 or hybrid). Lower BM25 = more relevant.
	Score float64 `json:"score"`
	// Kind distinguishes result types: "doc", "symbol", "memory", "code_content".
	Kind string `json:"kind"`
	// ID is the unique identifier for the matched item.
	ID string `json:"id,omitempty"`
	// Metadata carries extra type-specific fields.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TruncateSnippet truncates content to maxLen chars, appending "..." if
// truncated. It tries to break at a word boundary to avoid splitting
// mid-word.
func TruncateSnippet(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	// Try to break at last space before maxLen.
	truncated := content[:maxLen]
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}

// ExtractSnippet finds the portion of content around a query match and
// returns a snippet of maxLen chars centered on the match. If the query
// is not found, returns the first maxLen chars.
func ExtractSnippet(content, query string, maxLen int) string {
	lower := strings.ToLower(content)
	queryLower := strings.ToLower(query)
	idx := strings.Index(lower, queryLower)
	if idx < 0 {
		return TruncateSnippet(content, maxLen)
	}
	// Center the snippet around the match.
	start := idx - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(content) {
		end = len(content)
		start = end - maxLen
		if start < 0 {
			start = 0
		}
	}
	snippet := content[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}
	return snippet
}
