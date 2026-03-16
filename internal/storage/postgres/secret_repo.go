package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hyperax/hyperax/internal/repo"
)

// SecretRepo implements repo.SecretRepo for PostgreSQL.
type SecretRepo struct {
	db *sql.DB
}

// Get retrieves the value of a secret by key and scope.
// Returns an error if the secret does not exist.
func (r *SecretRepo) Get(ctx context.Context, key string, scope string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		"SELECT value FROM secrets WHERE key = $1 AND scope = $2",
		key, scope,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("secret %q in scope %q not found", key, scope)
	}
	if err != nil {
		return "", fmt.Errorf("postgres.SecretRepo.Get: %w", err)
	}
	return value, nil
}

// Set creates or replaces a secret value for the given key and scope.
func (r *SecretRepo) Set(ctx context.Context, key string, value string, scope string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO secrets (key, value, scope, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT(key, scope) DO UPDATE SET
		   value = EXCLUDED.value,
		   updated_at = NOW()`,
		key, value, scope,
	)
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.Set: %w", err)
	}
	return nil
}

// Delete removes a secret by key and scope.
// Returns an error if the secret does not exist.
func (r *SecretRepo) Delete(ctx context.Context, key string, scope string) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM secrets WHERE key = $1 AND scope = $2",
		key, scope,
	)
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("secret %q in scope %q not found", key, scope)
	}
	return nil
}

// List returns all secret keys for the given scope, ordered alphabetically.
func (r *SecretRepo) List(ctx context.Context, scope string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key FROM secrets WHERE scope = $1 ORDER BY key",
		scope,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.SecretRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("postgres.SecretRepo.List: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.SecretRepo.List: %w", err)
	}
	return keys, nil
}

// SetWithAccess creates or replaces a secret value with an access scope restriction.
func (r *SecretRepo) SetWithAccess(ctx context.Context, key string, value string, scope string, accessScope string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO secrets (key, value, scope, access_scope, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT(key, scope) DO UPDATE SET
		   value = EXCLUDED.value,
		   access_scope = EXCLUDED.access_scope,
		   updated_at = NOW()`,
		key, value, scope, accessScope,
	)
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.SetWithAccess: %w", err)
	}
	return nil
}

// ListEntries returns secret metadata (not values) for a scope.
func (r *SecretRepo) ListEntries(ctx context.Context, scope string) ([]repo.SecretEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, scope, access_scope, created_at, updated_at FROM secrets WHERE scope = $1 ORDER BY key`,
		scope,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.SecretRepo.ListEntries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []repo.SecretEntry
	for rows.Next() {
		var e repo.SecretEntry
		if err := rows.Scan(&e.Key, &e.Scope, &e.AccessScope, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres.SecretRepo.ListEntries: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.SecretRepo.ListEntries: %w", err)
	}
	return entries, nil
}

// GetAccessScope returns the access_scope for a secret.
func (r *SecretRepo) GetAccessScope(ctx context.Context, key string, scope string) (string, error) {
	var accessScope string
	err := r.db.QueryRowContext(ctx,
		"SELECT access_scope FROM secrets WHERE key = $1 AND scope = $2",
		key, scope,
	).Scan(&accessScope)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("secret %q in scope %q not found", key, scope)
	}
	if err != nil {
		return "", fmt.Errorf("postgres.SecretRepo.GetAccessScope: %w", err)
	}
	return accessScope, nil
}

// UpdateAccessScope changes the access_scope of an existing secret.
func (r *SecretRepo) UpdateAccessScope(ctx context.Context, key string, scope string, accessScope string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE secrets SET access_scope = $1, updated_at = NOW() WHERE key = $2 AND scope = $3",
		accessScope, key, scope,
	)
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.UpdateAccessScope: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.SecretRepo.UpdateAccessScope: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("secret %q in scope %q not found", key, scope)
	}
	return nil
}
