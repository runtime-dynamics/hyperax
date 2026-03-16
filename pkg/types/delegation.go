package types

// DelegationGrantType defines the kind of delegation grant.
type DelegationGrantType string

const (
	// GrantClearanceElevation temporarily elevates the grantee's clearance level.
	GrantClearanceElevation DelegationGrantType = "clearance_elevation"
	// GrantCredentialPassthrough passes the granter's credential through to the grantee.
	GrantCredentialPassthrough DelegationGrantType = "credential_passthrough"
	// GrantScopeAccess grants access to additional scopes beyond the grantee's own.
	GrantScopeAccess DelegationGrantType = "scope_access"
)

// Delegation represents an on-behalf-of grant from one persona to another.
type Delegation struct {
	ID             string              `json:"id"`
	GranterID      string              `json:"granter_id"`
	GranteeID      string              `json:"grantee_id"`
	GrantType      DelegationGrantType `json:"grant_type"`
	CredentialKey  string              `json:"credential_key,omitempty"`
	ElevatedLevel  int                 `json:"elevated_level,omitempty"`
	Scopes         []string            `json:"scopes,omitempty"`
	ExpiresAt      string              `json:"expires_at,omitempty"`
	Reason         string              `json:"reason"`
	CreatedAt      string              `json:"created_at"`
	RevokedAt      string              `json:"revoked_at,omitempty"`
}

// IsActive returns true if the delegation has not been revoked.
func (d *Delegation) IsActive() bool {
	return d.RevokedAt == ""
}

// Delegation event types for the nervous system.
const (
	EventDelegationGranted = "delegation.granted"
	EventDelegationRevoked = "delegation.revoked"
	EventDelegationExpired = "delegation.expired"
)
