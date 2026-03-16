package repo

import (
	"context"
)

// Symbol represents a code symbol extracted from source files.
type Symbol struct {
	ID          string
	WorkspaceID string
	FileID      int64
	Name        string
	Kind        string
	StartLine   int
	EndLine     int
	Signature   string
	// FilePath is the workspace-relative file path resolved from file_hashes.
	// Populated by search queries that JOIN file_hashes; empty for direct lookups.
	FilePath string
	// Score holds the BM25 relevance score when returned by FTS5 search.
	// Lower (more negative) values indicate higher relevance. Zero means
	// the result was produced by a LIKE fallback query with no ranking.
	Score float64
}

// SymbolRepo handles CRUD operations for symbols and file hashes.
type SymbolRepo interface {
	UpsertFileHash(ctx context.Context, workspaceID, filePath, hash string) (int64, error)
	GetFileHash(ctx context.Context, workspaceID, filePath string) (string, error)
	Upsert(ctx context.Context, sym *Symbol) error
	GetFileSymbols(ctx context.Context, workspaceID, filePath string) ([]*Symbol, error)
	DeleteByFile(ctx context.Context, fileID int64) error
	DeleteByWorkspacePath(ctx context.Context, workspaceID, filePath string) error
}
