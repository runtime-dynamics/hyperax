package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// CheckpointRepo implements repo.CheckpointRepo for MySQL.
type CheckpointRepo struct {
	db *sql.DB
}

// Save inserts a new agent checkpoint. Generates an ID if one is not provided.
func (r *CheckpointRepo) Save(ctx context.Context, cp *repo.AgentCheckpoint) error {
	if cp.ID == "" {
		cp.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_checkpoints
		 (id, agent_id, task_id, last_message_id, working_context, active_files, refactor_tx_id, checkpointed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NOW())`,
		cp.ID, cp.AgentID, cp.TaskID, cp.LastMessageID,
		cp.WorkingContext, cp.ActiveFiles, cp.RefactorTxID,
	)
	if err != nil {
		return fmt.Errorf("mysql.CheckpointRepo.Save: %w", err)
	}
	return nil
}

// GetLatest returns the most recent checkpoint for the given agent.
func (r *CheckpointRepo) GetLatest(ctx context.Context, agentID string) (*repo.AgentCheckpoint, error) {
	var cp repo.AgentCheckpoint
	err := r.db.QueryRowContext(ctx,
		`SELECT id, agent_id, task_id, last_message_id, working_context,
		        active_files, refactor_tx_id, checkpointed_at
		 FROM agent_checkpoints
		 WHERE agent_id = ?
		 ORDER BY checkpointed_at DESC
		 LIMIT 1`,
		agentID,
	).Scan(
		&cp.ID, &cp.AgentID, &cp.TaskID, &cp.LastMessageID,
		&cp.WorkingContext, &cp.ActiveFiles, &cp.RefactorTxID,
		&cp.CheckpointedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no checkpoint found")
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.CheckpointRepo.GetLatest: %w", err)
	}
	return &cp, nil
}

// List returns the N most recent checkpoints for the given agent.
func (r *CheckpointRepo) List(ctx context.Context, agentID string, limit int) ([]*repo.AgentCheckpoint, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, task_id, last_message_id, working_context,
		        active_files, refactor_tx_id, checkpointed_at
		 FROM agent_checkpoints
		 WHERE agent_id = ?
		 ORDER BY checkpointed_at DESC
		 LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.CheckpointRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*repo.AgentCheckpoint
	for rows.Next() {
		var cp repo.AgentCheckpoint
		if err := rows.Scan(
			&cp.ID, &cp.AgentID, &cp.TaskID, &cp.LastMessageID,
			&cp.WorkingContext, &cp.ActiveFiles, &cp.RefactorTxID,
			&cp.CheckpointedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.CheckpointRepo.List: %w", err)
		}
		result = append(result, &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.CheckpointRepo.List: %w", err)
	}
	return result, nil
}

// Delete removes a checkpoint by ID.
func (r *CheckpointRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM agent_checkpoints WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("mysql.CheckpointRepo.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.CheckpointRepo.Delete: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("checkpoint %q not found", id)
	}
	return nil
}

// DeleteOlderThan removes checkpoints older than the given timestamp for the
// given agent.
func (r *CheckpointRepo) DeleteOlderThan(ctx context.Context, agentID string, before time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM agent_checkpoints
		 WHERE agent_id = ? AND checkpointed_at < ?`,
		agentID, before.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("mysql.CheckpointRepo.DeleteOlderThan: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql.CheckpointRepo.DeleteOlderThan: %w", err)
	}
	return int(n), nil
}
