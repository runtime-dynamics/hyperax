package agentmail

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestAckTracker_TrackAndAcknowledge(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())

	mail := &types.AgentMail{
		ID:          "mail-001",
		From:        "a",
		To:          "b",
		WorkspaceID: "ws-1",
		Priority:    types.MailPriorityStandard,
		Payload:     json.RawMessage(`{}`),
		SentAt:      time.Now(),
	}

	tracker.Track(mail)

	if tracker.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, want 1", tracker.PendingCount())
	}

	ack := &types.MailAck{
		MailID:     "mail-001",
		InstanceID: "b",
		AckedAt:    time.Now(),
		Status:     "received",
	}

	if err := tracker.Acknowledge(ack); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	if tracker.PendingCount() != 0 {
		t.Fatalf("PendingCount = %d, want 0 after ack", tracker.PendingCount())
	}

	stored := tracker.AckStatus("mail-001")
	if stored == nil {
		t.Fatal("expected ack to be stored")
	}
	if stored.Status != "received" {
		t.Errorf("ack status = %q, want %q", stored.Status, "received")
	}
}

func TestAckTracker_AcknowledgeUntracked(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())

	ack := &types.MailAck{MailID: "unknown-id"}
	if err := tracker.Acknowledge(ack); err == nil {
		t.Fatal("expected error for untracked mail ack")
	}
}

func TestAckTracker_DeadlineExpiry(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-partition", types.EventAgentMailPartitionDetected)

	tracker := NewAckTracker(bus, testLogger())
	tracker.checkInterval = 10 * time.Millisecond

	// Create mail with an already-expired deadline.
	mail := &types.AgentMail{
		ID:          "mail-expired",
		From:        "a",
		To:          "b",
		WorkspaceID: "ws-test",
		Priority:    types.MailPriorityUrgent,
		Payload:     json.RawMessage(`{}`),
		SentAt:      time.Now().Add(-1 * time.Minute), // well in the past
		AckDeadline: 1 * time.Second,
	}

	tracker.Track(mail)

	// Run the check manually.
	tracker.checkDeadlines()

	if tracker.PendingCount() != 0 {
		t.Fatalf("PendingCount = %d, want 0 after deadline expiry", tracker.PendingCount())
	}

	// Verify partition lock was created.
	lock := tracker.GetPartitionLock("ws-test")
	if lock == nil {
		t.Fatal("expected partition lock for ws-test")
	}
	if lock.InstanceID != "b" {
		t.Errorf("lock instance = %q, want %q", lock.InstanceID, "b")
	}

	// Verify event was published.
	select {
	case evt := <-sub.Ch:
		if evt.Type != types.EventAgentMailPartitionDetected {
			t.Errorf("event type = %q, want %q", evt.Type, types.EventAgentMailPartitionDetected)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected partition event within 100ms")
	}
}

func TestAckTracker_ResolvePartition(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-resolve", types.EventAgentMailPartitionResolved)

	tracker := NewAckTracker(bus, testLogger())

	// Manually create a partition.
	tracker.mu.Lock()
	tracker.locks["ws-1"] = &types.PartitionLock{
		WorkspaceID: "ws-1",
		InstanceID:  "b",
	}
	tracker.mu.Unlock()

	if err := tracker.ResolvePartition("ws-1"); err != nil {
		t.Fatalf("ResolvePartition: %v", err)
	}

	if lock := tracker.GetPartitionLock("ws-1"); lock != nil {
		t.Fatal("partition lock should be removed after resolution")
	}

	select {
	case evt := <-sub.Ch:
		if evt.Type != types.EventAgentMailPartitionResolved {
			t.Errorf("event type = %q, want %q", evt.Type, types.EventAgentMailPartitionResolved)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected partition resolved event")
	}
}

func TestAckTracker_ResolvePartitionNotFound(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())

	if err := tracker.ResolvePartition("nonexistent"); err == nil {
		t.Fatal("expected error for non-existent partition")
	}
}

func TestAckTracker_RunContext(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())
	tracker.checkInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Run should exit when context is cancelled.
	tracker.Run(ctx)
}

func TestAckTracker_PendingMails(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())

	mail := &types.AgentMail{
		ID:     "mail-pending",
		SentAt: time.Now(),
	}
	tracker.Track(mail)

	pending := tracker.PendingMails()
	if len(pending) != 1 {
		t.Fatalf("PendingMails len = %d, want 1", len(pending))
	}
	if pending[0].Mail.ID != "mail-pending" {
		t.Errorf("pending mail ID = %q, want %q", pending[0].Mail.ID, "mail-pending")
	}
}

func TestAckTracker_Stats(t *testing.T) {
	bus := nervous.NewEventBus(64)
	tracker := NewAckTracker(bus, testLogger())

	stats := tracker.Stats()
	var m map[string]any
	if err := json.Unmarshal(stats, &m); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if m["pending_count"].(float64) != 0 {
		t.Errorf("expected 0 pending")
	}
}
