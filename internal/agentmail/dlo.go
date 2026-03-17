package agentmail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// DefaultMaxRetries is the maximum number of retry attempts before permanent discard.
	DefaultMaxRetries = 3

	// DefaultRetryDelay is the base delay between retry attempts (exponential backoff applied).
	DefaultRetryDelay = 30 * time.Second
)

// DeadLetterOffice quarantines failed mail messages for inspection, retry, or discard.
// It publishes DLO events on the EventBus for observability.
type DeadLetterOffice struct {
	mu      sync.RWMutex
	entries map[string]*types.DeadLetterEntry // keyed by entry ID

	bus    *nervous.EventBus
	logger *slog.Logger

	maxRetries int
	retryDelay time.Duration

	// retrySend is the function used to retry sending a quarantined message.
	// It is set externally (typically by the Postbox) to avoid circular dependencies.
	retrySend func(ctx context.Context, mail *types.AgentMail) error
}

// DLOOption configures the DeadLetterOffice.
type DLOOption func(*DeadLetterOffice)

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(n int) DLOOption {
	return func(d *DeadLetterOffice) {
		if n > 0 {
			d.maxRetries = n
		}
	}
}

// WithRetryDelay sets the base retry delay.
func WithRetryDelay(delay time.Duration) DLOOption {
	return func(d *DeadLetterOffice) {
		if delay > 0 {
			d.retryDelay = delay
		}
	}
}

// NewDeadLetterOffice creates a DLO wired to the EventBus.
func NewDeadLetterOffice(bus *nervous.EventBus, logger *slog.Logger, opts ...DLOOption) *DeadLetterOffice {
	dlo := &DeadLetterOffice{
		entries:    make(map[string]*types.DeadLetterEntry),
		bus:        bus,
		logger:     logger.With("component", "dlo"),
		maxRetries: DefaultMaxRetries,
		retryDelay: DefaultRetryDelay,
	}
	for _, opt := range opts {
		opt(dlo)
	}
	return dlo
}

// SetRetrySend configures the function used to retry sending quarantined messages.
func (d *DeadLetterOffice) SetRetrySend(fn func(ctx context.Context, mail *types.AgentMail) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retrySend = fn
}

// Quarantine adds a failed mail message to the DLO with the given reason.
// Returns the DLO entry ID.
func (d *DeadLetterOffice) Quarantine(mail *types.AgentMail, reason string) string {
	id := generateDLOID()

	entry := &types.DeadLetterEntry{
		ID:            id,
		MailID:        mail.ID,
		Reason:        reason,
		Attempts:      0,
		QuarantinedAt: time.Now(),
		OriginalMail:  mail,
	}

	d.mu.Lock()
	d.entries[id] = entry
	d.mu.Unlock()

	d.logger.Warn("mail quarantined in DLO",
		"entry_id", id,
		"mail_id", mail.ID,
		"from", mail.From,
		"to", mail.To,
		"reason", reason,
	)

	if d.bus != nil {
		d.bus.Publish(nervous.NewEvent(
			types.EventAgentMailDLOQuarantined,
			"dlo",
			mail.WorkspaceID,
			map[string]any{
				"entry_id": id,
				"mail_id":  mail.ID,
				"reason":   reason,
			},
		))
	}

	return id
}

// Retry attempts to resend a quarantined message.
// Returns an error if the entry is not found, max retries are exceeded,
// or the retry send function is not configured.
func (d *DeadLetterOffice) Retry(ctx context.Context, entryID string) error {
	d.mu.Lock()
	entry, ok := d.entries[entryID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("DLO entry %q not found", entryID)
	}

	if entry.Attempts >= d.maxRetries {
		d.mu.Unlock()
		return fmt.Errorf("max retries (%d) exceeded for entry %q", d.maxRetries, entryID)
	}

	if d.retrySend == nil {
		d.mu.Unlock()
		return fmt.Errorf("retry send function not configured")
	}

	entry.Attempts++
	mail := entry.OriginalMail
	retrySend := d.retrySend
	d.mu.Unlock()

	d.logger.Info("retrying DLO entry",
		"entry_id", entryID,
		"mail_id", mail.ID,
		"attempt", entry.Attempts,
	)

	if err := retrySend(ctx, mail); err != nil {
		return fmt.Errorf("agentmail.DeadLetterOffice.Retry: attempt %d: %w", entry.Attempts, err)
	}


	// Successful retry: remove from DLO.
	d.mu.Lock()
	delete(d.entries, entryID)
	d.mu.Unlock()

	d.logger.Info("DLO entry retried successfully",
		"entry_id", entryID,
		"mail_id", mail.ID,
	)

	return nil
}

// Discard permanently removes a quarantined entry from the DLO.
// Publishes an audit event for traceability.
func (d *DeadLetterOffice) Discard(entryID string) error {
	d.mu.Lock()
	entry, ok := d.entries[entryID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("DLO entry %q not found", entryID)
	}
	delete(d.entries, entryID)
	d.mu.Unlock()

	d.logger.Info("DLO entry discarded",
		"entry_id", entryID,
		"mail_id", entry.MailID,
	)

	if d.bus != nil {
		d.bus.Publish(nervous.NewEvent(
			types.EventAgentMailDLOAudit,
			"dlo",
			"",
			map[string]any{
				"action":   "discard",
				"entry_id": entryID,
				"mail_id":  entry.MailID,
				"reason":   entry.Reason,
				"attempts": entry.Attempts,
			},
		))
	}

	return nil
}

// Get returns a DLO entry by ID, or nil if not found.
func (d *DeadLetterOffice) Get(entryID string) *types.DeadLetterEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.entries[entryID]
}

// List returns all quarantined entries.
func (d *DeadLetterOffice) List() []*types.DeadLetterEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*types.DeadLetterEntry, 0, len(d.entries))
	for _, entry := range d.entries {
		result = append(result, entry)
	}
	return result
}

// Count returns the number of quarantined entries.
func (d *DeadLetterOffice) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.entries)
}

// generateDLOID creates a short random ID for DLO entries.
// Falls back to a timestamp-based ID if crypto/rand fails.
func generateDLOID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand is unavailable.
		return fmt.Sprintf("dlo-%d", time.Now().UnixNano())
	}
	return "dlo-" + hex.EncodeToString(b)
}
