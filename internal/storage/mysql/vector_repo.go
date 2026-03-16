package mysql

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// VectorRepo implements repo.VectorRepo for MySQL, storing float32 embeddings
// as little-endian BLOB columns.
type VectorRepo struct {
	db *sql.DB
}

// UpsertSymbolEmbedding stores or updates the embedding for a symbol.
func (r *VectorRepo) UpsertSymbolEmbedding(ctx context.Context, symbolID, workspaceID string, embedding []float32, dim int, model string) error {
	blob := myEncodeEmbedding(embedding)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO symbol_embeddings (symbol_id, workspace_id, embedding, dim, model, updated_at)
		 VALUES (?, ?, ?, ?, ?, NOW())
		 ON DUPLICATE KEY UPDATE
		   workspace_id = VALUES(workspace_id),
		   embedding    = VALUES(embedding),
		   dim          = VALUES(dim),
		   model        = VALUES(model),
		   updated_at   = NOW()`,
		symbolID, workspaceID, blob, dim, model,
	)
	if err != nil {
		return fmt.Errorf("mysql.VectorRepo.UpsertSymbolEmbedding: %w", err)
	}
	return nil
}

// UpsertDocChunkEmbedding stores or updates the embedding for a doc chunk.
func (r *VectorRepo) UpsertDocChunkEmbedding(ctx context.Context, chunkID, workspaceID string, embedding []float32, dim int, model string) error {
	blob := myEncodeEmbedding(embedding)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO doc_chunk_embeddings (chunk_id, workspace_id, embedding, dim, model, updated_at)
		 VALUES (?, ?, ?, ?, ?, NOW())
		 ON DUPLICATE KEY UPDATE
		   workspace_id = VALUES(workspace_id),
		   embedding    = VALUES(embedding),
		   dim          = VALUES(dim),
		   model        = VALUES(model),
		   updated_at   = NOW()`,
		chunkID, workspaceID, blob, dim, model,
	)
	if err != nil {
		return fmt.Errorf("mysql.VectorRepo.UpsertDocChunkEmbedding: %w", err)
	}
	return nil
}

// GetSymbolEmbeddings retrieves all embeddings for symbols in the given workspaces.
func (r *VectorRepo) GetSymbolEmbeddings(ctx context.Context, workspaceIDs []string) ([]repo.EmbeddingRecord, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}

	placeholders := myVecPlaceholders(len(workspaceIDs))
	args := make([]any, len(workspaceIDs))
	for i, id := range workspaceIDs {
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT symbol_id, workspace_id, embedding, dim
		 FROM symbol_embeddings
		 WHERE workspace_id IN (%s)`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql.VectorRepo.GetSymbolEmbeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return myScanEmbeddingRecords(rows)
}

// GetDocChunkEmbeddings retrieves all embeddings for doc chunks in the given workspaces.
func (r *VectorRepo) GetDocChunkEmbeddings(ctx context.Context, workspaceIDs []string) ([]repo.EmbeddingRecord, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}

	placeholders := myVecPlaceholders(len(workspaceIDs))
	args := make([]any, len(workspaceIDs))
	for i, id := range workspaceIDs {
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT chunk_id, workspace_id, embedding, dim
		 FROM doc_chunk_embeddings
		 WHERE workspace_id IN (%s)`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql.VectorRepo.GetDocChunkEmbeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return myScanEmbeddingRecords(rows)
}

// DeleteSymbolEmbedding removes the embedding for a symbol.
func (r *VectorRepo) DeleteSymbolEmbedding(ctx context.Context, symbolID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM symbol_embeddings WHERE symbol_id = ?`, symbolID)
	if err != nil {
		return fmt.Errorf("mysql.VectorRepo.DeleteSymbolEmbedding: %w", err)
	}
	return nil
}

// DeleteDocChunkEmbedding removes the embedding for a doc chunk.
func (r *VectorRepo) DeleteDocChunkEmbedding(ctx context.Context, chunkID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM doc_chunk_embeddings WHERE chunk_id = ?`, chunkID)
	if err != nil {
		return fmt.Errorf("mysql.VectorRepo.DeleteDocChunkEmbedding: %w", err)
	}
	return nil
}

// CountSymbolEmbeddings returns the number of symbol embeddings for the given workspaces.
func (r *VectorRepo) CountSymbolEmbeddings(ctx context.Context, workspaceIDs []string) (int, error) {
	if len(workspaceIDs) == 0 {
		return 0, nil
	}

	placeholders := myVecPlaceholders(len(workspaceIDs))
	args := make([]any, len(workspaceIDs))
	for i, id := range workspaceIDs {
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT COUNT(*) FROM symbol_embeddings WHERE workspace_id IN (%s)`, placeholders)

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mysql.VectorRepo.CountSymbolEmbeddings: %w", err)
	}
	return count, nil
}

// ---------- BLOB serialisation helpers ----------

func myEncodeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func myDecodeEmbedding(blob []byte, dim int) []float32 {
	if len(blob) != dim*4 {
		return nil
	}
	v := make([]float32, dim)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return v
}

func myScanEmbeddingRecords(rows *sql.Rows) ([]repo.EmbeddingRecord, error) {
	var records []repo.EmbeddingRecord
	for rows.Next() {
		var (
			id          string
			workspaceID string
			blob        []byte
			dim         int
		)
		if err := rows.Scan(&id, &workspaceID, &blob, &dim); err != nil {
			return nil, fmt.Errorf("mysql.myScanEmbeddingRecords: %w", err)
		}

		embedding := myDecodeEmbedding(blob, dim)
		if embedding == nil {
			continue
		}

		records = append(records, repo.EmbeddingRecord{
			ID:          id,
			WorkspaceID: workspaceID,
			Embedding:   embedding,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.myScanEmbeddingRecords: %w", err)
	}
	return records, nil
}

func myVecPlaceholders(count int) string {
	parts := make([]string, count)
	for i := 0; i < count; i++ {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
