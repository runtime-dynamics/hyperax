package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// MemoryRepo implements repo.MemoryRepo for PostgreSQL.
// Uses ILIKE for text search (no FTS5). PostgreSQL full-text search via
// tsvector can be added later for improved relevance ranking.
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
			return "", fmt.Errorf("postgres.MemoryRepo.Store: %w", err)
		}
		metadata = string(b)
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memories (id, scope, type, content, workspace_id, persona_id, metadata, created_at, accessed_at, access_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), 0)`,
		memory.ID, string(memory.Scope), string(memory.Type), memory.Content,
		pgNullableString(memory.WorkspaceID), pgNullableString(memory.PersonaID),
		metadata,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.MemoryRepo.Store: %w", err)
	}

	return memory.ID, nil
}

// Get retrieves a single memory by ID.
func (r *MemoryRepo) Get(ctx context.Context, id string) (*types.Memory, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories WHERE id = $1`, id,
	)

	m, err := scanPgMemory(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.Get: %w", err)
	}
	return m, nil
}

// Delete removes a memory by ID.
func (r *MemoryRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM memories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.MemoryRepo.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.MemoryRepo.Delete: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("memory %q not found", id)
	}
	return nil
}

// Recall searches memories using ILIKE. Results are filtered by scope,
// workspaceID, and personaID. Contested and consolidated memories are excluded.
func (r *MemoryRepo) Recall(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	if query == "" {
		return nil, nil
	}

	var where []string
	var args []any
	paramIdx := 1

	where = append(where, fmt.Sprintf("content ILIKE $%d", paramIdx))
	args = append(args, "%"+query+"%")
	paramIdx++

	where = append(where, "consolidated_into IS NULL")
	where = append(where, "contested_by IS NULL")

	if scope != "" {
		where = append(where, fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, string(scope))
		paramIdx++
	}
	if workspaceID != "" {
		where = append(where, fmt.Sprintf("(workspace_id = $%d OR workspace_id IS NULL)", paramIdx))
		args = append(args, workspaceID)
		paramIdx++
	}
	if personaID != "" {
		where = append(where, fmt.Sprintf("(persona_id = $%d OR persona_id IS NULL)", paramIdx))
		args = append(args, personaID)
		paramIdx++
	}

	args = append(args, limit)

	q := fmt.Sprintf(
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories
		 WHERE %s
		 ORDER BY accessed_at DESC
		 LIMIT $%d`,
		strings.Join(where, " AND "), paramIdx,
	)

	return r.queryMemories(ctx, q, args...)
}

// TouchAccess updates accessed_at and increments access_count.
func (r *MemoryRepo) TouchAccess(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE memories SET accessed_at = NOW(), access_count = access_count + 1 WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres.MemoryRepo.TouchAccess: %w", err)
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
	paramIdx := 1

	where = append(where, "type = 'episodic'")
	where = append(where, "consolidated_into IS NULL")
	where = append(where, "contested_by IS NULL")
	where = append(where, fmt.Sprintf("accessed_at < NOW() - ($%d || ' days')::INTERVAL", paramIdx))
	args = append(args, fmt.Sprintf("%d", olderThanDays))
	paramIdx++

	if scope != "" {
		where = append(where, fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, string(scope))
		paramIdx++
	}

	// Exclude anchored memories.
	where = append(where, "(metadata->>'anchored' IS NULL OR metadata->>'anchored' = '0' OR metadata->>'anchored' = 'false')")

	args = append(args, limit)

	q := fmt.Sprintf(
		`SELECT id, scope, type, content, workspace_id, persona_id, metadata,
		        created_at, accessed_at, access_count, consolidated_into, contested_by, contested_at
		 FROM memories
		 WHERE %s
		 ORDER BY accessed_at ASC
		 LIMIT $%d`,
		strings.Join(where, " AND "), paramIdx,
	)

	return r.queryMemories(ctx, q, args...)
}

// MarkConsolidated sets consolidated_into on the given memory IDs.
func (r *MemoryRepo) MarkConsolidated(ctx context.Context, ids []string, targetID string) error {
	if len(ids) == 0 {
		return nil
	}

	args := make([]any, 0, len(ids)+1)
	args = append(args, targetID)
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}

	q := fmt.Sprintf(
		`UPDATE memories SET consolidated_into = $1 WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	_, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("postgres.MemoryRepo.MarkConsolidated: %w", err)
	}
	return nil
}

