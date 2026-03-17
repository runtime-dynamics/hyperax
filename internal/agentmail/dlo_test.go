package agentmail

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func newTestMailForDLO(id string) *types.AgentMail {
	return &types.AgentMail{
		ID:          id,
		From:        "sender",
		To:          "receiver",
		WorkspaceID: "ws-dlo",
		Priority:    types.MailPriorityStandard,
		Payload:     json.RawMessage(`{"test":true}`),
		SentAt:      time.Now(),
	}
}

func TestDLO_QuarantineAndList(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("dlo-test", types.EventAgentMailDLOQuarantined)

	dlo := NewDeadLetterOffice(bus, testLogger())

	mail := newTestMailForDLO("mail-fail-1")
	entryID := dlo.Quarantine(mail, "adapter timeout")

	if entryID == "" {
		t.Fatal("expected non-empty entry ID")
	}

	if dlo.Count() != 1 {
		t.Fatalf("Count = %d, want 1", dlo.Count())
	}

	entries := dlo.List()
	if len(entries) != 1 {
		t.Fatalf("List len = %d, want 1", len(entries))
	}
	if entries[0].Reason != "adapter timeout" {
		t.Errorf("reason = %q, want %q", entries[0].Reason, "adapter timeout")
	}

	// Verify quarantine event was published.
	select {
	case evt := <-sub.Ch:
		if evt.Type != types.EventAgentMailDLOQuarantined {
			t.Errorf("event type = %q, want %q", evt.Type, types.EventAgentMailDLOQuarantined)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected DLO quarantine event")
	}
}

func TestDLO_Get(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	mail := newTestMailForDLO("mail-get-1")
	entryID := dlo.Quarantine(mail, "test")

	entry := dlo.Get(entryID)
	if entry == nil {
		t.Fatal("expected entry from Get")
	}
	if entry.MailID != "mail-get-1" {
		t.Errorf("MailID = %q, want %q", entry.MailID, "mail-get-1")
	}

	if dlo.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent entry")
	}
}

func TestDLO_RetrySuccess(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	mail := newTestMailForDLO("mail-retry-1")
	entryID := dlo.Quarantine(mail, "transient error")

	// Configure a successful retry function.
	dlo.SetRetrySend(func(ctx context.Context, m *types.AgentMail) error {
		return nil
	})

	if err := dlo.Retry(context.Background(), entryID); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Entry should be removed after successful retry.
	if dlo.Count() != 0 {
		t.Fatalf("Count = %d, want 0 after successful retry", dlo.Count())
	}
}

func TestDLO_RetryFailure(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	mail := newTestMailForDLO("mail-retry-fail")
	entryID := dlo.Quarantine(mail, "error")

	dlo.SetRetrySend(func(ctx context.Context, m *types.AgentMail) error {
		return fmt.Errorf("still failing")
	})

	if err := dlo.Retry(context.Background(), entryID); err == nil {
		t.Fatal("expected error from failed retry")
	}

	// Entry should remain after failed retry.
	if dlo.Count() != 1 {
		t.Fatalf("Count = %d, want 1 after failed retry", dlo.Count())
	}

	// Attempt count should have incremented.
	entry := dlo.Get(entryID)
	if entry.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", entry.Attempts)
	}
}

func TestDLO_RetryMaxExceeded(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger(), WithMaxRetries(2))

	mail := newTestMailForDLO("mail-max-retry")
	entryID := dlo.Quarantine(mail, "error")

	dlo.SetRetrySend(func(ctx context.Context, m *types.AgentMail) error {
		return fmt.Errorf("fail")
	})

	// Exhaust retries (errors expected since retry func always fails).
	if err := dlo.Retry(context.Background(), entryID); err == nil {
		t.Fatal("expected error from retry 1")
	}
	if err := dlo.Retry(context.Background(), entryID); err == nil {
		t.Fatal("expected error from retry 2")
	}

	// Third attempt should be rejected.
	if err := dlo.Retry(context.Background(), entryID); err == nil {
		t.Fatal("expected max retries error")
	}
}

func TestDLO_RetryNoSendFunc(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	mail := newTestMailForDLO("mail-no-func")
	entryID := dlo.Quarantine(mail, "error")

	if err := dlo.Retry(context.Background(), entryID); err == nil {
		t.Fatal("expected error when retrySend is not configured")
	}
}

func TestDLO_RetryNotFound(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	if err := dlo.Retry(context.Background(), "nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

func TestDLO_Discard(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("dlo-discard", types.EventAgentMailDLOAudit)

	dlo := NewDeadLetterOffice(bus, testLogger())

	mail := newTestMailForDLO("mail-discard-1")
	entryID := dlo.Quarantine(mail, "permanent failure")

	// Drain quarantine event.
	<-time.After(10 * time.Millisecond)

	if err := dlo.Discard(entryID); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if dlo.Count() != 0 {
		t.Fatalf("Count = %d, want 0 after discard", dlo.Count())
	}

	// Verify audit event.
	select {
	case evt := <-sub.Ch:
		if evt.Type != types.EventAgentMailDLOAudit {
			t.Errorf("event type = %q, want %q", evt.Type, types.EventAgentMailDLOAudit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected DLO audit event")
	}
}

func TestDLO_DiscardNotFound(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger())

	if err := dlo.Discard("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

func TestDLO_Options(t *testing.T) {
	dlo := NewDeadLetterOffice(nil, testLogger(),
		WithMaxRetries(10),
		WithRetryDelay(1*time.Minute),
	)

	if dlo.maxRetries != 10 {
		t.Errorf("maxRetries = %d, want 10", dlo.maxRetries)
	}
	if dlo.retryDelay != 1*time.Minute {
		t.Errorf("retryDelay = %v, want 1m", dlo.retryDelay)
	}
}
