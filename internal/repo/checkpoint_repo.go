package repo

import (
	"context"
	"time"
)

// AgentCheckpoint captures a snapshot of an agent's working state for
// crash recovery and rehydration. Checkpoints are created on significant
// state changes (task transitions, CommHub messages, refactor tx begin).
type AgentCheckpoint struct {
	ID              string    `json:"id"`
	AgentID         string    `json:"agent_id"`
	TaskID          string    `json:"task_id"`
	LastMessageID   string    `json:"last_message_id"`
	WorkingContext  string    `json:"working_context"`  // JSON object
	ActiveFiles     string    `json:"active_files"`     // JSON array of file paths
	RefactorTxID    string    `json:"refactor_tx_id"`
	CheckpointedAt  time.Time `json:"checkpointed_at"`
}

// CheckpointRepo handles agent checkpoint CRUD for crash recovery.
type CheckpointRepo interface {
	// Save inserts a new checkpoint. If id is empty, one is generated.
	Save(ctx context.Context, cp *AgentCheckpoint) error

	// GetLatest returns the most recent checkpoint for the given agent.
	// Returns an error if no checkpoint exists.
	GetLatest(ctx context.Context, agentID string) (*AgentCheckpoint, error)

	// List returns the N most recent checkpoints for the given agent,
	// ordered by checkpointed_at DESC.
	List(ctx context.Context, agentID string, limit int) ([]*AgentCheckpoint, error)

	// Delete removes a checkpoint by ID.
	Delete(ctx context.Context, id string) error

	// DeleteOlderThan removes checkpoints older than the given timestamp
	// for the given agent, returning the count of deleted rows.
	DeleteOlderThan(ctx context.Context, agentID string, before time.Time) (int, error)
}