// MarkContested flags a memory as contested by another memory.
func (r *MemoryRepo) MarkContested(ctx context.Context, id, contestedByID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE memories SET contested_by = $1, contested_at = NOW() WHERE id = $2`,
		contestedByID, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.MemoryRepo.MarkContested: %w", err)
	}
	return nil
}

// Count returns the number of active memories for the given scope and workspace.
func (r *MemoryRepo) Count(ctx context.Context, scope types.MemoryScope, workspaceID string) (int, error) {
	var where []string
	var args []any
	paramIdx := 1

	where = append(where, "consolidated_into IS NULL")

	if scope != "" {
		where = append(where, fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, string(scope))
		paramIdx++
	}
	if workspaceID != "" {
		where = append(where, fmt.Sprintf("workspace_id = $%d", paramIdx))
		args = append(args, workspaceID)
	}

	q := fmt.Sprintf("SELECT COUNT(*) FROM memories WHERE %s", strings.Join(where, " AND "))

	var count int
	err := r.db.QueryRowContext(ctx, q, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres.MemoryRepo.Count: %w", err)
	}
	return count, nil
}

// CountByType returns memory counts grouped by type.
func (r *MemoryRepo) CountByType(ctx context.Context, scope types.MemoryScope, workspaceID string) (map[types.MemoryType]int, error) {
	var where []string
	var args []any
	paramIdx := 1

	where = append(where, "consolidated_into IS NULL")

	if scope != "" {
		where = append(where, fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, string(scope))
		paramIdx++
	}
	if workspaceID != "" {
		where = append(where, fmt.Sprintf("workspace_id = $%d", paramIdx))
		args = append(args, workspaceID)
	}

	q := fmt.Sprintf(
		"SELECT type, COUNT(*) FROM memories WHERE %s GROUP BY type",
		strings.Join(where, " AND "),
	)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.CountByType: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[types.MemoryType]int)
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, fmt.Errorf("postgres.MemoryRepo.CountByType: %w", err)
		}
		result[types.MemoryType(typ)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.CountByType: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5)`,
		ann.ID, ann.MemoryID, ann.Annotation, ann.AnnotationType, ann.CreatedBy,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.MemoryRepo.StoreAnnotation: %w", err)
	}
	return ann.ID, nil
}

// GetAnnotations retrieves all annotations for a given memory.
func (r *MemoryRepo) GetAnnotations(ctx context.Context, memoryID string) ([]*types.MemoryAnnotation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, memory_id, annotation, annotation_type, created_by, created_at
		 FROM memory_annotations WHERE memory_id = $1 ORDER BY created_at ASC`,
		memoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.GetAnnotations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var anns []*types.MemoryAnnotation
	for rows.Next() {
		a := &types.MemoryAnnotation{}
		if err := rows.Scan(&a.ID, &a.MemoryID, &a.Annotation, &a.AnnotationType, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres.MemoryRepo.GetAnnotations: %w", err)
		}
		anns = append(anns, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.GetAnnotations: %w", err)
	}
	return anns, nil
}

// --- helpers ---

// queryMemories executes a query and scans the results into Memory slices.
func (r *MemoryRepo) queryMemories(ctx context.Context, query string, args ...any) ([]*types.Memory, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.queryMemories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var memories []*types.Memory
	for rows.Next() {
		m, err := scanPgMemoryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres.MemoryRepo.queryMemories: %w", err)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.MemoryRepo.queryMemories: %w", err)
	}
	return memories, nil
}

// scanPgMemory scans a single row (sql.Row) into a Memory.
func scanPgMemory(row *sql.Row) (*types.Memory, error) {
	m := &types.Memory{}
	var (
		scope, typ                               string
		workspaceID, personaID, metadataStr      sql.NullString
		consolidatedInto, contestedBy            sql.NullString
		contestAt                                sql.NullTime
		accessCount                              int
	)

	err := row.Scan(
		&m.ID, &scope, &typ, &m.Content,
		&workspaceID, &personaID, &metadataStr,
		&m.CreatedAt, &m.AccessedAt, &accessCount,
		&consolidatedInto, &contestedBy, &contestAt,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.scanPgMemory: %w", err)
	}

	m.Scope = types.MemoryScope(scope)
	m.Type = types.MemoryType(typ)
	m.WorkspaceID = workspaceID.String
	m.PersonaID = personaID.String
	m.AccessCount = accessCount
	m.ConsolidatedInto = consolidatedInto.String
	m.ContestedBy = contestedBy.String
	if contestAt.Valid {
		m.ContestedAt = contestAt.Time
	}
	if metadataStr.Valid && metadataStr.String != "" {
		if err := json.Unmarshal([]byte(metadataStr.String), &m.Metadata); err != nil {
			return nil, fmt.Errorf("postgres.MemoryRepo: unmarshal metadata: %w", err)
		}
	}

	return m, nil
}

// scanPgMemoryRow scans a row from a *sql.Rows result set.
func scanPgMemoryRow(rows *sql.Rows) (*types.Memory, error) {
	m := &types.Memory{}
	var (
		scope, typ                               string
		workspaceID, personaID, metadataStr      sql.NullString
		consolidatedInto, contestedBy            sql.NullString
		contestAt                                sql.NullTime
		accessCount                              int
	)

	err := rows.Scan(
		&m.ID, &scope, &typ, &m.Content,
		&workspaceID, &personaID, &metadataStr,
		&m.CreatedAt, &m.AccessedAt, &accessCount,
		&consolidatedInto, &contestedBy, &contestAt,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.scanPgMemoryRow: %w", err)
	}

	m.Scope = types.MemoryScope(scope)
	m.Type = types.MemoryType(typ)
	m.WorkspaceID = workspaceID.String
	m.PersonaID = personaID.String
	m.AccessCount = accessCount
	m.ConsolidatedInto = consolidatedInto.String
	m.ContestedBy = contestedBy.String
	if contestAt.Valid {
		m.ContestedAt = contestAt.Time
	}
	if metadataStr.Valid && metadataStr.String != "" {
		if err := json.Unmarshal([]byte(metadataStr.String), &m.Metadata); err != nil {
			return nil, fmt.Errorf("postgres.MemoryRepo: unmarshal metadata: %w", err)
		}
	}

	return m, nil
}

// pgNullableString converts an empty string to a sql.NullString with Valid=false.
func pgNullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
