package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// PluginRepo implements repo.PluginRepo for SQLite.
type PluginRepo struct {
	db *sql.DB
}

// SavePlugin upserts the runtime state for a plugin.
func (r *PluginRepo) SavePlugin(ctx context.Context, state *types.PluginState) error {
	if state.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if state.ID == "" {
		state.ID = fmt.Sprintf("plg-%d", time.Now().UnixNano())
	}

	var lastHealthAt sql.NullString
	if state.LastHealthAt != nil {
		lastHealthAt = sql.NullString{
			String: state.LastHealthAt.UTC().Format(sqliteTimeFormat),
			Valid:  true,
		}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO plugins (id, name, version, type, status, enabled, tool_count,
			health_status, failure_count, error, loaded_at, last_health_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
			version = excluded.version,
			type = excluded.type,
			status = excluded.status,
			enabled = excluded.enabled,
			tool_count = excluded.tool_count,
			health_status = excluded.health_status,
			failure_count = excluded.failure_count,
			error = excluded.error,
			last_health_at = excluded.last_health_at`,
		state.ID,
		state.Name,
		state.Version,
		string(state.Type),
		state.Status,
		boolToInt(state.Enabled),
		state.ToolCount,
		state.HealthStatus,
		state.FailureCount,
		state.Error,
		state.LoadedAt.UTC().Format(sqliteTimeFormat),
		lastHealthAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite.PluginRepo.SavePlugin: save plugin %q: %w", state.Name, err)
	}
	return nil
}

// GetPlugin retrieves the persisted state for a plugin by name.
func (r *PluginRepo) GetPlugin(ctx context.Context, name string) (*types.PluginState, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, version, type, status, enabled, tool_count,
			health_status, failure_count, error, loaded_at, last_health_at
		 FROM plugins WHERE name = ?`, name)

	state, err := scanPlugin(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite.PluginRepo.GetPlugin: get plugin %q: %w", name, err)
	}
	return state, nil
}

// ListPlugins returns all persisted plugin states.
func (r *PluginRepo) ListPlugins(ctx context.Context) ([]*types.PluginState, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, version, type, status, enabled, tool_count,
			health_status, failure_count, error, loaded_at, last_health_at
		 FROM plugins ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PluginRepo.ListPlugins: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var plugins []*types.PluginState
	for rows.Next() {
		state, err := scanPluginRow(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.PluginRepo.ListPlugins: %w", err)
		}
		plugins = append(plugins, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PluginRepo.ListPlugins: %w", err)
	}
	return plugins, nil
}

// DeletePlugin removes the persisted state for a plugin by name.
func (r *PluginRepo) DeletePlugin(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM plugins WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("sqlite.PluginRepo.DeletePlugin: delete plugin %q: %w", name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.PluginRepo.DeletePlugin: delete plugin %q: %w", name, err)
	}
	if affected == 0 {
		return repo.ErrNotFound
	}
	return nil
}

// scanPlugin scans a single row into a PluginState.
func scanPlugin(row *sql.Row) (*types.PluginState, error) {
	var (
		state        types.PluginState
		pluginType   string
		enabled      int
		loadedAt     string
		lastHealthAt sql.NullString
	)

	err := row.Scan(
		&state.ID,
		&state.Name,
		&state.Version,
		&pluginType,
		&state.Status,
		&enabled,
		&state.ToolCount,
		&state.HealthStatus,
		&state.FailureCount,
		&state.Error,
		&loadedAt,
		&lastHealthAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanPlugin: %w", err)
	}

	state.Type = types.PluginType(pluginType)
	state.Enabled = enabled != 0
	if state.LoadedAt, err = parseSQLiteTime(loadedAt, "sqlite.scanPlugin"); err != nil {
		return nil, err
	}
	if lastHealthAt.Valid {
		t, parseErr := parseSQLiteTime(lastHealthAt.String, "sqlite.scanPlugin.lastHealthAt")
		if parseErr != nil {
			return nil, parseErr
		}
		state.LastHealthAt = &t
	}

	return &state, nil
}

// scanPluginRow scans a *sql.Rows row into a PluginState.
func scanPluginRow(rows *sql.Rows) (*types.PluginState, error) {
	var (
		state        types.PluginState
		pluginType   string
		enabled      int
		loadedAt     string
		lastHealthAt sql.NullString
	)

	err := rows.Scan(
		&state.ID,
		&state.Name,
		&state.Version,
		&pluginType,
		&state.Status,
		&enabled,
		&state.ToolCount,
		&state.HealthStatus,
		&state.FailureCount,
		&state.Error,
		&loadedAt,
		&lastHealthAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanPluginRow: %w", err)
	}

	state.Type = types.PluginType(pluginType)
	state.Enabled = enabled != 0
	if state.LoadedAt, err = parseSQLiteTime(loadedAt, "sqlite.scanPluginRow"); err != nil {
		return nil, err
	}
	if lastHealthAt.Valid {
		t, parseErr := parseSQLiteTime(lastHealthAt.String, "sqlite.scanPluginRow.lastHealthAt")
		if parseErr != nil {
			return nil, parseErr
		}
		state.LastHealthAt = &t
	}

	return &state, nil
}
