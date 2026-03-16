package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// MemoryRepo implements repo.MemoryRepo for SQLite using the memories table
// with FTS5 full-text search.
type MemoryRepo struct {
	db *sql.DB
}

// Store inserts a new memory entry and returns its generated ID.
func (r *MemoryRepo) Store(ctx context.Context, memory *types.Memory) (string, error) {
	if memory.ID == "" {
		memory.ID = uuid.New().String()
	}

	metadata := "{}"
	if memory.Metadata != nil {
		b, err := json.Marshal(memory.Metadata)
		if err != nil {
			return "", fmt.Errorf("sqlite.MemoryRepo.Store: %w", err)
		}
		metadata = string(b)
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memories (id, scope, type, content, workspace_id, persona_id, metadata, created_at, accessed_at, access_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		memory.ID, string(memory.Scope), string(memory.Type), memory.Content,
		nullableString(memory.WorkspaceID), nullableString(memory.PersonaID),
		metadata, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.MemoryRepo.Store: %w", err)
	}
	// FTS5 index is auto-synced via trigger (migration 039).

	return memory.ID, nil
}

// Get retrieves a single memory by ID.
func (r *MemoryRepo) Get(ctx context.Context, id string) (*types.Memory, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories WHERE id = ?`, id,
	)

	m, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo.MemoryRepo.Get: memory %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.Get: %w", err)
	}
	return m, nil
}

// Delete removes a memory by ID.
// FTS5 index is auto-cleaned via trigger (migration 039).
func (r *MemoryRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite.MemoryRepo.Delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("repo.MemoryRepo.Delete: memory %q not found", id)
	}
	return nil
}

// Recall searches memories using FTS5 BM25 ranking. Falls back to LIKE if
// FTS5 is unavailable or returns an error. Results are filtered by scope,
// workspaceID, and personaID. Contested memories (non-NULL contested_by) and
// consolidated memories (non-NULL consolidated_into) are excluded.
func (r *MemoryRepo) Recall(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	if query == "" {
		return nil, nil
	}

	// Try FTS5 first.
	results, err := r.recallFTS5(ctx, query, scope, workspaceID, personaID, limit)
	if err != nil {
		// FTS5 unavailable — fall back to LIKE.
		return r.recallLike(ctx, query, scope, workspaceID, personaID, limit)
	}
	return results, nil
}

// recallFTS5 uses FTS5 MATCH for BM25-ranked retrieval.
func (r *MemoryRepo) recallFTS5(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	var where []string
	var args []any

	// Join memories with memory_fts via the memory_id column (migration 039).
	where = append(where, "m.id IN (SELECT memory_id FROM memory_fts WHERE memory_fts MATCH ?)")
	args = append(args, ftsQuery(query))

	// Exclude consolidated and contested memories.
	where = append(where, "m.consolidated_into IS NULL")
	where = append(where, "m.contested_by IS NULL")

	if scope != "" {
		where = append(where, "m.scope = ?")
		args = append(args, string(scope))
	}
	if workspaceID != "" {
		where = append(where, "(m.workspace_id = ? OR m.workspace_id IS NULL)")
		args = append(args, workspaceID)
	}
	if personaID != "" {
		where = append(where, "(m.persona_id = ? OR m.persona_id IS NULL)")
		args = append(args, personaID)
	}

	args = append(args, limit)

	// Use BM25 rank from FTS5 for ordering when available.
	q := fmt.Sprintf(
		`SELECT m.id, m.scope, m.type, m.content, m.workspace_id, m.persona_id, m.metadata,
		        m.created_at, m.accessed_at, m.access_count, m.consolidated_into, m.contested_by, m.contested_at
		 FROM memories m
		 WHERE %s
		 ORDER BY m.accessed_at DESC
		 LIMIT ?`,
		strings.Join(where, " AND "),
	)

	return r.queryMemories(ctx, q, args...)
}

// recallLike uses LIKE-based search as a fallback when FTS5 is unavailable.
func (r *MemoryRepo) recallLike(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	var where []string
	var args []any

	where = append(where, "content LIKE ?")
	args = append(args, "%"+query+"%")

	where = append(where, "consolidated_into IS NULL")
	where = append(where, "contested_by IS NULL")

	if scope != "" {
		where = append(where, "scope = ?")
		args = append(args, string(scope))
	}
	if workspaceID != "" {
		where = append(where, "(workspace_id = ? OR workspace_id IS NULL)")
		args = append(args, workspaceID)
	}
	if personaID != "" {
		where = append(where, "(persona_id = ? OR persona_id IS NULL)")
		args = append(args, personaID)
	}

	args = append(args, limit)

	q := fmt.Sprintf(
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories
		 WHERE %s
		 ORDER BY accessed_at DESC
		 LIMIT ?`,
		strings.Join(where, " AND "),
	)

	return r.queryMemories(ctx, q, args...)
}

