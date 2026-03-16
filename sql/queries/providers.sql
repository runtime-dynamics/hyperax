-- name: CreateProvider :exec
INSERT INTO providers (id, name, kind, base_url, secret_key_ref, is_default, is_enabled, models, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetProvider :one
SELECT id, name, kind, base_url, COALESCE(secret_key_ref, '') AS secret_key_ref,
       is_default, is_enabled, models, metadata, created_at, updated_at
FROM providers WHERE id = ?;

-- name: GetProviderByName :one
SELECT id, name, kind, base_url, COALESCE(secret_key_ref, '') AS secret_key_ref,
       is_default, is_enabled, models, metadata, created_at, updated_at
FROM providers WHERE name = ?;

-- name: GetDefaultProvider :one
SELECT id, name, kind, base_url, COALESCE(secret_key_ref, '') AS secret_key_ref,
       is_default, is_enabled, models, metadata, created_at, updated_at
FROM providers WHERE is_default = 1;

-- name: ListProviders :many
SELECT id, name, kind, base_url, COALESCE(secret_key_ref, '') AS secret_key_ref,
       is_default, is_enabled, models, metadata, created_at, updated_at
FROM providers ORDER BY name;

-- name: UpdateProvider :exec
UPDATE providers SET
    name = ?, kind = ?, base_url = ?, secret_key_ref = ?,
    is_default = ?, is_enabled = ?, models = ?, metadata = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: ClearDefaultProvider :exec
UPDATE providers SET is_default = 0 WHERE is_default = 1;

-- name: SetDefaultProvider :exec
UPDATE providers SET is_default = 1, updated_at = datetime('now') WHERE id = ?;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;
