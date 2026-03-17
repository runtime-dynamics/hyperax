package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// DelegationRepo implements repo.DelegationRepo for MySQL.
type DelegationRepo struct {
	db *sql.DB
}

// Create stores a new delegation grant.
func (r *DelegationRepo) Create(ctx context.Context, d *types.Delegation) error {
	scopesJSON, err := json.Marshal(d.Scopes)
	if err != nil {
		return fmt.Errorf("mysql.DelegationRepo.Create: %w", err)
	}

	var expiresAt sql.NullString
	if d.ExpiresAt != "" {
		expiresAt = sql.NullString{String: d.ExpiresAt, Valid: true}
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO delegations (id, granter_id, grantee_id, grant_type, credential_key, elevated_level, scopes, expires_at, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())`,
		d.ID, d.GranterID, d.GranteeID, string(d.GrantType),
		d.CredentialKey, d.ElevatedLevel, string(scopesJSON),
		expiresAt, d.Reason,
	)
	if err != nil {
		return fmt.Errorf("mysql.DelegationRepo.Create: %w", err)
	}
	return nil
}

// GetByID retrieves a delegation by its ID.
func (r *DelegationRepo) GetByID(ctx context.Context, id string) (*types.Delegation, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, granter_id, grantee_id, grant_type, credential_key, elevated_level, scopes, expires_at, reason, created_at, revoked_at
		 FROM delegations WHERE id = ?`, id)

	d, err := scanMyDelegation(row)
	if err == sql.ErrNoRows {
		return nil, repo.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.DelegationRepo.GetByID: %w", err)
	}
	return d, nil
}

// ListByGrantee returns all active (non-revoked, non-expired) delegations for a grantee.
func (r *DelegationRepo) ListByGrantee(ctx context.Context, granteeID string) ([]*types.Delegation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, granter_id, grantee_id, grant_type, credential_key, elevated_level, scopes, expires_at, reason, created_at, revoked_at
		 FROM delegations
		 WHERE grantee_id = ? AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY created_at DESC`, granteeID)
	if err != nil {
		return nil, fmt.Errorf("mysql.DelegationRepo.ListByGrantee: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyDelegations(rows)
}

// ListByGranter returns all delegations created by a granter.
func (r *DelegationRepo) ListByGranter(ctx context.Context, granterID string) ([]*types.Delegation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, granter_id, grantee_id, grant_type, credential_key, elevated_level, scopes, expires_at, reason, created_at, revoked_at
		 FROM delegations
		 WHERE granter_id = ?
		 ORDER BY created_at DESC`, granterID)
	if err != nil {
		return nil, fmt.Errorf("mysql.DelegationRepo.ListByGranter: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyDelegations(rows)
}

// ListAll returns all delegations across all personas.
func (r *DelegationRepo) ListAll(ctx context.Context) ([]*types.Delegation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, granter_id, grantee_id, grant_type, credential_key, elevated_level, scopes, expires_at, reason, created_at, revoked_at
		 FROM delegations
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("mysql.DelegationRepo.ListAll: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyDelegations(rows)
}

// Revoke marks a delegation as revoked by setting revoked_at.
func (r *DelegationRepo) Revoke(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE delegations SET revoked_at = NOW() WHERE id = ? AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("mysql.DelegationRepo.Revoke: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.DelegationRepo.Revoke: %w", err)
	}
	if affected == 0 {
		return repo.ErrNotFound
	}
	return nil
}

// CleanupExpired revokes all delegations that have passed their expires_at.
func (r *DelegationRepo) CleanupExpired(ctx context.Context) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE delegations SET revoked_at = NOW()
		 WHERE revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("mysql.DelegationRepo.CleanupExpired: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql.DelegationRepo.CleanupExpired: %w", err)
	}
	return int(affected), nil
}

// scanMyDelegation scans a single delegation row.
func scanMyDelegation(row *sql.Row) (*types.Delegation, error) {
	var d types.Delegation
	var credKey, scopesJSON sql.NullString
	var expiresAt, revokedAt sql.NullString

	err := row.Scan(
		&d.ID, &d.GranterID, &d.GranteeID, &d.GrantType,
		&credKey, &d.ElevatedLevel, &scopesJSON,
		&expiresAt, &d.Reason, &d.CreatedAt, &revokedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.scanMyDelegation: %w", err)
	}

	if credKey.Valid {
		d.CredentialKey = credKey.String
	}
	if scopesJSON.Valid && scopesJSON.String != "" {
		if err := json.Unmarshal([]byte(scopesJSON.String), &d.Scopes); err != nil {
			return nil, fmt.Errorf("mysql.scanMyDelegation: unmarshal scopes: %w", err)
		}
	}
	if expiresAt.Valid {
		d.ExpiresAt = expiresAt.String
	}
	if revokedAt.Valid {
		d.RevokedAt = revokedAt.String
	}
	return &d, nil
}

// scanMyDelegations scans multiple delegation rows.
func scanMyDelegations(rows *sql.Rows) ([]*types.Delegation, error) {
	var results []*types.Delegation
	for rows.Next() {
		var d types.Delegation
		var credKey, scopesJSON sql.NullString
		var expiresAt, revokedAt sql.NullString

		err := rows.Scan(
			&d.ID, &d.GranterID, &d.GranteeID, &d.GrantType,
			&credKey, &d.ElevatedLevel, &scopesJSON,
			&expiresAt, &d.Reason, &d.CreatedAt, &revokedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("mysql.scanMyDelegations: %w", err)
		}

		if credKey.Valid {
			d.CredentialKey = credKey.String
		}
		if scopesJSON.Valid && scopesJSON.String != "" {
			if err := json.Unmarshal([]byte(scopesJSON.String), &d.Scopes); err != nil {
				return nil, fmt.Errorf("mysql.scanMyDelegations: unmarshal scopes: %w", err)
			}
		}
		if expiresAt.Valid {
			d.ExpiresAt = expiresAt.String
		}
		if revokedAt.Valid {
			d.RevokedAt = revokedAt.String
		}
		results = append(results, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.scanMyDelegations: %w", err)
	}
	return results, nil
}
