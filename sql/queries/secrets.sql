-- name: GetSecret :one
SELECT value FROM secrets WHERE key = ? AND scope = ?;

-- name: UpsertSecret :exec
INSERT INTO secrets (key, value, scope, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(key, scope) DO UPDATE SET
  value = excluded.value,
  updated_at = excluded.updated_at;

-- name: DeleteSecret :exec
DELETE FROM secrets WHERE key = ? AND scope = ?;

-- name: ListSecrets :many
SELECT key FROM secrets WHERE scope = ? ORDER BY key;
