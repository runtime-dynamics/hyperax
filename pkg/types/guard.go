package types

import "time"

// GuardStatus represents the state of a guard action.
type GuardStatus string

const (
	GuardStatusPending  GuardStatus = "pending"
	GuardStatusApproved GuardStatus = "approved"
	GuardStatusRejected GuardStatus = "rejected"
	GuardStatusTimeout  GuardStatus = "timeout"
)

// GuardAction represents a tool call that requires guard approval.
type GuardAction struct {
	ID            string      `json:"id"`
	ToolName      string      `json:"tool_name"`
	ToolAction    string      `json:"tool_action"`
	ToolParams    string      `json:"tool_params"`
	GuardName     string      `json:"guard_name"`
	CallerPersona string      `json:"caller_persona"`
	Status        GuardStatus `json:"status"`
	DecidedBy     string      `json:"decided_by,omitempty"`
	Notes         string      `json:"notes,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	DecidedAt     *time.Time  `json:"decided_at,omitempty"`
	ExpiresAt     time.Time   `json:"expires_at"`
	TraceID       string      `json:"trace_id,omitempty"`
}
