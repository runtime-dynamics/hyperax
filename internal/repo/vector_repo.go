package repo

import "context"

// VectorRepo handles storage and retrieval of vector embeddings for symbols
// and document chunks. Distance computation (cosine similarity) is performed
// in Go, not in SQL, since the project uses pure-Go SQLite without the
// sqlite-vec extension.
type VectorRepo interface {
	// UpsertSymbolEmbedding stores or updates the embedding for a symbol.
	UpsertSymbolEmbedding(ctx context.Context, symbolID, workspaceID string, embedding []float32, dim int, model string) error

	// UpsertDocChunkEmbedding stores or updates the embedding for a doc chunk.
	UpsertDocChunkEmbedding(ctx context.Context, chunkID, workspaceID string, embedding []float32, dim int, model string) error

	// GetSymbolEmbeddings retrieves all embeddings for symbols in the given workspaces.
	// Returns symbolID -> embedding pairs. Used by the vector search leg.
	GetSymbolEmbeddings(ctx context.Context, workspaceIDs []string) ([]EmbeddingRecord, error)

	// GetDocChunkEmbeddings retrieves all embeddings for doc chunks in the given workspaces.
	GetDocChunkEmbeddings(ctx context.Context, workspaceIDs []string) ([]EmbeddingRecord, error)

	// DeleteSymbolEmbedding removes the embedding for a symbol.
	DeleteSymbolEmbedding(ctx context.Context, symbolID string) error

	// DeleteDocChunkEmbedding removes the embedding for a doc chunk.
	DeleteDocChunkEmbedding(ctx context.Context, chunkID string) error

	// CountSymbolEmbeddings returns the number of symbol embeddings for the given workspaces.
	CountSymbolEmbeddings(ctx context.Context, workspaceIDs []string) (int, error)
}

// EmbeddingRecord holds a stored embedding with its associated entity ID.
type EmbeddingRecord struct {
	ID          string    // symbol_id or chunk_id
	WorkspaceID string
	Embedding   []float32
}
