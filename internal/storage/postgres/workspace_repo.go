package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hyperax/hyperax/pkg/types"
)

// WorkspaceRepo implements repo.WorkspaceRepo for PostgreSQL.
type WorkspaceRepo struct {
	db *sql.DB
}

// WorkspaceExists returns true if a workspace with the given name exists.
func (r *WorkspaceRepo) WorkspaceExists(ctx context.Context, name string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workspaces WHERE name = $1", name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("postgres.WorkspaceRepo.WorkspaceExists: %w", err)
	}
	return count > 0, nil
}

// ListWorkspaces returns all workspaces ordered by name.
func (r *WorkspaceRepo) ListWorkspaces(ctx context.Context) ([]*types.WorkspaceInfo, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, name, root_path, created_at, COALESCE(metadata, '') FROM workspaces ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.WorkspaceRepo.ListWorkspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workspaces []*types.WorkspaceInfo
	for rows.Next() {
		ws := &types.WorkspaceInfo{}
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.RootPath, &ws.CreatedAt, &ws.Metadata); err != nil {
			return nil, fmt.Errorf("postgres.WorkspaceRepo.ListWorkspaces: %w", err)
		}
		workspaces = append(workspaces, ws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.WorkspaceRepo.ListWorkspaces: %w", err)
	}
	return workspaces, nil
}

// GetWorkspace retrieves a workspace by name.
func (r *WorkspaceRepo) GetWorkspace(ctx context.Context, name string) (*types.WorkspaceInfo, error) {
	ws := &types.WorkspaceInfo{}
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, root_path, created_at, COALESCE(metadata, '') FROM workspaces WHERE name = $1",
		name,
	).Scan(&ws.ID, &ws.Name, &ws.RootPath, &ws.CreatedAt, &ws.Metadata)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.WorkspaceRepo.GetWorkspace: %w", err)
	}
	return ws, nil
}

// CreateWorkspace inserts or updates a workspace record. On name conflict the
// ID, root_path, and metadata are updated to support rescans with new IDs.
func (r *WorkspaceRepo) CreateWorkspace(ctx context.Context, ws *types.WorkspaceInfo) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workspaces (id, name, root_path, metadata) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (name) DO UPDATE SET id = EXCLUDED.id, root_path = EXCLUDED.root_path, metadata = EXCLUDED.metadata`,
		ws.ID, ws.Name, ws.RootPath, ws.Metadata,
	)
	if err != nil {
		return fmt.Errorf("postgres.WorkspaceRepo.CreateWorkspace: %w", err)
	}
	return nil
}

// DeleteWorkspace removes a workspace by name.
// Returns an error if the workspace does not exist.
func (r *WorkspaceRepo) DeleteWorkspace(ctx context.Context, name string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workspaces WHERE name = $1", name)
	if err != nil {
		return fmt.Errorf("postgres.WorkspaceRepo.DeleteWorkspace: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.WorkspaceRepo.DeleteWorkspace: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("workspace %q not found", name)
	}
	return nil
}
