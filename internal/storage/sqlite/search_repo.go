package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/hyperax/hyperax/internal/repo"
)

// SearchRepo implements repo.SearchRepo for SQLite with FTS5 support and
// automatic LIKE fallback when FTS5 tables are unavailable.
type SearchRepo struct {
	db *sql.DB
	// ftsAvailable caches whether FTS5 tables exist. It is lazily initialised
	// on the first search call and remains valid for the lifetime of the repo.
	// A nil value means the check has not yet been performed.
	ftsAvailable *bool
}

// ---------- FTS5 availability detection ----------

// checkFTSAvailable probes the database for the symbols_fts table. The result
// is cached so subsequent calls are free.
func (r *SearchRepo) checkFTSAvailable(ctx context.Context) bool {
	if r.ftsAvailable != nil {
		return *r.ftsAvailable
	}
	available := false
	// A lightweight probe: SELECT from the FTS table with an impossible match.
	// If the table does not exist the query will error.
	row := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM symbols_fts WHERE symbols_fts MATCH '\"__hyperax_fts_probe__\"' LIMIT 1")
	var n int
	if row.Scan(&n) == nil {
		available = true
	}
	r.ftsAvailable = &available
	return available
}

// ---------- FTS5 query sanitiser ----------

// sanitizeFTSQuery converts a user-supplied search string into a valid FTS5
// MATCH expression. Each alphanumeric token is turned into a prefix query
// (e.g. "foo" becomes "foo*") and tokens are joined with implicit AND
// (space-separated). FTS5 special characters (* + - : " ( ) ^ { } ~) are
// stripped so they cannot break the query syntax.
func sanitizeFTSQuery(query string) string {
	// Remove characters that have special meaning in FTS5.
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '*', '+', '-', '"', '(', ')', ':', '^', '{', '}', '~':
			return ' '
		default:
			return r
		}
	}, query)

	// Split on whitespace, drop empties, append prefix wildcard.
	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return ""
	}

	terms := make([]string, 0, len(words))
	for _, w := range words {
		// Extra safety: ensure the token contains at least one alphanumeric.
		hasAlnum := false
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				hasAlnum = true
				break
			}
		}
		if hasAlnum {
			terms = append(terms, w+"*")
		}
	}

	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " ")
}

// ---------- SearchSymbols ----------

// SearchSymbols finds symbols whose name matches the query, scoped to the
// given workspace IDs. An optional kind filter narrows results further.
// Results are ranked by BM25 relevance when FTS5 is available, otherwise
// ordered alphabetically by name. Results are capped at limit.
func (r *SearchRepo) SearchSymbols(ctx context.Context, workspaceIDs []string, query string, kind string, limit int) ([]*repo.Symbol, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	if r.checkFTSAvailable(ctx) {
		results, err := r.searchSymbolsFTS(ctx, workspaceIDs, query, kind, limit)
		if err == nil {
			return results, nil
		}
		// FTS5 query failed (e.g. bad MATCH syntax). Fall through to LIKE.
	}
	return r.searchSymbolsLIKE(ctx, workspaceIDs, query, kind, limit)
}

// searchSymbolsFTS executes a full-text search against symbols_fts with BM25
// ranking. Weights: name=10, signature=5, kind=1.
func (r *SearchRepo) searchSymbolsFTS(ctx context.Context, workspaceIDs []string, query, kind string, limit int) ([]*repo.Symbol, error) {
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		// Empty sanitised query: fall through to LIKE which handles "%" patterns.
		return nil, fmt.Errorf("empty FTS query after sanitisation")
	}

	placeholders := strings.Repeat("?,", len(workspaceIDs)-1) + "?"

	args := make([]any, 0, len(workspaceIDs)+3)
	args = append(args, ftsQuery)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}

	qb := fmt.Sprintf(
		`SELECT s.id, s.file_id, s.name, s.kind, s.start_line, s.end_line,
		        COALESCE(s.signature, ''), s.workspace_id, fh.file_path,
		        bm25(symbols_fts, 10.0, 5.0, 1.0) AS rank
		 FROM symbols_fts
		 JOIN symbols s ON symbols_fts.rowid = s.id
		 JOIN file_hashes fh ON s.file_id = fh.file_id
		 WHERE symbols_fts MATCH ?
		   AND s.workspace_id IN (%s)`, placeholders)

	if kind != "" {
		qb += " AND s.kind = ?"
		args = append(args, kind)
	}

	qb += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsFTS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*repo.Symbol
	for rows.Next() {
		sym := &repo.Symbol{}
		var id int64
		if err := rows.Scan(&id, &sym.FileID, &sym.Name, &sym.Kind, &sym.StartLine, &sym.EndLine, &sym.Signature, &sym.WorkspaceID, &sym.FilePath, &sym.Score); err != nil {
			return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsFTS: %w", err)
		}
		sym.ID = strconv.FormatInt(id, 10)
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsFTS: %w", err)
	}
	return symbols, nil
}

