package auth

import (
	"context"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

// PermissionType represents the kind of action being checked.
type PermissionType string

const (
	PermRead   PermissionType = "read"
	PermWrite  PermissionType = "write"
	PermDelete PermissionType = "delete"
	PermAdmin  PermissionType = "admin"
	PermCreate PermissionType = "create"
	PermExec   PermissionType = "execute"
)

// Scope represents the ABAC scope level.
type Scope string

const (
	ScopeOrg      Scope = "org"
	ScopeTeam     Scope = "team"
	ScopeResource Scope = "resource"
)

// Session represents an authenticated caller's identity and permissions.
// It is derived from the AuthContext injected by the transport layer
// and optionally enriched with delegation grants.
type Session struct {
	PersonaID      string         `json:"persona_id"`
	TokenID        string         `json:"token_id"`
	ClearanceLevel int            `json:"clearance_level"`
	Scopes         []string       `json:"scopes"`
	Authenticated  bool           `json:"authenticated"`
	DelegatedBy    string         `json:"delegated_by,omitempty"` // If acting on behalf of another persona
	Permissions    []Permission   `json:"permissions,omitempty"`  // Explicit permission grants
}

// Permission is an explicit ABAC permission entry.
type Permission struct {
	Action   PermissionType `json:"action"`
	Scope    Scope          `json:"scope"`
	Resource string         `json:"resource,omitempty"` // Optional resource identifier
}

// GetSession extracts the Session from the request context.
// If no authentication is present, returns a zero-value Session with
// Authenticated=false and ClearanceLevel=0.
func GetSession(ctx context.Context) Session {
	ac := mcp.AuthFromContext(ctx)
	return Session{
		PersonaID:      ac.PersonaID,
		TokenID:        ac.TokenID,
		ClearanceLevel: ac.ClearanceLevel,
		Scopes:         ac.Scopes,
		Authenticated:  ac.Authenticated,
	}
}

// CheckABACPermissions evaluates whether the session has the required
// clearance level and action permission for the requested operation.
//
// Rules:
//   - Clearance level must be >= requiredClearance.
//   - If scopes are configured on the token and requiredScope is non-empty,
//     the scope must be present.
//   - Returns true if access is granted, false otherwise.
func CheckABACPermissions(session Session, requiredClearance int, requiredAction PermissionType, requiredScope string) bool {
	// Clearance check.
	if session.ClearanceLevel < requiredClearance {
		return false
	}

	// Scope check: if token has scopes and a scope is required, verify membership.
	if requiredScope != "" && len(session.Scopes) > 0 {
		found := false
		for _, s := range session.Scopes {
			if s == requiredScope || s == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// HasClearance checks if the session meets the minimum clearance level.
func (s Session) HasClearance(level int) bool {
	return s.ClearanceLevel >= level
}

// HasScope checks if the session has a specific scope (or wildcard).
func (s Session) HasScope(scope string) bool {
	if len(s.Scopes) == 0 {
		return true // No scope restrictions = all scopes.
	}
	for _, sc := range s.Scopes {
		if sc == scope || sc == "*" {
			return true
		}
	}
	return false
}

// ToAuthContext converts a Session back to a types.AuthContext.
// Useful for injecting enriched sessions (e.g., with delegation) back into context.
func (s Session) ToAuthContext() types.AuthContext {
	return types.AuthContext{
		PersonaID:      s.PersonaID,
		TokenID:        s.TokenID,
		ClearanceLevel: s.ClearanceLevel,
		Scopes:         s.Scopes,
		Authenticated:  s.Authenticated,
	}
}
