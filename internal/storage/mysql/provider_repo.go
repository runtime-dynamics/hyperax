package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// ProviderRepo implements repo.ProviderRepo for MySQL.
type ProviderRepo struct {
	db *sql.DB
}

// Create inserts a new provider and returns its generated ID.
func (r *ProviderRepo) Create(ctx context.Context, p *repo.Provider) (string, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
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

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO providers (id, name, kind, base_url, secret_key_ref, is_default, is_enabled, models, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Kind, p.BaseURL, secretKeyRef,
		p.IsDefault, p.IsEnabled, models, metadata,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.ProviderRepo.Create: %w", err)
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
		return nil, fmt.Errorf("mysql.ProviderRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var providers []*repo.Provider
	for rows.Next() {
		p := &repo.Provider{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.SecretKeyRef,
			&p.IsDefault, &p.IsEnabled, &p.Models, &p.Metadata,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.ProviderRepo.List: %w", err)
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.ProviderRepo.List: %w", err)
	}
	return providers, nil
}

// Update modifies an existing provider by ID.
func (r *ProviderRepo) Update(ctx context.Context, id string, p *repo.Provider) error {
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
		    is_default = ?, is_enabled = ?, models = ?, metadata = ?
		 WHERE id = ?`,
		p.Name, p.Kind, p.BaseURL, secretKeyRef,
		p.IsDefault, p.IsEnabled, models, metadata, id,
	)
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.Update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.Update: %w", err)
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
		return fmt.Errorf("mysql.ProviderRepo.SetDefault: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, "UPDATE providers SET is_default = 0 WHERE is_default = 1")
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.SetDefault: %w", err)
	}

	res, err := tx.ExecContext(ctx, "UPDATE providers SET is_default = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.SetDefault: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.SetDefault: %w", err)
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
		return fmt.Errorf("mysql.ProviderRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.ProviderRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("provider %q not found", id)
	}
	return nil
}

// scanOne executes a query expected to return a single provider row.
func (r *ProviderRepo) scanOne(ctx context.Context, query string, args ...any) (*repo.Provider, error) {
	p := &repo.Provider{}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.SecretKeyRef,
		&p.IsDefault, &p.IsEnabled, &p.Models, &p.Metadata,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("provider not found")
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.ProviderRepo.scanOne: %w", err)
	}
	return p, nil
}