// searchSymbolsLIKE is the legacy LIKE-based search used as a fallback when
// FTS5 is unavailable or the FTS query is invalid.
func (r *SearchRepo) searchSymbolsLIKE(ctx context.Context, workspaceIDs []string, query, kind string, limit int) ([]*repo.Symbol, error) {
	placeholders := strings.Repeat("?,", len(workspaceIDs)-1) + "?"

	args := make([]any, 0, len(workspaceIDs)+3)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}
	args = append(args, "%"+query+"%")

	qb := fmt.Sprintf(
		`SELECT s.id, s.file_id, s.name, s.kind, s.start_line, s.end_line,
		        COALESCE(s.signature, ''), s.workspace_id, fh.file_path
		 FROM symbols s
		 JOIN file_hashes fh ON s.file_id = fh.file_id
		 WHERE s.workspace_id IN (%s)
		   AND s.name LIKE ?`, placeholders)

	if kind != "" {
		qb += " AND s.kind = ?"
		args = append(args, kind)
	}

	qb += " ORDER BY s.name LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsLIKE: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*repo.Symbol
	for rows.Next() {
		sym := &repo.Symbol{}
		var id int64
		if err := rows.Scan(&id, &sym.FileID, &sym.Name, &sym.Kind, &sym.StartLine, &sym.EndLine, &sym.Signature, &sym.WorkspaceID, &sym.FilePath); err != nil {
			return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsLIKE: %w", err)
		}
		sym.ID = strconv.FormatInt(id, 10)
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchSymbolsLIKE: %w", err)
	}
	return symbols, nil
}

// ---------- UpsertDocChunk ----------

// UpsertDocChunk inserts or replaces a documentation chunk. The unique
// constraint is (workspace_id, file_path, chunk_index). If ContentType is
// empty it defaults to "doc" for backwards compatibility.
func (r *SearchRepo) UpsertDocChunk(ctx context.Context, chunk *repo.DocChunk) error {
	ct := chunk.ContentType
	if ct == "" {
		ct = "doc"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO doc_chunks (workspace_id, file_path, file_hash, chunk_index, section_header, content, token_count, content_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id, file_path, chunk_index)
		 DO UPDATE SET
		   file_hash      = excluded.file_hash,
		   section_header = excluded.section_header,
		   content        = excluded.content,
		   token_count    = excluded.token_count,
		   content_type   = excluded.content_type`,
		chunk.WorkspaceID, chunk.FilePath, chunk.FileHash, chunk.ChunkIndex,
		chunk.SectionHeader, chunk.Content, chunk.TokenCount, ct,
	)
	if err != nil {
		return fmt.Errorf("sqlite.SearchRepo.UpsertDocChunk: %w", err)
	}
	return nil
}

// ---------- DeleteDocChunksByPath ----------

// DeleteDocChunksByPath removes all documentation chunks for a file identified
// by workspace ID and path. This is used when a file is deleted from disk.
func (r *SearchRepo) DeleteDocChunksByPath(ctx context.Context, workspaceID, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM doc_chunks WHERE workspace_id = ? AND file_path = ?`,
		workspaceID, filePath)
	if err != nil {
		return fmt.Errorf("sqlite.SearchRepo.DeleteDocChunksByPath: %w", err)
	}
	return nil
}

// ---------- SearchDocs ----------

// SearchDocs finds documentation chunks whose content or section header
// matches the query, scoped to the given workspace IDs. Only chunks with
// content_type='doc' (or NULL for pre-migration rows) are returned. Results
// are ranked by BM25 relevance when FTS5 is available, otherwise ordered by
// file path and chunk index. Results are capped at limit.
func (r *SearchRepo) SearchDocs(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*repo.DocChunk, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if r.checkFTSAvailable(ctx) {
		results, err := r.searchDocsFTS(ctx, workspaceIDs, query, "doc", limit)
		if err == nil {
			return results, nil
		}
		// FTS5 query failed. Fall through to LIKE.
	}
	return r.searchDocsLIKE(ctx, workspaceIDs, query, "doc", limit)
}