// TouchAccess updates accessed_at and increments access_count.
func (r *MemoryRepo) TouchAccess(ctx context.Context, id string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		`UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.MemoryRepo.TouchAccess: %w", err)
	}
	return nil
}

// ListConsolidationCandidates returns episodic memories that haven't been
// accessed in the given number of days. These are candidates for merging
// into semantic memories.
func (r *MemoryRepo) ListConsolidationCandidates(ctx context.Context, scope types.MemoryScope, olderThanDays int, limit int) ([]*types.Memory, error) {
	if limit <= 0 {
		limit = 100
	}

	var where []string
	var args []any

	where = append(where, "type = 'episodic'")
	where = append(where, "consolidated_into IS NULL")
	where = append(where, "contested_by IS NULL")
	where = append(where, "accessed_at < datetime('now', ?)")
	args = append(args, fmt.Sprintf("-%d days", olderThanDays))

	if scope != "" {
		where = append(where, "scope = ?")
		args = append(args, string(scope))
	}

	// Exclude anchored memories.
	where = append(where, "(json_extract(metadata, '$.anchored') IS NULL OR json_extract(metadata, '$.anchored') = 0)")

	args = append(args, limit)

	q := fmt.Sprintf(
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories
		 WHERE %s
		 ORDER BY accessed_at ASC
		 LIMIT ?`,
		strings.Join(where, " AND "),
	)

	return r.queryMemories(ctx, q, args...)
}

// MarkConsolidated sets consolidated_into on the given memory IDs.
func (r *MemoryRepo) MarkConsolidated(ctx context.Context, ids []string, targetID string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, targetID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	q := fmt.Sprintf(
		`UPDATE memories SET consolidated_into = ? WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	_, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("sqlite.MemoryRepo.MarkConsolidated: %w", err)
	}
	return nil
}

// MarkContested flags a memory as contested by another memory.
func (r *MemoryRepo) MarkContested(ctx context.Context, id, contestedByID string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		`UPDATE memories SET contested_by = ?, contested_at = ? WHERE id = ?`,
		contestedByID, now, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.MemoryRepo.MarkContested: %w", err)
	}
	return nil
}

// Count returns the number of active memories for the given scope and workspace.
func (r *MemoryRepo) Count(ctx context.Context, scope types.MemoryScope, workspaceID string) (int, error) {
	var where []string
	var args []any

	where = append(where, "consolidated_into IS NULL")

	if scope != "" {
		where = append(where, "scope = ?")
		args = append(args, string(scope))
	}
	if workspaceID != "" {
		where = append(where, "workspace_id = ?")
		args = append(args, workspaceID)
	}

	q := fmt.Sprintf("SELECT COUNT(*) FROM memories WHERE %s", strings.Join(where, " AND "))

	var count int
	err := r.db.QueryRowContext(ctx, q, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite.MemoryRepo.Count: %w", err)
	}
	return count, nil
}

// CountByType returns memory counts grouped by type.
func (r *MemoryRepo) CountByType(ctx context.Context, scope types.MemoryScope, workspaceID string) (map[types.MemoryType]int, error) {
	var where []string
	var args []any

	where = append(where, "consolidated_into IS NULL")

	if scope != "" {
		where = append(where, "scope = ?")
		args = append(args, string(scope))
	}
	if workspaceID != "" {
		where = append(where, "workspace_id = ?")
		args = append(args, workspaceID)
	}

	q := fmt.Sprintf(
		"SELECT type, COUNT(*) FROM memories WHERE %s GROUP BY type",
		strings.Join(where, " AND "),
	)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.CountByType: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[types.MemoryType]int)
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, fmt.Errorf("sqlite.MemoryRepo.CountByType: %w", err)
		}
		result[types.MemoryType(typ)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.CountByType: %w", err)
	}
	return result, nil
}

// StoreAnnotation inserts a new annotation.
func (r *MemoryRepo) StoreAnnotation(ctx context.Context, ann *types.MemoryAnnotation) (string, error) {
	if ann.ID == "" {
		ann.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memory_annotations (id, memory_id, annotation, annotation_type, created_by)
		 VALUES (?, ?, ?, ?, ?)`,
		ann.ID, ann.MemoryID, ann.Annotation, ann.AnnotationType, ann.CreatedBy,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.MemoryRepo.StoreAnnotation: %w", err)
	}
	return ann.ID, nil
}

