package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// InterjectionRepo handles the Andon Cord interjection system.
type InterjectionRepo interface {
	// Create inserts a new interjection with status "active".
	Create(ctx context.Context, interjection *types.Interjection) (string, error)

	// GetByID returns a single interjection by its ID.
	GetByID(ctx context.Context, id string) (*types.Interjection, error)

	// GetActive returns all active interjections for the given scope.
	GetActive(ctx context.Context, scope string) ([]*types.Interjection, error)

	// GetAllActive returns all active interjections across all scopes.
	GetAllActive(ctx context.Context) ([]*types.Interjection, error)

	// GetHistory returns resolved interjections for a scope, newest first.
	GetHistory(ctx context.Context, scope string, limit int) ([]*types.Interjection, error)

	// Resolve marks an interjection as resolved with action and resolver info.
	Resolve(ctx context.Context, id string, action *types.ResolutionAction) error

	// Expire marks an interjection as expired (TTL-based auto-resolution).
	Expire(ctx context.Context, id string) error

	// GetClearanceLevel retrieves the clearance_level for a persona from the personas table.
	GetClearanceLevel(ctx context.Context, personaID string) (int, error)

	// --- Sieve Bypass ---

	// CreateBypass stores a sieve bypass grant.
	CreateBypass(ctx context.Context, bypass *types.SieveBypass) (string, error)

	// GetActiveBypass returns active (non-expired, non-revoked) bypasses for a scope.
	GetActiveBypass(ctx context.Context, scope string) ([]*types.SieveBypass, error)

	// RevokeBypass marks a bypass as revoked.
	RevokeBypass(ctx context.Context, id string) error

	// ExpireBypasses marks all expired but non-revoked bypasses as revoked.
	ExpireBypasses(ctx context.Context) (int, error)

	// --- Dead Letter Queue ---

	// EnqueueDLQ adds an entry to the dead letter queue.
	EnqueueDLQ(ctx context.Context, entry *types.DLQEntry) (string, error)

	// ListDLQ returns queued DLQ entries for an interjection.
	ListDLQ(ctx context.Context, interjectionID string, limit int) ([]*types.DLQEntry, error)

	// ReplayDLQ marks a DLQ entry as replayed.
	ReplayDLQ(ctx context.Context, id string) error

	// DismissDLQ marks a DLQ entry as dismissed.
	DismissDLQ(ctx context.Context, id string) error

	// CountDLQ returns the number of queued DLQ entries for a given interjection.
	CountDLQ(ctx context.Context, interjectionID string) (int, error)
}
