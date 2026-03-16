package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// CheckpointRepo implements repo.CheckpointRepo for SQLite.
type CheckpointRepo struct {
	db *sql.DB
}

// compile-time interface assertion.
var _ repo.CheckpointRepo = (*CheckpointRepo)(nil)

// Save inserts a new agent checkpoint. Generates an ID if one is not provided.
func (r *CheckpointRepo) Save(ctx context.Context, cp *repo.AgentCheckpoint) error {
	if cp.ID == "" {
		cp.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_checkpoints
		 (id, agent_id, task_id, last_message_id, working_context, active_files, refactor_tx_id, checkpointed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		cp.ID, cp.AgentID, cp.TaskID, cp.LastMessageID,
		cp.WorkingContext, cp.ActiveFiles, cp.RefactorTxID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CheckpointRepo.Save: %w", err)
	}
	return nil
}

// GetLatest returns the most recent checkpoint for the given agent.
func (r *CheckpointRepo) GetLatest(ctx context.Context, agentID string) (*repo.AgentCheckpoint, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, agent_id, task_id, last_message_id, working_context,
		        active_files, refactor_tx_id, checkpointed_at
		 FROM agent_checkpoints
		 WHERE agent_id = ?
		 ORDER BY checkpointed_at DESC
		 LIMIT 1`,
		agentID,
	)
	return scanCheckpoint(row)
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
		return nil, fmt.Errorf("sqlite.CheckpointRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*repo.AgentCheckpoint
	for rows.Next() {
		cp, err := scanCheckpointRow(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.CheckpointRepo.List: %w", err)
		}
		result = append(result, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CheckpointRepo.List: %w", err)
	}
	return result, nil
}

// Delete removes a checkpoint by ID.
func (r *CheckpointRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM agent_checkpoints WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CheckpointRepo.Delete: %w", err)
	}
	n, _ := res.RowsAffected()
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
		agentID, before.UTC().Format(sqliteTimeFormat),
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite.CheckpointRepo.DeleteOlderThan: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// scanCheckpoint scans a single checkpoint from a QueryRow result.
func scanCheckpoint(row *sql.Row) (*repo.AgentCheckpoint, error) {
	var cp repo.AgentCheckpoint
	var checkpointedAt string
	err := row.Scan(
		&cp.ID, &cp.AgentID, &cp.TaskID, &cp.LastMessageID,
		&cp.WorkingContext, &cp.ActiveFiles, &cp.RefactorTxID,
		&checkpointedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no checkpoint found")
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanCheckpoint: %w", err)
	}
	cp.CheckpointedAt, _ = time.Parse(sqliteTimeFormat, checkpointedAt)
	return &cp, nil
}

// scanCheckpointRow scans a single checkpoint from a Rows iterator.
func scanCheckpointRow(rows *sql.Rows) (*repo.AgentCheckpoint, error) {
	var cp repo.AgentCheckpoint
	var checkpointedAt string
	err := rows.Scan(
		&cp.ID, &cp.AgentID, &cp.TaskID, &cp.LastMessageID,
		&cp.WorkingContext, &cp.ActiveFiles, &cp.RefactorTxID,
		&checkpointedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanCheckpointRow: %w", err)
	}
	cp.CheckpointedAt, _ = time.Parse(sqliteTimeFormat, checkpointedAt)
	return &cp, nil
}