// ---------- SearchCodeContent ----------

// SearchCodeContent searches doc_chunks with content_type='code' for source
// file body matches. This enables finding functions that USE a symbol
// internally, not just functions NAMED after it. Results are ranked by BM25
// relevance when FTS5 is available, otherwise ordered by file path and chunk
// index. Results are capped at limit.
func (r *SearchRepo) SearchCodeContent(ctx context.Context, workspaceIDs []string, query string, limit int) ([]*repo.DocChunk, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if r.checkFTSAvailable(ctx) {
		results, err := r.searchDocsFTS(ctx, workspaceIDs, query, "code", limit)
		if err == nil {
			return results, nil
		}
	}
	return r.searchDocsLIKE(ctx, workspaceIDs, query, "code", limit)
}

// searchDocsFTS executes a full-text search against doc_chunks_fts with BM25
// ranking, filtered by content_type. Weights: content=10, section_header=5.
func (r *SearchRepo) searchDocsFTS(ctx context.Context, workspaceIDs []string, query, contentType string, limit int) ([]*repo.DocChunk, error) {
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		return nil, fmt.Errorf("empty FTS query after sanitisation")
	}

	placeholders := strings.Repeat("?,", len(workspaceIDs)-1) + "?"

	args := make([]any, 0, len(workspaceIDs)+4)
	args = append(args, ftsQuery)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}
	args = append(args, contentType, limit)

	qb := fmt.Sprintf(
		`SELECT dc.id, dc.workspace_id, dc.file_path, dc.file_hash, dc.chunk_index,
		        COALESCE(dc.section_header, ''), dc.content, dc.token_count,
		        COALESCE(dc.content_type, 'doc'),
		        bm25(doc_chunks_fts, 10.0, 5.0) AS rank
		 FROM doc_chunks_fts
		 JOIN doc_chunks dc ON doc_chunks_fts.rowid = dc.id
		 WHERE doc_chunks_fts MATCH ?
		   AND dc.workspace_id IN (%s)
		   AND COALESCE(dc.content_type, 'doc') = ?
		 ORDER BY rank
		 LIMIT ?`, placeholders)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsFTS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []*repo.DocChunk
	for rows.Next() {
		c := &repo.DocChunk{}
		var id int64
		if err := rows.Scan(&id, &c.WorkspaceID, &c.FilePath, &c.FileHash, &c.ChunkIndex, &c.SectionHeader, &c.Content, &c.TokenCount, &c.ContentType, &c.Score); err != nil {
			return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsFTS: %w", err)
		}
		c.ID = strconv.FormatInt(id, 10)
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsFTS: %w", err)
	}
	return chunks, nil
}

// searchDocsLIKE is the legacy LIKE-based search used as a fallback when FTS5
// is unavailable or the FTS query is invalid. Filters by content_type.
func (r *SearchRepo) searchDocsLIKE(ctx context.Context, workspaceIDs []string, query, contentType string, limit int) ([]*repo.DocChunk, error) {
	placeholders := strings.Repeat("?,", len(workspaceIDs)-1) + "?"

	args := make([]any, 0, len(workspaceIDs)+4)
	for _, id := range workspaceIDs {
		args = append(args, id)
	}
	likePattern := "%" + query + "%"
	args = append(args, likePattern, likePattern, contentType, limit)

	qb := fmt.Sprintf(
		`SELECT id, workspace_id, file_path, file_hash, chunk_index,
		        COALESCE(section_header, ''), content, token_count,
		        COALESCE(content_type, 'doc')
		 FROM doc_chunks
		 WHERE workspace_id IN (%s)
		   AND (content LIKE ? OR section_header LIKE ?)
		   AND COALESCE(content_type, 'doc') = ?
		 ORDER BY file_path, chunk_index
		 LIMIT ?`, placeholders)

	rows, err := r.db.QueryContext(ctx, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsLIKE: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []*repo.DocChunk
	for rows.Next() {
		c := &repo.DocChunk{}
		var id int64
		if err := rows.Scan(&id, &c.WorkspaceID, &c.FilePath, &c.FileHash, &c.ChunkIndex, &c.SectionHeader, &c.Content, &c.TokenCount, &c.ContentType); err != nil {
			return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsLIKE: %w", err)
		}
		c.ID = strconv.FormatInt(id, 10)
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SearchRepo.searchDocsLIKE: %w", err)
	}
	return chunks, nil
}
