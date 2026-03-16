package types

import "time"

// AgentState represents the FSM states for agent lifecycle.
type AgentState string

const (
	AgentStateIdle       AgentState = "idle"
	AgentStateActive     AgentState = "active"
	AgentStateSuspended  AgentState = "suspended"
	AgentStateTerminated AgentState = "terminated"
	AgentStateError      AgentState = "error"
)

// ValidTransitions defines the allowed FSM transitions.
var ValidTransitions = map[AgentState][]AgentState{
	AgentStateIdle:       {AgentStateActive, AgentStateTerminated},
	AgentStateActive:     {AgentStateIdle, AgentStateSuspended, AgentStateError, AgentStateTerminated},
	AgentStateSuspended:  {AgentStateIdle, AgentStateActive, AgentStateTerminated},
	AgentStateError:      {AgentStateIdle, AgentStateTerminated},
	AgentStateTerminated: {}, // terminal state
}

// IsValidTransition checks if a state transition is allowed.
func IsValidTransition(from, to AgentState) bool {
	allowed, exists := ValidTransitions[from]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// AgentInfo describes an agent's current state and metadata.
type AgentInfo struct {
	ID           string     `json:"id"`
	PersonaID    string     `json:"persona_id,omitempty"`
	State        AgentState `json:"state"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	StartedAt    time.Time  `json:"started_at"`
	WorkspaceID  string     `json:"workspace_id,omitempty"`
	CurrentTask  string     `json:"current_task,omitempty"`
}

// LifecycleCheckpoint captures a snapshot of agent state for replay.
type LifecycleCheckpoint struct {
	AgentID   string    `json:"agent_id"`
	State     AgentState `json:"state"`
	Context   string    `json:"context,omitempty"` // serialized agent context
	CreatedAt time.Time `json:"created_at"`
}
