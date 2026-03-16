package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// WorkQueueRepo implements repo.WorkQueueRepo for SQLite.
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
		return fmt.Errorf("sqlite.WorkQueueRepo.Enqueue: %w", err)
	}
	return nil
}

// Dequeue atomically retrieves and marks consumed the next unconsumed item
// for the given agent, ordered by priority DESC then created_at ASC.
// Returns (nil, nil) when no items are available.
func (r *WorkQueueRepo) Dequeue(ctx context.Context, agentName string) (*types.WorkQueueItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkQueueRepo.Dequeue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Select the next pending item.
	var itemID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM agent_work_queue
		 WHERE agent_name = ? COLLATE NOCASE AND consumed_at IS NULL
		 ORDER BY priority DESC, created_at ASC
		 LIMIT 1`,
		agentName,
	).Scan(&itemID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkQueueRepo.Dequeue: %w", err)
	}

	// Mark as consumed.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = tx.ExecContext(ctx,
		`UPDATE agent_work_queue SET consumed_at = ? WHERE id = ?`,
		now, itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkQueueRepo.Dequeue: %w", err)
	}

	// Read the full item.
	item := &types.WorkQueueItem{}
	var createdAt string
	err = tx.QueryRowContext(ctx,
		`SELECT id, agent_name, from_agent, content, content_type, trust, session_id, priority, created_at
		 FROM agent_work_queue WHERE id = ?`,
		itemID,
	).Scan(&item.ID, &item.AgentName, &item.FromAgent, &item.Content,
		&item.ContentType, &item.Trust, &item.SessionID, &item.Priority, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkQueueRepo.Dequeue: %w", err)
	}
	item.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)

	consumedAt := time.Now().UTC()
	item.ConsumedAt = &consumedAt

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkQueueRepo.Dequeue: %w", err)
	}
	return item, nil
}

// PeekCount returns the number of unconsumed items for an agent.
func (r *WorkQueueRepo) PeekCount(ctx context.Context, agentName string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_work_queue WHERE agent_name = ? COLLATE NOCASE AND consumed_at IS NULL`,
		agentName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite.WorkQueueRepo.PeekCount: %w", err)
	}
	return count, nil
}

// PurgeConsumed deletes consumed items older than the given cutoff.
func (r *WorkQueueRepo) PurgeConsumed(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format("2006-01-02 15:04:05")
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM agent_work_queue WHERE consumed_at IS NOT NULL AND consumed_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite.WorkQueueRepo.PurgeConsumed: %w", err)
	}
	return res.RowsAffected()
}

// RenameAgent cascades an agent name change to all work queue entries.
func (r *WorkQueueRepo) RenameAgent(ctx context.Context, oldName, newName string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_work_queue SET agent_name = ? WHERE agent_name = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.WorkQueueRepo.RenameAgent: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_work_queue SET from_agent = ? WHERE from_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.WorkQueueRepo.RenameAgent: %w", err)
	}
	return nil
}
