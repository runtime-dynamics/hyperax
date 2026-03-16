package agentmail

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// MailroomConfig holds tunable parameters for the Mailroom dispatcher.
type MailroomConfig struct {
	// PollInterval is how often the Mailroom polls the outbound queue.
	// Default: 2 seconds.
	PollInterval time.Duration

	// InboundPollInterval is how often the Mailroom polls adapters for inbound messages.
	// Default: 5 seconds.
	InboundPollInterval time.Duration

	// BatchSize is the maximum number of outbound messages dequeued per poll cycle.
	// Default: 50.
	BatchSize int

	// MaxRetries is the number of delivery attempts before dead-lettering a message.
	// Default: 3.
	MaxRetries int
}

// DefaultMailroomConfig returns sensible defaults for the Mailroom.
func DefaultMailroomConfig() MailroomConfig {
	return MailroomConfig{
		PollInterval:        2 * time.Second,
		InboundPollInterval: 5 * time.Second,
		BatchSize:           50,
		MaxRetries:          3,
	}
}

// Mailroom is the dispatch engine that moves messages between the Postbox
// and external adapters. It runs two polling loops:
//
//  1. Outbound loop: dequeues messages from the Postbox outbound queue,
//     resolves the target adapter, and dispatches via adapter.Send().
//
//  2. Inbound loop: polls all healthy adapters for inbound messages,
//     routes them through CommHub's sieve into the target agent's inbox.
//
// Both loops are gracefully stoppable via context cancellation.
type Mailroom struct {
	postbox  *Postbox
	registry *AdapterRegistry
	hub      *commhub.CommHub
	bus      *nervous.EventBus
	logger   *slog.Logger
	config   MailroomConfig

	// retryCount tracks delivery attempt counts per mail ID (in-memory, best-effort).
	retryCount map[string]int
}

// NewMailroom creates a Mailroom wired to the Postbox, adapter registry, and CommHub.
func NewMailroom(
	postbox *Postbox,
	registry *AdapterRegistry,
	hub *commhub.CommHub,
	bus *nervous.EventBus,
	logger *slog.Logger,
	config MailroomConfig,
) *Mailroom {
	if config.PollInterval == 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.InboundPollInterval == 0 {
		config.InboundPollInterval = 5 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 50
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	return &Mailroom{
		postbox:    postbox,
		registry:   registry,
		hub:        hub,
		bus:        bus,
		logger:     logger,
		config:     config,
		retryCount: make(map[string]int),
	}
}

// Run starts both the outbound dispatch loop and the inbound poll loop.
// Blocks until the context is cancelled.
func (m *Mailroom) Run(ctx context.Context) {
	m.logger.Info("mailroom started",
		"poll_interval", m.config.PollInterval,
		"inbound_poll_interval", m.config.InboundPollInterval,
		"batch_size", m.config.BatchSize,
	)

	outboundTicker := time.NewTicker(m.config.PollInterval)
	inboundTicker := time.NewTicker(m.config.InboundPollInterval)
	defer outboundTicker.Stop()
	defer inboundTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("mailroom stopping")
			return

		case <-outboundTicker.C:
			m.dispatchOutbound(ctx)

		case <-inboundTicker.C:
			m.pollInbound(ctx)
		}
	}
}

// dispatchOutbound dequeues outbound messages and routes them to the
// appropriate adapter for delivery. Messages without a matching adapter
// are logged and skipped (they remain deleted from the queue — the adapter
// should be registered before messages are sent).
func (m *Mailroom) dispatchOutbound(ctx context.Context) {
	messages, err := m.postbox.DequeueOutbound(ctx, m.config.BatchSize)
	if err != nil {
		m.logger.Error("outbound dequeue failed", "error", err)
		return
	}

	if len(messages) == 0 {
		return
	}

	m.logger.Debug("dispatching outbound batch", "count", len(messages))

	for _, mail := range messages {
		m.deliverOutbound(ctx, mail)
	}
}

