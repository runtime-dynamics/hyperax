package repo

import (
	"context"
	"time"
)

// LifecycleTransition records an agent state change.
type LifecycleTransition struct {
	ID        string
	AgentID   string
	FromState string
	ToState   string
	Reason    string
	CreatedAt time.Time
}

// AgentState represents an agent's current lifecycle state and last heartbeat.
type AgentState struct {
	AgentID   string
	State     string
	UpdatedAt time.Time
}

// LifecycleRepo handles agent lifecycle state and heartbeats.
type LifecycleRepo interface {
	LogTransition(ctx context.Context, entry *LifecycleTransition) error
	GetState(ctx context.Context, agentID string) (string, error)
	WriteHeartbeat(ctx context.Context, agentID string) error
	GetStaleAgents(ctx context.Context, ttl time.Duration) ([]string, error)
	ListAgentStates(ctx context.Context) ([]*AgentState, error)
}
