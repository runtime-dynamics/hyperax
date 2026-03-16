package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// AgentMailRepo provides persistence for the AgentMail postbox system.
// It manages outbound/inbound message queues, acknowledgments, and
// dead letter entries. All methods are context-aware for cancellation
// and timeout support.
type AgentMailRepo interface {
	// Enqueue persists a new outbound or inbound mail message.
	// The direction field ("outbound" or "inbound") determines the queue.
	Enqueue(ctx context.Context, mail *types.AgentMail, direction string) error

	// Dequeue retrieves and removes up to limit messages from the specified
	// direction queue, ordered by priority (urgent first) then sent_at ASC.
	// Returns an empty slice if no messages are available.
	Dequeue(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error)

	// Peek returns up to limit messages from the specified direction queue
	// without removing them. Ordered by priority then sent_at ASC.
	Peek(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error)

	// GetByID retrieves a single mail message by its ID.
	// Returns an error if not found.
	GetByID(ctx context.Context, id string) (*types.AgentMail, error)

	// Delete removes a mail message by its ID.
	Delete(ctx context.Context, id string) error

	// CountByDirection returns the number of messages in the given direction queue.
	CountByDirection(ctx context.Context, direction string) (int, error)

	// Acknowledge records an acknowledgment for a mail message.
	Acknowledge(ctx context.Context, ack *types.MailAck) error

	// GetAck retrieves the acknowledgment for a mail message.
	// Returns an error if no acknowledgment exists.
	GetAck(ctx context.Context, mailID string) (*types.MailAck, error)

	// ListUnacknowledged returns mail messages that have not been acknowledged
	// and whose ack deadline has passed. Used by the Mailroom to detect
	// messages that need retry or dead-lettering.
	ListUnacknowledged(ctx context.Context, limit int) ([]*types.AgentMail, error)

	// QuarantineToDLO moves a failed message to the dead letter office.
	QuarantineToDLO(ctx context.Context, entry *types.DeadLetterEntry) error

	// ListDLO returns dead letter entries, ordered by quarantined_at DESC.
	ListDLO(ctx context.Context, limit int) ([]*types.DeadLetterEntry, error)

	// RemoveFromDLO removes a dead letter entry by its ID.
	RemoveFromDLO(ctx context.Context, id string) error
}