// deliverOutbound sends a single outbound mail via the target adapter.
// If delivery fails, the message is retried up to MaxRetries times
// before being quarantined to the dead letter office.
func (m *Mailroom) deliverOutbound(ctx context.Context, mail *types.AgentMail) {
	// Determine which adapter to use. Convention: the "To" field format is
	// "adapter:destination" (e.g. "webhook:https://example.com/hook" or
	// "slack:#channel"). If no adapter prefix, try all healthy adapters.
	adapterName, _ := parseAdapterTarget(mail.To)

	var adapter MessengerAdapter
	if adapterName != "" {
		adapter = m.registry.Get(adapterName)
	}

	if adapter == nil {
		// No specific adapter — try to send via any healthy adapter.
		for _, name := range m.registry.List() {
			a := m.registry.Get(name)
			if a != nil && a.Healthy() {
				adapter = a
				break
			}
		}
	}

	if adapter == nil {
		m.logger.Warn("no adapter available for outbound mail",
			"mail_id", mail.ID,
			"to", mail.To,
		)
		m.handleDeliveryFailure(ctx, mail, "no adapter available")
		return
	}

	if err := adapter.Send(ctx, mail); err != nil {
		m.logger.Warn("outbound delivery failed",
			"mail_id", mail.ID,
			"adapter", adapter.Name(),
			"error", err,
		)
		m.handleDeliveryFailure(ctx, mail, err.Error())
		return
	}

	m.logger.Debug("outbound mail delivered",
		"mail_id", mail.ID,
		"adapter", adapter.Name(),
	)

	// Clean up retry count on success.
	delete(m.retryCount, mail.ID)
}

// handleDeliveryFailure increments the retry counter and either re-enqueues
// the message or quarantines it to the dead letter office.
func (m *Mailroom) handleDeliveryFailure(ctx context.Context, mail *types.AgentMail, reason string) {
	m.retryCount[mail.ID]++
	attempts := m.retryCount[mail.ID]

	if attempts >= m.config.MaxRetries {
		// Dead-letter the message.
		entry := &types.DeadLetterEntry{
			ID:           fmt.Sprintf("dle-%d", time.Now().UnixNano()),
			MailID:       mail.ID,
			Reason:       reason,
			Attempts:     attempts,
			OriginalMail: mail,
		}
		if err := m.postbox.Quarantine(ctx, entry); err != nil {
			m.logger.Error("failed to quarantine mail", "mail_id", mail.ID, "error", err)
		}
		delete(m.retryCount, mail.ID)
		return
	}

	// Re-enqueue for retry.
	if err := m.postbox.SendOutbound(ctx, mail); err != nil {
		m.logger.Error("failed to re-enqueue mail for retry",
			"mail_id", mail.ID,
			"attempt", attempts,
			"error", err,
		)
	}
}

// pollInbound queries all healthy adapters for inbound messages and routes
// them through the CommHub into the target agent's inbox.
func (m *Mailroom) pollInbound(ctx context.Context) {
	for _, name := range m.registry.List() {
		adapter := m.registry.Get(name)
		if adapter == nil || !adapter.Healthy() {
			continue
		}

		messages, err := adapter.Receive(ctx)
		if err != nil {
			m.logger.Warn("inbound poll failed",
				"adapter", name,
				"error", err,
			)
			continue
		}

		for _, mail := range messages {
			m.routeInbound(ctx, mail)
		}
	}
}

// routeInbound enqueues an inbound message to the Postbox and dispatches
// it to the target agent via CommHub.
func (m *Mailroom) routeInbound(ctx context.Context, mail *types.AgentMail) {
	// Persist to the inbound queue for audit/replay.
	if err := m.postbox.ReceiveInbound(ctx, mail); err != nil {
		m.logger.Error("failed to enqueue inbound mail",
			"mail_id", mail.ID,
			"error", err,
		)
		return
	}

	// Route through CommHub with TrustExternal (external adapter messages).
	env := &types.MessageEnvelope{
		ID:          mail.ID,
		From:        mail.From,
		To:          mail.To,
		Trust:       types.TrustExternal,
		ContentType: "agentmail",
		Content:     string(mail.Payload),
		Metadata: map[string]string{
			"workspace_id": mail.WorkspaceID,
			"priority":     string(mail.Priority),
			"schema_id":    mail.SchemaID,
			"source":       "agentmail",
		},
		Timestamp: mail.SentAt.UnixMilli(),
	}

	if err := m.hub.Send(ctx, env); err != nil {
		m.logger.Warn("inbound routing through CommHub failed",
			"mail_id", mail.ID,
			"to", mail.To,
			"error", err,
		)
		return
	}

	m.logger.Debug("inbound mail routed to agent",
		"mail_id", mail.ID,
		"to", mail.To,
	)
}

// parseAdapterTarget splits a "adapter:target" string.
// Returns ("", original) if no adapter prefix is found.
func parseAdapterTarget(to string) (adapter, target string) {
	for i, c := range to {
		if c == ':' {
			return to[:i], to[i+1:]
		}
	}
	return "", to
}
