package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// DelegationRepo handles on-behalf-of delegation persistence.
type DelegationRepo interface {
	// Create stores a new delegation grant.
	Create(ctx context.Context, d *types.Delegation) error

	// GetByID retrieves a delegation by its ID.
	// Returns ErrNotFound if the delegation does not exist.
	GetByID(ctx context.Context, id string) (*types.Delegation, error)

	// ListByGrantee returns all active (non-revoked, non-expired) delegations for a grantee.
	ListByGrantee(ctx context.Context, granteeID string) ([]*types.Delegation, error)

	// ListByGranter returns all delegations created by a granter.
	ListByGranter(ctx context.Context, granterID string) ([]*types.Delegation, error)

	// ListAll returns all delegations across all personas (for admin views).
	ListAll(ctx context.Context) ([]*types.Delegation, error)

	// Revoke marks a delegation as revoked by setting revoked_at.
	// Returns ErrNotFound if the delegation does not exist.
	Revoke(ctx context.Context, id string) error

	// CleanupExpired revokes all delegations that have passed their expires_at.
	// Returns the number of delegations expired.
	CleanupExpired(ctx context.Context) (int, error)
}
