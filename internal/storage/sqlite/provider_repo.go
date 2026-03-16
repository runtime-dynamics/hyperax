package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// ProviderRepo implements repo.ProviderRepo for SQLite.
type ProviderRepo struct {
	db *sql.DB
}

// Create inserts a new provider and returns its generated ID.
func (r *ProviderRepo) Create(ctx context.Context, p *repo.Provider) (string, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}

	isDefault := 0
	if p.IsDefault {
		isDefault = 1
	}
	isEnabled := 0
	if p.IsEnabled {
		isEnabled = 1
	}

	// Use NULL for empty secret_key_ref to keep keyless providers clean.
	var secretKeyRef *string
	if p.SecretKeyRef != "" {
		secretKeyRef = &p.SecretKeyRef
	}

	models := p.Models
	if models == "" {
		models = "[]"
	}
	metadata := p.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO providers (id, name, kind, base_url, secret_key_ref, is_default, is_enabled, models, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Kind, p.BaseURL, secretKeyRef,
		isDefault, isEnabled, models, metadata,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.ProviderRepo.Create: %w", err)
	}

	return p.ID, nil
}

// Get retrieves a provider by its ID.
func (r *ProviderRepo) Get(ctx context.Context, id string) (*repo.Provider, error) {
	return r.scanOne(ctx,
		`SELECT id, name, kind, base_url, COALESCE(secret_key_ref, ''),
		        is_default, is_enabled, models, metadata, created_at, updated_at
		 FROM providers WHERE id = ?`, id,
	)
}

// GetByName retrieves a provider by its unique name.
func (r *ProviderRepo) GetByName(ctx context.Context, name string) (*repo.Provider, error) {
	return r.scanOne(ctx,
		`SELECT id, name, kind, base_url, COALESCE(secret_key_ref, ''),
		        is_default, is_enabled, models, metadata, created_at, updated_at
		 FROM providers WHERE name = ?`, name,
	)
}

// GetDefault retrieves the provider marked as the global default.
func (r *ProviderRepo) GetDefault(ctx context.Context) (*repo.Provider, error) {
	return r.scanOne(ctx,
		`SELECT id, name, kind, base_url, COALESCE(secret_key_ref, ''),
		        is_default, is_enabled, models, metadata, created_at, updated_at
		 FROM providers WHERE is_default = 1`,
	)
}

// List returns all providers ordered by name.
func (r *ProviderRepo) List(ctx context.Context) ([]*repo.Provider, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, kind, base_url, COALESCE(secret_key_ref, ''),
		        is_default, is_enabled, models, metadata, created_at, updated_at
		 FROM providers ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ProviderRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var providers []*repo.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.ProviderRepo.List: %w", err)
		}
		providers = append(providers, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ProviderRepo.List: %w", err)
	}
	return providers, nil
}

// Update modifies an existing provider by ID.
func (r *ProviderRepo) Update(ctx context.Context, id string, p *repo.Provider) error {
	isDefault := 0
	if p.IsDefault {
		isDefault = 1
	}
	isEnabled := 0
	if p.IsEnabled {
		isEnabled = 1
	}

	var secretKeyRef *string
	if p.SecretKeyRef != "" {
		secretKeyRef = &p.SecretKeyRef
	}

	models := p.Models
	if models == "" {
		models = "[]"
	}
	metadata := p.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE providers SET
		    name = ?, kind = ?, base_url = ?, secret_key_ref = ?,
		    is_default = ?, is_enabled = ?, models = ?, metadata = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		p.Name, p.Kind, p.BaseURL, secretKeyRef,
		isDefault, isEnabled, models, metadata, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.Update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.Update: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("provider %q not found", id)
	}

	return nil
}

// SetDefault marks a provider as the global default, clearing any existing default.
// This operation runs in a transaction to ensure exactly one default at a time.
func (r *ProviderRepo) SetDefault(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.SetDefault: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear any existing default.
	_, err = tx.ExecContext(ctx, `UPDATE providers SET is_default = 0 WHERE is_default = 1`)
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.SetDefault: %w", err)
	}

	// Set the new default.
	res, err := tx.ExecContext(ctx, `UPDATE providers SET is_default = 1, updated_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.SetDefault: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.SetDefault: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("provider %q not found", id)
	}

	return tx.Commit()
}

// Delete removes a provider by its ID.
func (r *ProviderRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM providers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ProviderRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("provider %q not found", id)
	}

	return nil
}

// scanOne executes a query expected to return a single provider row.
func (r *ProviderRepo) scanOne(ctx context.Context, query string, args ...any) (*repo.Provider, error) {
	p := &repo.Provider{}
	var isDefault, isEnabled int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.SecretKeyRef,
		&isDefault, &isEnabled, &p.Models, &p.Metadata, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("provider not found")
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.ProviderRepo.scanOne: %w", err)
	}

	p.IsDefault = isDefault == 1
	p.IsEnabled = isEnabled == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return p, nil
}

// scanner is an interface satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanProvider scans a provider from a row scanner (used in List).
func scanProvider(row scanner) (*repo.Provider, error) {
	p := &repo.Provider{}
	var isDefault, isEnabled int
	var createdAt, updatedAt string

	err := row.Scan(
		&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.SecretKeyRef,
		&isDefault, &isEnabled, &p.Models, &p.Metadata, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanProvider: %w", err)
	}

	p.IsDefault = isDefault == 1
	p.IsEnabled = isEnabled == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return p, nil
}
