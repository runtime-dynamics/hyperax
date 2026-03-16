package types

import "time"

// InterjectionSeverity levels for the Andon Cord system.
type InterjectionSeverity string

const (
	SeverityWarning  InterjectionSeverity = "warning"  // Log + notify, agents may continue
	SeverityCritical InterjectionSeverity = "critical" // Agents in scope must pause
	SeverityFatal    InterjectionSeverity = "fatal"    // Full stop, all agents halt
)

// InterjectionScope defines the blast radius of an interjection.
type InterjectionScope string

const (
	ScopeAgent     InterjectionScope = "agent"     // Single agent
	ScopeWorkspace InterjectionScope = "workspace" // All agents in workspace
	ScopeGlobal    InterjectionScope = "global"    // All agents instance-wide
)

// InterjectionStatus tracks the lifecycle of an interjection.
type InterjectionStatus string

const (
	StatusActive   InterjectionStatus = "active"
	StatusResolved InterjectionStatus = "resolved"
	StatusExpired  InterjectionStatus = "expired"
)

// Interjection represents an Andon Cord pull stored in the database.
type Interjection struct {
	ID                 string               `json:"id"`
	Scope              string               `json:"scope"`
	Severity           string               `json:"severity"`
	Source             string               `json:"source"`
	Reason             string               `json:"reason"`
	Status             string               `json:"status"`
	Resolution         string               `json:"resolution,omitempty"`
	CreatedBy          string               `json:"created_by,omitempty"`
	SourceClearance    int                  `json:"source_clearance"`
	ResolvedBy         string               `json:"resolved_by,omitempty"`
	ResolverClearance  int                  `json:"resolver_clearance,omitempty"`
	RemediationPersona string               `json:"remediation_persona,omitempty"`
	Action             string               `json:"action,omitempty"` // resume, abort, retry
	TrustLevel         string               `json:"trust_level,omitempty"`
	TraceID            string               `json:"trace_id,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	ResolvedAt         *time.Time           `json:"resolved_at,omitempty"`
	ExpiresAt          *time.Time           `json:"expires_at,omitempty"`
}

// InterjectionSignal is the event payload published when the Andon Cord is pulled.
type InterjectionSignal struct {
	InterjectionID string               `json:"interjection_id"`
	Scope          InterjectionScope    `json:"scope"`
	Severity       InterjectionSeverity `json:"severity"`
	Source         string               `json:"source"`
	Reason         string               `json:"reason"`
}

// ResolutionAction describes how an interjection was resolved.
type ResolutionAction struct {
	InterjectionID    string `json:"interjection_id"`
	ResolvedBy        string `json:"resolved_by"`
	Resolution        string `json:"resolution"`
	Action            string `json:"action"` // "resume", "abort", "retry"
	ResolverClearance int    `json:"resolver_clearance"`
}

// SieveBypass represents a temporary Context Sieve bypass grant.
type SieveBypass struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
	Pattern   string    `json:"pattern"`
	GrantedBy string    `json:"granted_by"`
	GrantedAt time.Time `json:"granted_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason,omitempty"`
	Revoked   bool      `json:"revoked"`
}

// DLQEntry represents a dead-letter queue item held during Safe Mode.
type DLQEntry struct {
	ID              string     `json:"id"`
	InterjectionID  string     `json:"interjection_id"`
	MessageType     string     `json:"message_type"`
	Payload         string     `json:"payload"`
	Source          string     `json:"source"`
	Scope           string     `json:"scope"`
	QueuedAt        time.Time  `json:"queued_at"`
	ReplayedAt      *time.Time `json:"replayed_at,omitempty"`
	DismissedAt     *time.Time `json:"dismissed_at,omitempty"`
	Status          string     `json:"status"` // queued, replayed, dismissed
}
