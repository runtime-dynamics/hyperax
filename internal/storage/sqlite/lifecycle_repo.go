package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// LifecycleRepo implements repo.LifecycleRepo for SQLite.
type LifecycleRepo struct {
	db *sql.DB
}

// LogTransition inserts a lifecycle transition record and upserts the agent's
// heartbeat row with the new state.
func (r *LifecycleRepo) LogTransition(ctx context.Context, entry *repo.LifecycleTransition) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO lifecycle_transitions (id, agent_id, from_state, to_state, reason)
		 VALUES (?, ?, ?, ?, ?)`,
		entry.ID, entry.AgentID, entry.FromState, entry.ToState, entry.Reason,
	)
	if err != nil {
		return fmt.Errorf("sqlite.LifecycleRepo.LogTransition: %w", err)
	}

	// Upsert heartbeat with the new state.
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO agent_heartbeats (agent_id, state, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(agent_id) DO UPDATE SET
		   state = excluded.state,
		   updated_at = excluded.updated_at`,
		entry.AgentID, entry.ToState,
	)
	if err != nil {
		return fmt.Errorf("sqlite.LifecycleRepo.LogTransition: %w", err)
	}

	return nil
}

// GetState returns the current state for an agent from the heartbeats table.
// Returns an error if the agent has no recorded heartbeat.
func (r *LifecycleRepo) GetState(ctx context.Context, agentID string) (string, error) {
	var state string
	err := r.db.QueryRowContext(ctx,
		`SELECT state FROM agent_heartbeats WHERE agent_id = ?`,
		agentID,
	).Scan(&state)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("agent %q not found", agentID)
	}
	if err != nil {
		return "", fmt.Errorf("sqlite.LifecycleRepo.GetState: %w", err)
	}

	return state, nil
}

// WriteHeartbeat upserts the agent heartbeat timestamp without changing state.
func (r *LifecycleRepo) WriteHeartbeat(ctx context.Context, agentID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_heartbeats (agent_id, state, updated_at)
		 VALUES (?, 'idle', datetime('now'))
		 ON CONFLICT(agent_id) DO UPDATE SET
		   updated_at = datetime('now')`,
		agentID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.LifecycleRepo.WriteHeartbeat: %w", err)
	}

	return nil
}

// ListAgentStates returns the current state and last heartbeat for all agents.
func (r *LifecycleRepo) ListAgentStates(ctx context.Context) ([]*repo.AgentState, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT agent_id, state, updated_at FROM agent_heartbeats ORDER BY agent_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.LifecycleRepo.ListAgentStates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*repo.AgentState
	for rows.Next() {
		var a repo.AgentState
		var updatedAt string
		if err := rows.Scan(&a.AgentID, &a.State, &updatedAt); err != nil {
			return nil, fmt.Errorf("sqlite.LifecycleRepo.ListAgentStates: %w", err)
		}
		a.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updatedAt)
		agents = append(agents, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.LifecycleRepo.ListAgentStates: %w", err)
	}
	return agents, nil
}

// GetStaleAgents returns agent IDs whose heartbeat is older than the given TTL.
func (r *LifecycleRepo) GetStaleAgents(ctx context.Context, ttl time.Duration) ([]string, error) {
	seconds := int(ttl.Seconds())

	rows, err := r.db.QueryContext(ctx,
		`SELECT agent_id FROM agent_heartbeats
		 WHERE updated_at < datetime('now', ? || ' seconds')`,
		fmt.Sprintf("-%d", seconds),
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.LifecycleRepo.GetStaleAgents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, fmt.Errorf("sqlite.LifecycleRepo.GetStaleAgents: %w", err)
		}
		agents = append(agents, agentID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.LifecycleRepo.GetStaleAgents: %w", err)
	}
	return agents, nil
}
