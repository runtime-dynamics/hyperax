-- name: CreatePersona :exec
INSERT INTO personas (id, name, description, system_prompt, team, role, home_machine_uuid, clearance_level, is_active, provider_id, default_model)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPersona :one
SELECT id, name, COALESCE(description, '') AS description, COALESCE(system_prompt, '') AS system_prompt,
       COALESCE(team, '') AS team, COALESCE(role, '') AS role, COALESCE(home_machine_uuid, '') AS home_machine_uuid,
       clearance_level, is_active, COALESCE(provider_id, '') AS provider_id, COALESCE(default_model, '') AS default_model,
       created_at, updated_at
FROM personas WHERE id = ?;

-- name: GetPersonaByName :one
SELECT id, name, COALESCE(description, '') AS description, COALESCE(system_prompt, '') AS system_prompt,
       COALESCE(team, '') AS team, COALESCE(role, '') AS role, COALESCE(home_machine_uuid, '') AS home_machine_uuid,
       clearance_level, is_active, COALESCE(provider_id, '') AS provider_id, COALESCE(default_model, '') AS default_model,
       created_at, updated_at
FROM personas WHERE name = ?;

-- name: ListPersonas :many
SELECT id, name, COALESCE(description, '') AS description, COALESCE(system_prompt, '') AS system_prompt,
       COALESCE(team, '') AS team, COALESCE(role, '') AS role, COALESCE(home_machine_uuid, '') AS home_machine_uuid,
       clearance_level, is_active, COALESCE(provider_id, '') AS provider_id, COALESCE(default_model, '') AS default_model,
       created_at, updated_at
FROM personas ORDER BY name;

-- name: UpdatePersona :exec
UPDATE personas SET
    name = ?, description = ?, system_prompt = ?,
    team = ?, role = ?, home_machine_uuid = ?,
    clearance_level = ?, is_active = ?,
    provider_id = ?, default_model = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeletePersona :exec
DELETE FROM personas WHERE id = ?;
