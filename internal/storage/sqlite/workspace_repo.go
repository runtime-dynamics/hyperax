package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hyperax/hyperax/pkg/types"
)

// WorkspaceRepo implements repo.WorkspaceRepo for SQLite.
type WorkspaceRepo struct {
	db *sql.DB
}

func (r *WorkspaceRepo) WorkspaceExists(ctx context.Context, name string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workspaces WHERE name = ?", name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("sqlite.WorkspaceRepo.WorkspaceExists: %w", err)
	}
	return count > 0, nil
}

func (r *WorkspaceRepo) ListWorkspaces(ctx context.Context) ([]*types.WorkspaceInfo, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, name, root_path, created_at, COALESCE(metadata, '') FROM workspaces ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkspaceRepo.ListWorkspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workspaces []*types.WorkspaceInfo
	for rows.Next() {
		ws := &types.WorkspaceInfo{}
		var createdAt string
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.RootPath, &createdAt, &ws.Metadata); err != nil {
			return nil, fmt.Errorf("sqlite.WorkspaceRepo.ListWorkspaces: %w", err)
		}
		var parseErr error
		if ws.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkspaceRepo.ListWorkspaces"); parseErr != nil {
			return nil, parseErr
		}
		workspaces = append(workspaces, ws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkspaceRepo.ListWorkspaces: %w", err)
	}
	return workspaces, nil
}

func (r *WorkspaceRepo) GetWorkspace(ctx context.Context, name string) (*types.WorkspaceInfo, error) {
	ws := &types.WorkspaceInfo{}
	var createdAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, root_path, created_at, COALESCE(metadata, '') FROM workspaces WHERE name = ?",
		name,
	).Scan(&ws.ID, &ws.Name, &ws.RootPath, &createdAt, &ws.Metadata)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkspaceRepo.GetWorkspace: %w", err)
	}
	if ws.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.WorkspaceRepo.GetWorkspace"); err != nil {
		return nil, err
	}
	return ws, nil
}

func (r *WorkspaceRepo) CreateWorkspace(ctx context.Context, ws *types.WorkspaceInfo) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workspaces (id, name, root_path, metadata) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET id = excluded.id, root_path = excluded.root_path, metadata = excluded.metadata`,
		ws.ID, ws.Name, ws.RootPath, ws.Metadata,
	)
	if err != nil {
		return fmt.Errorf("sqlite.WorkspaceRepo.CreateWorkspace: %w", err)
	}
	return nil
}

// DeleteWorkspace removes a workspace by name. Returns an error if the
// workspace does not exist.
func (r *WorkspaceRepo) DeleteWorkspace(ctx context.Context, name string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workspaces WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("sqlite.WorkspaceRepo.DeleteWorkspace: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkspaceRepo.DeleteWorkspace: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("workspace %q not found", name)
	}
	return nil
}
