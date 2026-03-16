-- name: WorkspaceExists :one
SELECT COUNT(*) FROM workspaces WHERE name = ?;

-- name: ListWorkspaces :many
SELECT id, name, root_path, created_at, COALESCE(metadata, '') AS metadata
FROM workspaces ORDER BY name;

-- name: GetWorkspace :one
SELECT id, name, root_path, created_at, COALESCE(metadata, '') AS metadata
FROM workspaces WHERE name = ?;

-- name: CreateWorkspace :exec
INSERT INTO workspaces (id, name, root_path, metadata) VALUES (?, ?, ?, ?);

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE name = ?;
