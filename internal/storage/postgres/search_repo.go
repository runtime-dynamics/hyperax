package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// SearchRepo implements repo.SearchRepo for PostgreSQL.
// Uses ILIKE-based search (no FTS5). PostgreSQL full-text search via tsvector
// can be added later for improved relevance ranking.
type SearchRepo struct {
	db *sql.DB
}

// SearchSymbols finds symbols whose name matches the query, scoped to the
// given workspace IDs. Results are ordered alphabetically, capped at limit.
func (r *SearchRepo) SearchSymbols(ctx context.Context, workspaceIDs []string, query string, kind string, limit int) ([]*repo.Symbol, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	args := make([]any, 0, len(workspaceIDs)+3)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}
	placeholders := pgPlaceholders(1, len(workspaceIDs))

	args = append(args, "%"+query+"%")
	paramIdx := len(workspaceIDs) + 1

	qb := fmt.Sprintf(
		`SELECT s.id, s.file_id, s.name, s.kind, s.start_line, s.end_line,
		        COALESCE(s.signature, ''), s.workspace_id, fh.file_path
		 FROM symbols s
		 JOIN file_hashes fh ON s.file_id = fh.file_id
		 WHERE s.workspace_id IN (%s)
		   AND s.name ILIKE $%d`, placeholders, paramIdx)
	paramIdx++

	if kind != "" {
		qb += fmt.Sprintf(" AND s.kind = $%d", paramIdx)
		args = append(args, kind)
		paramIdx++
	}

	qb += fmt.Sprintf(" ORDER BY s.name LIMIT $%d", paramIdx)
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.SearchRepo.SearchSymbols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*repo.Symbol
	for rows.Next() {
		sym := &repo.Symbol{}
		var id int64
		if err := rows.Scan(&id, &sym.FileID, &sym.Name, &sym.Kind, &sym.StartLine, &sym.EndLine, &sym.Signature, &sym.WorkspaceID, &sym.FilePath); err != nil {
			return nil, fmt.Errorf("postgres.SearchRepo.SearchSymbols: %w", err)
		}
		sym.ID = strconv.FormatInt(id, 10)
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.SearchRepo.SearchSymbols: %w", err)
	}
	return symbols, nil
}

// UpsertDocChunk inserts or replaces a documentation chunk. If ContentType is
// empty it defaults to "doc" for backwards compatibility.
func (r *SearchRepo) UpsertDocChunk(ctx context.Context, chunk *repo.DocChunk) error {
	ct := chunk.ContentType
	if ct == "" {
		ct = "doc"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO doc_chunks (workspace_id, file_path, file_hash, chunk_index, section_header, content, token_count, content_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT(workspace_id, file_path, chunk_index)
		 DO UPDATE SET
		   file_hash      = EXCLUDED.file_hash,
		   section_header = EXCLUDED.section_header,
		   content        = EXCLUDED.content,
		   token_count    = EXCLUDED.token_count,
		   content_type   = EXCLUDED.content_type`,
		chunk.WorkspaceID, chunk.FilePath, chunk.FileHash, chunk.ChunkIndex,
		chunk.SectionHeader, chunk.Content, chunk.TokenCount, ct,
	)
	if err != nil {
		return fmt.Errorf("postgres.SearchRepo.UpsertDocChunk: %w", err)
	}
	return nil
}

// DeleteDocChunksByPath removes all documentation chunks for a file identified
// by workspace ID and path. This is used when a file is deleted from disk.
func (r *SearchRepo) DeleteDocChunksByPath(ctx context.Context, workspaceID, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM doc_chunks WHERE workspace_id = $1 AND file_path = $2`,
		workspaceID, filePath)
	if err != nil {
		return fmt.Errorf("postgres.SearchRepo.DeleteDocChunksByPath: %w", err)
	}
	return nil
}

// SearchDocs finds documentation chunks matching the query via ILIKE. Only
// chunks with content_type='doc' are returned.
func (r *SearchRepo) SearchDocs(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*repo.DocChunk, error) {
	return r.searchDocsByType(ctx, workspaceIDs, query, "doc", limit)
}

// SearchCodeContent searches doc_chunks with content_type='code' for source
// file body matches. This enables finding functions that USE a symbol
// internally, not just functions NAMED after it.
func (r *SearchRepo) SearchCodeContent(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*repo.DocChunk, error) {
	return r.searchDocsByType(ctx, workspaceIDs, query, "code", limit)
}

// searchDocsByType finds doc_chunks matching the query via ILIKE, filtered by
// content_type. Shared implementation for SearchDocs and SearchCodeContent.
func (r *SearchRepo) searchDocsByType(ctx context.Context, workspaceIDs []string, query, contentType string, limit int) ([]*repo.DocChunk, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	args := make([]any, 0, len(workspaceIDs)+4)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}
	placeholders := pgPlaceholders(1, len(workspaceIDs))
	paramIdx := len(workspaceIDs) + 1

	likePattern := "%" + query + "%"
	args = append(args, likePattern, likePattern, contentType, limit)

	qb := fmt.Sprintf(
		`SELECT id, workspace_id, file_path, file_hash, chunk_index,
		        COALESCE(section_header, ''), content, token_count,
		        COALESCE(content_type, 'doc')
		 FROM doc_chunks
		 WHERE workspace_id IN (%s)
		   AND (content ILIKE $%d OR section_header ILIKE $%d)
		   AND COALESCE(content_type, 'doc') = $%d
		 ORDER BY file_path, chunk_index
		 LIMIT $%d`, placeholders, paramIdx, paramIdx+1, paramIdx+2, paramIdx+3)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.SearchRepo.searchDocsByType: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []*repo.DocChunk
	for rows.Next() {
		c := &repo.DocChunk{}
		var id int64
		if err := rows.Scan(&id, &c.WorkspaceID, &c.FilePath, &c.FileHash, &c.ChunkIndex, &c.SectionHeader, &c.Content, &c.TokenCount, &c.ContentType); err != nil {
			return nil, fmt.Errorf("postgres.SearchRepo.searchDocsByType: %w", err)
		}
		c.ID = strconv.FormatInt(id, 10)
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.SearchRepo.searchDocsByType: %w", err)
	}
	return chunks, nil
}

// pgPlaceholders generates "$start, $start+1, ..., $start+count-1".
func pgPlaceholders(start, count int) string {
	parts := make([]string, count)
	for i := 0; i < count; i++ {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}
