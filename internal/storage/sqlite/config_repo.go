package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigRepo implements repo.ConfigRepo for SQLite.
type ConfigRepo struct {
	db *sql.DB
}

func (r *ConfigRepo) GetValue(ctx context.Context, key string, scope types.ConfigScope) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		"SELECT value FROM config_values WHERE key = ? AND scope_type = ? AND scope_id = ?",
		key, scope.Type, scope.ID,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("config key %q not found in scope %s/%s", key, scope.Type, scope.ID)
	}
	if err != nil {
		return "", fmt.Errorf("sqlite.ConfigRepo.GetValue: %w", err)
	}
	return value, nil
}

func (r *ConfigRepo) SetValue(ctx context.Context, key, value string, scope types.ConfigScope, actor string) error {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO config_values (id, key, scope_type, scope_id, value, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key, scope_type, scope_id)
		 DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = datetime('now')`,
		id, key, scope.Type, scope.ID, value, actor,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ConfigRepo.SetValue: %w", err)
	}
	return nil
}

func (r *ConfigRepo) GetKeyMeta(ctx context.Context, key string) (*types.ConfigKeyMeta, error) {
	meta := &types.ConfigKeyMeta{}
	var critical int
	err := r.db.QueryRowContext(ctx,
		"SELECT key, scope_type, value_type, COALESCE(default_val, ''), critical, description FROM config_keys WHERE key = ?",
		key,
	).Scan(&meta.Key, &meta.ScopeType, &meta.ValueType, &meta.DefaultVal, &critical, &meta.Description)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("config key %q not found", key)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.ConfigRepo.GetKeyMeta: %w", err)
	}
	meta.Critical = critical == 1
	return meta, nil
}

func (r *ConfigRepo) ListKeys(ctx context.Context) ([]types.ConfigKeyMeta, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key, scope_type, value_type, COALESCE(default_val, ''), critical, description FROM config_keys ORDER BY key",
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ConfigRepo.ListKeys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []types.ConfigKeyMeta
	for rows.Next() {
		var meta types.ConfigKeyMeta
		var critical int
		if err := rows.Scan(&meta.Key, &meta.ScopeType, &meta.ValueType, &meta.DefaultVal, &critical, &meta.Description); err != nil {
			return nil, fmt.Errorf("sqlite.ConfigRepo.ListKeys: %w", err)
		}
		meta.Critical = critical == 1
		keys = append(keys, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ConfigRepo.ListKeys: %w", err)
	}
	return keys, nil
}

func (r *ConfigRepo) ListValues(ctx context.Context, scope types.ConfigScope) ([]types.ConfigValue, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key, value, scope_type, scope_id FROM config_values WHERE scope_type = ? AND scope_id = ? ORDER BY key",
		scope.Type, scope.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ConfigRepo.ListValues: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var values []types.ConfigValue
	for rows.Next() {
		var v types.ConfigValue
		if err := rows.Scan(&v.Key, &v.Value, &v.ScopeType, &v.ScopeID); err != nil {
			return nil, fmt.Errorf("sqlite.ConfigRepo.ListValues: %w", err)
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ConfigRepo.ListValues: %w", err)
	}
	return values, nil
}

func (r *ConfigRepo) GetHistory(ctx context.Context, key string, limit int) ([]types.ConfigChange, error) {
	// For Phase 1, config history is not tracked — return empty
	return nil, nil
}

func (r *ConfigRepo) UpsertKeyMeta(ctx context.Context, meta *types.ConfigKeyMeta) error {
	critical := 0
	if meta.Critical {
		critical = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO config_keys (key, scope_type, value_type, default_val, critical, description)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   scope_type = excluded.scope_type,
		   value_type = excluded.value_type,
		   default_val = excluded.default_val,
		   critical = excluded.critical,
		   description = excluded.description`,
		meta.Key, meta.ScopeType, meta.ValueType, meta.DefaultVal, critical, meta.Description,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ConfigRepo.UpsertKeyMeta: %w", err)
	}
	return nil
}
