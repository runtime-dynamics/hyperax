package agentmail

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// PendingMail tracks a sent message awaiting acknowledgment.
type PendingMail struct {
	Mail     *types.AgentMail
	SentAt   time.Time
	Deadline time.Time
}

// AckTracker monitors sent mail for acknowledgment deadlines.
// When a deadline expires without acknowledgment, it fires a partition detection
// event on the EventBus and creates a PartitionLock for the affected workspace.
type AckTracker struct {
	mu      sync.RWMutex
	pending map[string]*PendingMail // keyed by mail ID
	acks    map[string]*types.MailAck
	locks   map[string]*types.PartitionLock // keyed by workspace ID

	bus    *nervous.EventBus
	logger *slog.Logger

	checkInterval time.Duration
}

// NewAckTracker creates an AckTracker wired to the given EventBus.
// The check interval determines how often the tracker scans for expired deadlines.
func NewAckTracker(bus *nervous.EventBus, logger *slog.Logger) *AckTracker {
	return &AckTracker{
		pending:       make(map[string]*PendingMail),
		acks:          make(map[string]*types.MailAck),
		locks:         make(map[string]*types.PartitionLock),
		bus:           bus,
		logger:        logger.With("component", "ack_tracker"),
		checkInterval: 5 * time.Second,
	}
}

// Track registers a sent mail for deadline monitoring.
// The ack deadline is computed from the mail's priority.
func (t *AckTracker) Track(mail *types.AgentMail) {
	if mail == nil {
		return
	}

	deadline := mail.AckDeadline
	if deadline == 0 {
		deadline = types.AckDeadlineFor(mail.Priority)
	}

	t.mu.Lock()
	t.pending[mail.ID] = &PendingMail{
		Mail:     mail,
		SentAt:   mail.SentAt,
		Deadline: mail.SentAt.Add(deadline),
	}
	t.mu.Unlock()

	t.logger.Debug("tracking mail ack deadline",
		"mail_id", mail.ID,
		"to", mail.To,
		"deadline", deadline,
	)
}

// Acknowledge records an ack for a tracked mail and removes it from pending.
// Returns an error if the mail ID is not being tracked.
func (t *AckTracker) Acknowledge(ack *types.MailAck) error {
	if ack == nil {
		return fmt.Errorf("ack must not be nil")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.pending[ack.MailID]; !ok {
		return fmt.Errorf("mail %q is not being tracked", ack.MailID)
	}

	delete(t.pending, ack.MailID)
	t.acks[ack.MailID] = ack

	t.logger.Debug("mail acknowledged",
		"mail_id", ack.MailID,
		"status", ack.Status,
	)

	if t.bus != nil {
		t.bus.Publish(nervous.NewEvent(
			types.EventAgentMailAck,
			"ack_tracker",
			"",
			ack,
		))
	}

	return nil
}

// Run starts the deadline check loop. It blocks until the context is cancelled.
func (t *AckTracker) Run(ctx context.Context) {
	ticker := time.NewTicker(t.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.checkDeadlines()
		}
	}
}

// checkDeadlines scans all pending mail and fires partition events for expired deadlines.
func (t *AckTracker) checkDeadlines() {
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	for id, pm := range t.pending {
		if now.After(pm.Deadline) {
			t.logger.Warn("ack deadline expired",
				"mail_id", id,
				"to", pm.Mail.To,
				"workspace_id", pm.Mail.WorkspaceID,
				"deadline", pm.Deadline,
			)

			// Create a partition lock for the affected workspace.
			lock := &types.PartitionLock{
				WorkspaceID: pm.Mail.WorkspaceID,
				InstanceID:  pm.Mail.To,
				Reason:      fmt.Sprintf("ack deadline expired for mail %s (priority: %s)", id, pm.Mail.Priority),
				LockedAt:    now,
				ExpiresAt:   now.Add(10 * time.Minute), // Default partition lock TTL.
			}
			t.locks[pm.Mail.WorkspaceID] = lock

			// Publish partition detection event.
			if t.bus != nil {
				t.bus.Publish(nervous.NewEvent(
					types.EventAgentMailPartitionDetected,
					"ack_tracker",
					pm.Mail.WorkspaceID,
					map[string]any{
						"mail_id":      id,
						"instance_id":  pm.Mail.To,
						"workspace_id": pm.Mail.WorkspaceID,
						"reason":       lock.Reason,
					},
				))
			}

			// Remove from pending after partition is raised.
			delete(t.pending, id)
		}
	}
}

// ResolvePartition removes a partition lock for the given workspace and publishes
// a resolution event. Returns an error if no lock exists for the workspace.
func (t *AckTracker) ResolvePartition(workspaceID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	lock, ok := t.locks[workspaceID]
	if !ok {
		return fmt.Errorf("no partition lock for workspace %q", workspaceID)
	}

	delete(t.locks, workspaceID)

	t.logger.Info("partition resolved",
		"workspace_id", workspaceID,
		"instance_id", lock.InstanceID,
	)

	if t.bus != nil {
		t.bus.Publish(nervous.NewEvent(
			types.EventAgentMailPartitionResolved,
			"ack_tracker",
			workspaceID,
			map[string]string{
				"workspace_id": workspaceID,
				"instance_id":  lock.InstanceID,
			},
		))
	}

	return nil
}

// GetPartitionLock returns the active partition lock for a workspace, or nil.
func (t *AckTracker) GetPartitionLock(workspaceID string) *types.PartitionLock {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.locks[workspaceID]
}

// GetPartitionStatus returns all active partition locks.
func (t *AckTracker) GetPartitionStatus() map[string]*types.PartitionLock {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]*types.PartitionLock, len(t.locks))
	for k, v := range t.locks {
		result[k] = v
	}
	return result
}

// PendingCount returns the number of mail messages awaiting acknowledgment.
func (t *AckTracker) PendingCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.pending)
}

// PendingMails returns a snapshot of all pending mail entries.
func (t *AckTracker) PendingMails() []PendingMail {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]PendingMail, 0, len(t.pending))
	for _, pm := range t.pending {
		result = append(result, *pm)
	}
	return result
}

// AckStatus returns the ack record for a given mail ID, or nil if not acknowledged.
func (t *AckTracker) AckStatus(mailID string) *types.MailAck {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.acks[mailID]
}

// Stats returns a JSON-serializable summary of the tracker state.
func (t *AckTracker) Stats() json.RawMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := map[string]any{
		"pending_count":    len(t.pending),
		"ack_count":        len(t.acks),
		"partition_count":  len(t.locks),
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.logger.Error("failed to marshal ack tracker stats", "error", err)
		return json.RawMessage(`{}`)
	}
	return data
}
