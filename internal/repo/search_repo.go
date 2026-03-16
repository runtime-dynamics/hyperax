package repo

import "context"

// DocChunk is a section of a document or source file indexed for search.
type DocChunk struct {
	ID            string
	WorkspaceID   string
	FilePath      string
	FileHash      string
	ChunkIndex    int
	SectionHeader string
	Content       string
	TokenCount    int
	// ContentType discriminates between documentation chunks ("doc") and
	// source code content chunks ("code"). Defaults to "doc" for backwards
	// compatibility with existing callers.
	ContentType string
	// Score holds the BM25 relevance score when returned by FTS5 search.
	// Lower (more negative) values indicate higher relevance. Zero means
	// the result was produced by a LIKE fallback query with no ranking.
	Score float64
}

// SearchRepo handles FTS5 queries and document chunk storage.
type SearchRepo interface {
	SearchSymbols(ctx context.Context, workspaceIDs []string, query string, kind string, limit int) ([]*Symbol, error)
	UpsertDocChunk(ctx context.Context, chunk *DocChunk) error
	SearchDocs(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*DocChunk, error)
	// SearchCodeContent searches doc_chunks with content_type='code' for
	// source file body matches. This enables finding functions that USE a
	// symbol internally, not just functions NAMED after it.
	SearchCodeContent(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*DocChunk, error)
	DeleteDocChunksByPath(ctx context.Context, workspaceID, filePath string) error
}
