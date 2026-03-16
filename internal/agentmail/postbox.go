package agentmail

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// Direction constants for the postbox queues.
const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"
)

// Postbox manages the inbound and outbound AgentMail queues backed by SQLite.
// It provides a high-level API for enqueuing, dequeuing, acknowledging, and
// dead-lettering messages, publishing domain events on the Nervous System
// EventBus for each significant operation.
type Postbox struct {
	repo   repo.AgentMailRepo
	bus    *nervous.EventBus
	logger *slog.Logger
}

// NewPostbox creates a Postbox wired to the given repository and event bus.
func NewPostbox(r repo.AgentMailRepo, bus *nervous.EventBus, logger *slog.Logger) *Postbox {
	return &Postbox{
		repo:   r,
		bus:    bus,
		logger: logger,
	}
}

// SendOutbound enqueues an outbound message for delivery by the Mailroom.
// Publishes an EventAgentMailSent event on success.
func (p *Postbox) SendOutbound(ctx context.Context, mail *types.AgentMail) error {
	if mail.ID == "" {
		mail.ID = fmt.Sprintf("mail-%d", time.Now().UnixNano())
	}
	if mail.AckDeadline == 0 {
		mail.AckDeadline = types.AckDeadlineFor(mail.Priority)
	}

	if err := p.repo.Enqueue(ctx, mail, DirectionOutbound); err != nil {
		return fmt.Errorf("agentmail.Postbox.SendOutbound: %w", err)
	}

	p.bus.Publish(nervous.NewEvent(
		types.EventAgentMailSent,
		"postbox",
		mail.WorkspaceID,
		map[string]string{
			"mail_id":  mail.ID,
			"from":     mail.From,
			"to":       mail.To,
			"priority": string(mail.Priority),
		},
	))

	p.logger.Debug("outbound mail enqueued",
		"id", mail.ID,
		"from", mail.From,
		"to", mail.To,
		"priority", mail.Priority,
	)

	return nil
}

// ReceiveInbound enqueues an inbound message (arriving from an external adapter).
// Publishes an EventAgentMailReceived event on success.
func (p *Postbox) ReceiveInbound(ctx context.Context, mail *types.AgentMail) error {
	if mail.ID == "" {
		mail.ID = fmt.Sprintf("mail-%d", time.Now().UnixNano())
	}

	if err := p.repo.Enqueue(ctx, mail, DirectionInbound); err != nil {
		return fmt.Errorf("agentmail.Postbox.ReceiveInbound: %w", err)
	}

	p.bus.Publish(nervous.NewEvent(
		types.EventAgentMailReceived,
		"postbox",
		mail.WorkspaceID,
		map[string]string{
			"mail_id":  mail.ID,
			"from":     mail.From,
			"to":       mail.To,
			"priority": string(mail.Priority),
		},
	))

	p.logger.Debug("inbound mail enqueued",
		"id", mail.ID,
		"from", mail.From,
		"to", mail.To,
	)

	return nil
}

// DequeueOutbound retrieves and removes up to limit outbound messages for delivery.
func (p *Postbox) DequeueOutbound(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	msgs, err := p.repo.Dequeue(ctx, DirectionOutbound, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.DequeueOutbound: %w", err)
	}
	return msgs, nil
}

// DequeueInbound retrieves and removes up to limit inbound messages for processing.
func (p *Postbox) DequeueInbound(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	msgs, err := p.repo.Dequeue(ctx, DirectionInbound, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.DequeueInbound: %w", err)
	}
	return msgs, nil
}

// PeekOutbound returns up to limit outbound messages without removing them.
func (p *Postbox) PeekOutbound(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	msgs, err := p.repo.Peek(ctx, DirectionOutbound, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.PeekOutbound: %w", err)
	}
	return msgs, nil
}

// PeekInbound returns up to limit inbound messages without removing them.
func (p *Postbox) PeekInbound(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	msgs, err := p.repo.Peek(ctx, DirectionInbound, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.PeekInbound: %w", err)
	}
	return msgs, nil
}

// Acknowledge records an acknowledgment for a delivered message.
// Publishes an EventAgentMailAck event on success.
func (p *Postbox) Acknowledge(ctx context.Context, ack *types.MailAck) error {
	if err := p.repo.Acknowledge(ctx, ack); err != nil {
		return fmt.Errorf("agentmail.Postbox.Acknowledge: %w", err)
	}

	p.bus.Publish(nervous.NewEvent(
		types.EventAgentMailAck,
		"postbox",
		"",
		map[string]string{
			"mail_id":     ack.MailID,
			"instance_id": ack.InstanceID,
			"status":      ack.Status,
		},
	))

	p.logger.Debug("mail acknowledged",
		"mail_id", ack.MailID,
		"status", ack.Status,
	)

	return nil
}

// Quarantine moves a failed message to the dead letter office.
// Publishes an EventAgentMailDLOQuarantined event on success.
func (p *Postbox) Quarantine(ctx context.Context, entry *types.DeadLetterEntry) error {
	if err := p.repo.QuarantineToDLO(ctx, entry); err != nil {
		return fmt.Errorf("agentmail.Postbox.Quarantine: %w", err)
	}

	p.bus.Publish(nervous.NewEvent(
		types.EventAgentMailDLOQuarantined,
		"postbox",
		"",
		map[string]string{
			"dle_id":   entry.ID,
			"mail_id":  entry.MailID,
			"reason":   entry.Reason,
			"attempts": fmt.Sprintf("%d", entry.Attempts),
		},
	))

	p.logger.Warn("mail quarantined to DLO",
		"dle_id", entry.ID,
		"mail_id", entry.MailID,
		"reason", entry.Reason,
		"attempts", entry.Attempts,
	)

	return nil
}

// OutboundCount returns the number of pending outbound messages.
func (p *Postbox) OutboundCount(ctx context.Context) (int, error) {
	n, err := p.repo.CountByDirection(ctx, DirectionOutbound)
	if err != nil {
		return 0, fmt.Errorf("agentmail.Postbox.OutboundCount: %w", err)
	}
	return n, nil
}

// InboundCount returns the number of pending inbound messages.
func (p *Postbox) InboundCount(ctx context.Context) (int, error) {
	n, err := p.repo.CountByDirection(ctx, DirectionInbound)
	if err != nil {
		return 0, fmt.Errorf("agentmail.Postbox.InboundCount: %w", err)
	}
	return n, nil
}

// ListUnacknowledged returns messages that missed their ack deadline.
func (p *Postbox) ListUnacknowledged(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	msgs, err := p.repo.ListUnacknowledged(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.ListUnacknowledged: %w", err)
	}
	return msgs, nil
}

// ListDLO returns dead letter entries.
func (p *Postbox) ListDLO(ctx context.Context, limit int) ([]*types.DeadLetterEntry, error) {
	entries, err := p.repo.ListDLO(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.ListDLO: %w", err)
	}
	return entries, nil
}

// RemoveFromDLO removes a dead letter entry by its ID.
func (p *Postbox) RemoveFromDLO(ctx context.Context, id string) error {
	if err := p.repo.RemoveFromDLO(ctx, id); err != nil {
		return fmt.Errorf("agentmail.Postbox.RemoveFromDLO: %w", err)
	}
	return nil
}

// GetByID retrieves a single mail message by its ID.
func (p *Postbox) GetByID(ctx context.Context, id string) (*types.AgentMail, error) {
	mail, err := p.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("agentmail.Postbox.GetByID: %w", err)
	}
	return mail, nil
}
