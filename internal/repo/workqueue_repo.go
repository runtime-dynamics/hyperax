package repo

import (
	"context"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// WorkQueueRepo manages the durable per-agent work queue.
// Items are enqueued on message delivery and dequeued by the Agent Scheduler.
type WorkQueueRepo interface {
	// Enqueue adds a work item to an agent's durable queue.
	Enqueue(ctx context.Context, item *types.WorkQueueItem) error

	// Dequeue atomically retrieves and marks consumed the next unconsumed item
	// for the given agent, ordered by priority DESC then created_at ASC.
	// Returns (nil, nil) when no items are available.
	Dequeue(ctx context.Context, agentName string) (*types.WorkQueueItem, error)

	// PeekCount returns the number of unconsumed items for an agent.
	PeekCount(ctx context.Context, agentName string) (int, error)

	// PurgeConsumed deletes consumed items older than the given cutoff.
	PurgeConsumed(ctx context.Context, olderThan time.Time) (int64, error)

	// RenameAgent cascades an agent name change to all work queue entries.
	RenameAgent(ctx context.Context, oldName, newName string) error
}
