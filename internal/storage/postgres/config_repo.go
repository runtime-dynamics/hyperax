package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigRepo implements repo.ConfigRepo for PostgreSQL.
type ConfigRepo struct {
	db *sql.DB
}

// GetValue retrieves a config value by key and scope.
func (r *ConfigRepo) GetValue(ctx context.Context, key string, scope types.ConfigScope) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		"SELECT value FROM config_values WHERE key = $1 AND scope_type = $2 AND scope_id = $3",
		key, scope.Type, scope.ID,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("config key %q not found in scope %s/%s", key, scope.Type, scope.ID)
	}
	if err != nil {
		return "", fmt.Errorf("postgres.ConfigRepo.GetValue: %w", err)
	}
	return value, nil
}

// SetValue creates or updates a config value for the given key and scope.
func (r *ConfigRepo) SetValue(ctx context.Context, key, value string, scope types.ConfigScope, actor string) error {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO config_values (id, key, scope_type, scope_id, value, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT(key, scope_type, scope_id)
		 DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		id, key, scope.Type, scope.ID, value, actor,
	)
	if err != nil {
		return fmt.Errorf("postgres.ConfigRepo.SetValue: %w", err)
	}
	return nil
}

// GetKeyMeta retrieves metadata for a configuration key.
func (r *ConfigRepo) GetKeyMeta(ctx context.Context, key string) (*types.ConfigKeyMeta, error) {
	meta := &types.ConfigKeyMeta{}
	err := r.db.QueryRowContext(ctx,
		"SELECT key, scope_type, value_type, COALESCE(default_val, ''), critical, description FROM config_keys WHERE key = $1",
		key,
	).Scan(&meta.Key, &meta.ScopeType, &meta.ValueType, &meta.DefaultVal, &meta.Critical, &meta.Description)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("config key %q not found", key)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.ConfigRepo.GetKeyMeta: %w", err)
	}
	return meta, nil
}

// ListKeys returns all registered configuration key metadata ordered by key.
func (r *ConfigRepo) ListKeys(ctx context.Context) ([]types.ConfigKeyMeta, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key, scope_type, value_type, COALESCE(default_val, ''), critical, description FROM config_keys ORDER BY key",
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ConfigRepo.ListKeys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []types.ConfigKeyMeta
	for rows.Next() {
		var meta types.ConfigKeyMeta
		if err := rows.Scan(&meta.Key, &meta.ScopeType, &meta.ValueType, &meta.DefaultVal, &meta.Critical, &meta.Description); err != nil {
			return nil, fmt.Errorf("postgres.ConfigRepo.ListKeys: %w", err)
		}
		keys = append(keys, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ConfigRepo.ListKeys: %w", err)
	}
	return keys, nil
}

// ListValues returns all config values for the given scope, ordered by key.
func (r *ConfigRepo) ListValues(ctx context.Context, scope types.ConfigScope) ([]types.ConfigValue, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key, value, scope_type, scope_id FROM config_values WHERE scope_type = $1 AND scope_id = $2 ORDER BY key",
		scope.Type, scope.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ConfigRepo.ListValues: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var values []types.ConfigValue
	for rows.Next() {
		var v types.ConfigValue
		if err := rows.Scan(&v.Key, &v.Value, &v.ScopeType, &v.ScopeID); err != nil {
			return nil, fmt.Errorf("postgres.ConfigRepo.ListValues: %w", err)
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ConfigRepo.ListValues: %w", err)
	}
	return values, nil
}

// GetHistory returns config change history for a key.
// Not yet implemented for PostgreSQL — returns empty.
func (r *ConfigRepo) GetHistory(ctx context.Context, key string, limit int) ([]types.ConfigChange, error) {
	return nil, nil
}

// UpsertKeyMeta creates or updates configuration key metadata.
func (r *ConfigRepo) UpsertKeyMeta(ctx context.Context, meta *types.ConfigKeyMeta) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO config_keys (key, scope_type, value_type, default_val, critical, description)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT(key) DO UPDATE SET
		   scope_type = EXCLUDED.scope_type,
		   value_type = EXCLUDED.value_type,
		   default_val = EXCLUDED.default_val,
		   critical = EXCLUDED.critical,
		   description = EXCLUDED.description`,
		meta.Key, meta.ScopeType, meta.ValueType, meta.DefaultVal, meta.Critical, meta.Description,
	)
	if err != nil {
		return fmt.Errorf("postgres.ConfigRepo.UpsertKeyMeta: %w", err)
	}
	return nil
}
