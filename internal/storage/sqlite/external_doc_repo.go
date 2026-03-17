package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/hyperax/hyperax/internal/repo"
)

// ExternalDocRepoSQLite implements repo.ExternalDocRepo for SQLite.
type ExternalDocRepoSQLite struct {
	db *sql.DB
}

// NewExternalDocRepoSQLite creates a new ExternalDocRepoSQLite.
func NewExternalDocRepoSQLite(db *sql.DB) *ExternalDocRepoSQLite {
	return &ExternalDocRepoSQLite{db: db}
}

// generateSourceID creates a random prefixed ID for external doc sources.
func generateSourceID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sqlite.generateSourceID: %w", err)
	}
	return fmt.Sprintf("eds-%s", hex.EncodeToString(b)), nil
}

// generateTagID creates a random prefixed ID for doc tags.
func generateTagID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sqlite.generateTagID: %w", err)
	}
	return fmt.Sprintf("dt-%s", hex.EncodeToString(b)), nil
}

// AddExternalDocSource inserts a new external documentation source.
func (r *ExternalDocRepoSQLite) AddExternalDocSource(ctx context.Context, source *repo.ExternalDocSource) error {
	if source.ID == "" {
		id, err := generateSourceID()
		if err != nil {
			return fmt.Errorf("sqlite.ExternalDocRepoSQLite.AddExternalDocSource: %w", err)
		}
		source.ID = id
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO external_doc_sources (id, workspace_id, name, path)
		 VALUES (?, ?, ?, ?)`,
		source.ID, source.WorkspaceID, source.Name, source.Path,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.AddExternalDocSource: %w", err)
	}

	return nil
}

// RemoveExternalDocSource deletes an external documentation source by ID.
func (r *ExternalDocRepoSQLite) RemoveExternalDocSource(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM external_doc_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.RemoveExternalDocSource: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.RemoveExternalDocSource: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("external doc source %q not found", id)
	}

	return nil
}

// ListExternalDocSources returns all external doc sources for a workspace.
func (r *ExternalDocRepoSQLite) ListExternalDocSources(ctx context.Context, workspaceID string) ([]*repo.ExternalDocSource, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workspace_id, name, path, created_at
		 FROM external_doc_sources
		 WHERE workspace_id = ?
		 ORDER BY name`, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListExternalDocSources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sources []*repo.ExternalDocSource
	for rows.Next() {
		s := &repo.ExternalDocSource{}
		var createdAt string
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Path, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListExternalDocSources: %w", err)
		}
		var parseErr error
		if s.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.ExternalDocRepoSQLite.ListExternalDocSources"); parseErr != nil {
			return nil, parseErr
		}
		sources = append(sources, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListExternalDocSources: %w", err)
	}
	return sources, nil
}

// GetExternalDocSource retrieves a single external doc source by ID.
func (r *ExternalDocRepoSQLite) GetExternalDocSource(ctx context.Context, id string) (*repo.ExternalDocSource, error) {
	s := &repo.ExternalDocSource{}
	var createdAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, name, path, created_at
		 FROM external_doc_sources
		 WHERE id = ?`, id,
	).Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Path, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("external doc source %q not found", id)
		}
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.GetExternalDocSource: %w", err)
	}

	if s.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.ExternalDocRepoSQLite.GetExternalDocSource"); err != nil {
		return nil, err
	}
	return s, nil
}

// TagDocument creates or replaces a document tag for a workspace.
// Uses INSERT OR REPLACE because UNIQUE(workspace_id, tag) ensures each
// workspace can have at most one architecture doc and one standards doc.
func (r *ExternalDocRepoSQLite) TagDocument(ctx context.Context, tag *repo.DocTag) error {
	if tag.ID == "" {
		id, err := generateTagID()
		if err != nil {
			return fmt.Errorf("sqlite.ExternalDocRepoSQLite.TagDocument: %w", err)
		}
		tag.ID = id
	}
	if tag.SourceType == "" {
		tag.SourceType = "internal"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO doc_tags (id, workspace_id, file_path, tag, source_type)
		 VALUES (?, ?, ?, ?, ?)`,
		tag.ID, tag.WorkspaceID, tag.FilePath, tag.Tag, tag.SourceType,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.TagDocument: %w", err)
	}

	return nil
}

// UntagDocument removes a document tag for a workspace by tag name.
func (r *ExternalDocRepoSQLite) UntagDocument(ctx context.Context, workspaceID, tag string) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM doc_tags WHERE workspace_id = ? AND tag = ?",
		workspaceID, tag,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.UntagDocument: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ExternalDocRepoSQLite.UntagDocument: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("doc tag %q not found for workspace %q", tag, workspaceID)
	}

	return nil
}

// ListDocTags returns all document tags for a workspace.
func (r *ExternalDocRepoSQLite) ListDocTags(ctx context.Context, workspaceID string) ([]*repo.DocTag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workspace_id, file_path, tag, source_type, created_at
		 FROM doc_tags
		 WHERE workspace_id = ?
		 ORDER BY tag`, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListDocTags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []*repo.DocTag
	for rows.Next() {
		t := &repo.DocTag{}
		var createdAt string
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.FilePath, &t.Tag, &t.SourceType, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListDocTags: %w", err)
		}
		var parseErr error
		if t.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.ExternalDocRepoSQLite.ListDocTags"); parseErr != nil {
			return nil, parseErr
		}
		tags = append(tags, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.ListDocTags: %w", err)
	}
	return tags, nil
}

// GetDocTag retrieves a specific document tag by workspace and tag name.
func (r *ExternalDocRepoSQLite) GetDocTag(ctx context.Context, workspaceID, tag string) (*repo.DocTag, error) {
	t := &repo.DocTag{}
	var createdAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, file_path, tag, source_type, created_at
		 FROM doc_tags
		 WHERE workspace_id = ? AND tag = ?`,
		workspaceID, tag,
	).Scan(&t.ID, &t.WorkspaceID, &t.FilePath, &t.Tag, &t.SourceType, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("doc tag %q not found for workspace %q", tag, workspaceID)
		}
		return nil, fmt.Errorf("sqlite.ExternalDocRepoSQLite.GetDocTag: %w", err)
	}

	if t.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.ExternalDocRepoSQLite.GetDocTag"); err != nil {
		return nil, err
	}
	return t, nil
}
