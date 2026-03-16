-- name: GetConfigValue :one
SELECT value FROM config_values
WHERE key = ? AND scope_type = ? AND scope_id = ?;

-- name: UpsertConfigValue :exec
INSERT INTO config_values (id, key, scope_type, scope_id, value, updated_by)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(key, scope_type, scope_id)
DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = datetime('now');

-- name: GetKeyMeta :one
SELECT key, scope_type, value_type, COALESCE(default_val, '') AS default_val, critical, description
FROM config_keys WHERE key = ?;

-- name: ListKeys :many
SELECT key, scope_type, value_type, COALESCE(default_val, '') AS default_val, critical, description
FROM config_keys ORDER BY key;

-- name: ListValues :many
SELECT key, value, scope_type, scope_id
FROM config_values WHERE scope_type = ? AND scope_id = ? ORDER BY key;

-- name: UpsertKeyMeta :exec
INSERT INTO config_keys (key, scope_type, value_type, default_val, critical, description)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
  scope_type = excluded.scope_type,
  value_type = excluded.value_type,
  default_val = excluded.default_val,
  critical = excluded.critical,
  description = excluded.description;
