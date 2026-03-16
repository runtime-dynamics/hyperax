package delegation

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// SessionGrants represents the effective permissions from active delegations
// for a given persona. This is consumed by the auth layer when building
// a session or enriching JWT claims.
type SessionGrants struct {
	// ElevatedLevel is the highest clearance level granted via delegation.
	// -1 means no elevation (use the persona's own level).
	ElevatedLevel int `json:"elevated_level"`

	// AdditionalScopes are scopes granted via scope_access delegations.
	AdditionalScopes []string `json:"additional_scopes,omitempty"`

	// DelegatedBy lists the granter persona IDs for all active grants.
	DelegatedBy []string `json:"delegated_by,omitempty"`

	// HasCredentials is true if any active credential_passthrough grant exists.
	HasCredentials bool `json:"has_credentials"`
}

// ResolveSessionGrants queries active delegations for a persona and computes
// the effective elevated clearance level and expanded scopes. Expired grants
// are excluded. This function is designed to be called during auth session
// construction.
func ResolveSessionGrants(ctx context.Context, delegationRepo repo.DelegationRepo, personaID string) (*SessionGrants, error) {
	if delegationRepo == nil {
		return &SessionGrants{ElevatedLevel: -1}, nil
	}

	delegations, err := delegationRepo.ListByGrantee(ctx, personaID)
	if err != nil {
		return nil, fmt.Errorf("delegation.ResolveSessionGrants: %w", err)
	}

	grants := &SessionGrants{
		ElevatedLevel: -1,
	}

	now := time.Now().UTC()
	seenGranters := make(map[string]bool)

	for _, d := range delegations {
		// Skip revoked.
		if !d.IsActive() {
			continue
		}

		// Skip expired (double-check beyond the query filter).
		if d.ExpiresAt != "" {
			exp, parseErr := time.Parse(time.RFC3339, d.ExpiresAt)
			if parseErr == nil && now.After(exp) {
				continue
			}
		}

		// Track unique granters.
		if !seenGranters[d.GranterID] {
			seenGranters[d.GranterID] = true
			grants.DelegatedBy = append(grants.DelegatedBy, d.GranterID)
		}

		switch d.GrantType {
		case types.GrantClearanceElevation:
			if d.ElevatedLevel > grants.ElevatedLevel {
				grants.ElevatedLevel = d.ElevatedLevel
			}

		case types.GrantScopeAccess:
			grants.AdditionalScopes = append(grants.AdditionalScopes, d.Scopes...)

		case types.GrantCredentialPassthrough:
			grants.HasCredentials = true
		}
	}

	return grants, nil
}

// ApplyToClearance returns the effective clearance level considering delegation elevation.
// If no elevation grant exists (ElevatedLevel == -1), returns the original level.
func (sg *SessionGrants) ApplyToClearance(originalLevel int) int {
	if sg.ElevatedLevel > originalLevel {
		return sg.ElevatedLevel
	}
	return originalLevel
}

// MergeScopes returns the union of original scopes and delegation-granted scopes.
func (sg *SessionGrants) MergeScopes(originalScopes []string) []string {
	if len(sg.AdditionalScopes) == 0 {
		return originalScopes
	}

	seen := make(map[string]bool, len(originalScopes))
	merged := make([]string, 0, len(originalScopes)+len(sg.AdditionalScopes))
	for _, s := range originalScopes {
		seen[s] = true
		merged = append(merged, s)
	}
	for _, s := range sg.AdditionalScopes {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	return merged
}