// GetAnnotations retrieves all annotations for a given memory.
func (r *MemoryRepo) GetAnnotations(ctx context.Context, memoryID string) ([]*types.MemoryAnnotation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, memory_id, annotation, annotation_type, created_by, created_at
		 FROM memory_annotations WHERE memory_id = ? ORDER BY created_at ASC`,
		memoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.GetAnnotations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var anns []*types.MemoryAnnotation
	for rows.Next() {
		a := &types.MemoryAnnotation{}
		var createdAt string
		if err := rows.Scan(&a.ID, &a.MemoryID, &a.Annotation, &a.AnnotationType, &a.CreatedBy, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.MemoryRepo.GetAnnotations: %w", err)
		}
		a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		anns = append(anns, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.GetAnnotations: %w", err)
	}
	return anns, nil
}

// --- helpers ---

// queryMemories executes a query and scans the results into Memory slices.
func (r *MemoryRepo) queryMemories(ctx context.Context, query string, args ...any) ([]*types.Memory, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.queryMemories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var memories []*types.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.MemoryRepo.queryMemories: %w", err)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.MemoryRepo.queryMemories: %w", err)
	}
	return memories, nil
}

// scanMemory scans a single row (sql.Row) into a Memory.
func scanMemory(row *sql.Row) (*types.Memory, error) {
	m := &types.Memory{}
	var (
		scope, typ, createdAt, accessedAt        string
		workspaceID, personaID, metadataStr      sql.NullString
		consolidatedInto, contestedBy, contestAt sql.NullString
		accessCount                              int
	)

	err := row.Scan(
		&m.ID, &scope, &typ, &m.Content,
		&workspaceID, &personaID, &metadataStr,
		&createdAt, &accessedAt, &accessCount,
		&consolidatedInto, &contestedBy, &contestAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanMemory: %w", err)
	}

	m.Scope = types.MemoryScope(scope)
	m.Type = types.MemoryType(typ)
	m.WorkspaceID = workspaceID.String
	m.PersonaID = personaID.String
	m.AccessCount = accessCount
	m.ConsolidatedInto = consolidatedInto.String
	m.ContestedBy = contestedBy.String
	m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	m.AccessedAt, _ = time.Parse("2006-01-02 15:04:05", accessedAt)
	if contestAt.Valid {
		m.ContestedAt, _ = time.Parse("2006-01-02 15:04:05", contestAt.String)
	}
	if metadataStr.Valid && metadataStr.String != "" {
		if err := json.Unmarshal([]byte(metadataStr.String), &m.Metadata); err != nil {
			slog.Error("failed to unmarshal memory metadata from database", "error", err)
		}
	}

	return m, nil
}

// scanMemoryRow scans a row from a *sql.Rows result set.
func scanMemoryRow(rows *sql.Rows) (*types.Memory, error) {
	m := &types.Memory{}
	var (
		scope, typ, createdAt, accessedAt        string
		workspaceID, personaID, metadataStr      sql.NullString
		consolidatedInto, contestedBy, contestAt sql.NullString
		accessCount                              int
	)

	err := rows.Scan(
		&m.ID, &scope, &typ, &m.Content,
		&workspaceID, &personaID, &metadataStr,
		&createdAt, &accessedAt, &accessCount,
		&consolidatedInto, &contestedBy, &contestAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanMemoryRow: %w", err)
	}

	m.Scope = types.MemoryScope(scope)
	m.Type = types.MemoryType(typ)
	m.WorkspaceID = workspaceID.String
	m.PersonaID = personaID.String
	m.AccessCount = accessCount
	m.ConsolidatedInto = consolidatedInto.String
	m.ContestedBy = contestedBy.String
	m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	m.AccessedAt, _ = time.Parse("2006-01-02 15:04:05", accessedAt)
	if contestAt.Valid {
		m.ContestedAt, _ = time.Parse("2006-01-02 15:04:05", contestAt.String)
	}
	if metadataStr.Valid && metadataStr.String != "" {
		if err := json.Unmarshal([]byte(metadataStr.String), &m.Metadata); err != nil {
			slog.Error("failed to unmarshal memory metadata from database", "error", err)
		}
	}

	return m, nil
}

// nullableString converts an empty string to a sql.NullString with Valid=false.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// ftsQuery converts a natural language query into FTS5 MATCH syntax.
// Words are joined with implicit AND: "foo bar" → "foo AND bar".
func ftsQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return query
	}
	// Escape double quotes in individual terms.
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(words, " AND ")
}
