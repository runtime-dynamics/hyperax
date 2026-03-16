package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// WorkQueueRepo implements repo.WorkQueueRepo for MySQL.
type WorkQueueRepo struct {
	db *sql.DB
}

// Enqueue adds a work item to an agent's durable queue.
func (r *WorkQueueRepo) Enqueue(ctx context.Context, item *types.WorkQueueItem) error {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_work_queue (id, agent_name, from_agent, content, content_type, trust, session_id, priority)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.AgentName, item.FromAgent, item.Content,
		item.ContentType, item.Trust, item.SessionID, item.Priority,
	)
	if err != nil {
		return fmt.Errorf("mysql.WorkQueueRepo.Enqueue: %w", err)
	}
	return nil
}

// Dequeue atomically retrieves and marks consumed the next unconsumed item
// for the given agent using SELECT FOR UPDATE for safe concurrent access.
// Returns (nil, nil) when no items are available.
func (r *WorkQueueRepo) Dequeue(ctx context.Context, agentName string) (*types.WorkQueueItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkQueueRepo.Dequeue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Select and lock the next pending item.
	var itemID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM agent_work_queue
		 WHERE agent_name = ? AND consumed_at IS NULL
		 ORDER BY priority DESC, created_at ASC
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`,
		agentName,
	).Scan(&itemID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkQueueRepo.Dequeue: %w", err)
	}

	// Mark as consumed.
	_, err = tx.ExecContext(ctx,
		`UPDATE agent_work_queue SET consumed_at = NOW() WHERE id = ?`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkQueueRepo.Dequeue: %w", err)
	}

	// Read the full item.
	item := &types.WorkQueueItem{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, agent_name, from_agent, content, content_type, trust, session_id, priority, created_at, consumed_at
		 FROM agent_work_queue WHERE id = ?`,
		itemID,
	).Scan(&item.ID, &item.AgentName, &item.FromAgent, &item.Content,
		&item.ContentType, &item.Trust, &item.SessionID, &item.Priority,
		&item.CreatedAt, &item.ConsumedAt)
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkQueueRepo.Dequeue: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mysql.WorkQueueRepo.Dequeue: %w", err)
	}
	return item, nil
}

// PeekCount returns the number of unconsumed items for an agent.
func (r *WorkQueueRepo) PeekCount(ctx context.Context, agentName string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_work_queue WHERE agent_name = ? AND consumed_at IS NULL`,
		agentName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mysql.WorkQueueRepo.PeekCount: %w", err)
	}
	return count, nil
}

// PurgeConsumed deletes consumed items older than the given cutoff.
func (r *WorkQueueRepo) PurgeConsumed(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM agent_work_queue WHERE consumed_at IS NOT NULL AND consumed_at < ?`,
		olderThan,
	)
	if err != nil {
		return 0, fmt.Errorf("mysql.WorkQueueRepo.PurgeConsumed: %w", err)
	}
	return res.RowsAffected()
}

// RenameAgent cascades an agent name change to all work queue entries.
func (r *WorkQueueRepo) RenameAgent(ctx context.Context, oldName, newName string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_work_queue SET agent_name = ? WHERE agent_name = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("mysql.WorkQueueRepo.RenameAgent: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_work_queue SET from_agent = ? WHERE from_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("mysql.WorkQueueRepo.RenameAgent: %w", err)
	}
	return nil
}
