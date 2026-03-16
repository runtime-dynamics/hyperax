package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// MCPTokenRepo handles MCP bearer token persistence.
type MCPTokenRepo interface {
	// Create stores a new MCP token. The token_hash must already be bcrypt-hashed.
	Create(ctx context.Context, token *types.MCPToken) error

	// ValidateToken looks up a token by its bcrypt hash comparison.
	// Returns the token if found, not revoked, and not expired.
	// Returns an error if the token is invalid or not found.
	ValidateToken(ctx context.Context, plaintext string) (*types.MCPToken, error)

	// Revoke marks a token as revoked by setting its revoked_at timestamp.
	Revoke(ctx context.Context, tokenID string) error

	// ListByAgent returns all tokens (including revoked) for an agent.
	ListByAgent(ctx context.Context, agentID string) ([]*types.MCPToken, error)

	// DeleteExpired removes tokens that have passed their expiry time.
	DeleteExpired(ctx context.Context) (int64, error)

	// GetByID retrieves a single token by its ID.
	GetByID(ctx context.Context, tokenID string) (*types.MCPToken, error)
}
