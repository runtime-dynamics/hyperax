package types

import "time"

// MCPToken represents a bearer token for MCP API authentication.
type MCPToken struct {
	ID             string     `json:"id"`
	AgentID        string     `json:"agent_id"`
	TokenHash      string     `json:"-"` // Never serialised to JSON.
	Label          string     `json:"label"`
	ClearanceLevel int        `json:"clearance_level"`
	Scopes         []string   `json:"scopes"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

// IsExpired reports whether the token has passed its expiry time.
func (t *MCPToken) IsExpired() bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.ExpiresAt)
}

// IsRevoked reports whether the token has been revoked.
func (t *MCPToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

// IsValid reports whether the token is neither expired nor revoked.
func (t *MCPToken) IsValid() bool {
	return !t.IsExpired() && !t.IsRevoked()
}

// Clearance tier constants define the 4-level ABAC clearance hierarchy.
// Higher levels inherit all permissions of lower levels.
const (
	// ClearanceObserver grants read-only access to all public data.
	// Tools at this level: list/get/search/read operations.
	ClearanceObserver = 0

	// ClearanceOperator grants day-to-day operational access.
	// Tools at this level: create/update resources, run pipelines, refactoring,
	// lifecycle transitions, persona management, workflow execution.
	ClearanceOperator = 1

	// ClearanceAdmin grants administrative control over system configuration.
	// Tools at this level: config changes, secret management, provider CRUD,
	// plugin management, alert creation, budget thresholds, token management.
	ClearanceAdmin = 2

	// ClearanceChiefOfStaff grants full system authority including safety overrides.
	// Tools at this level: sieve bypass grants, andon cord operations,
	// interjection resolution, DLQ replay, adapter configuration.
	ClearanceChiefOfStaff = 3
)

// ClearanceTierName returns the human-readable name for a clearance level.
func ClearanceTierName(level int) string {
	switch level {
	case ClearanceObserver:
		return "Observer"
	case ClearanceOperator:
		return "Operator"
	case ClearanceAdmin:
		return "Admin"
	case ClearanceChiefOfStaff:
		return "ChiefOfStaff"
	default:
		return "Unknown"
	}
}

// AuthContext carries the authenticated identity through request context.
type AuthContext struct {
	PersonaID      string   `json:"persona_id"`
	TokenID        string   `json:"token_id"`
	ClearanceLevel int      `json:"clearance_level"`
	Scopes         []string `json:"scopes"`
	Authenticated  bool     `json:"authenticated"`
	PluginHash     string   `json:"plugin_hash,omitempty"`
	GuardBypass    bool     `json:"guard_bypass,omitempty"`
}
