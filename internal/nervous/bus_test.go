package nervous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus(16)

	sub := bus.Subscribe("test-sub", nil)
	defer bus.Unsubscribe("test-sub")

	event := NewEvent(types.EventMCPRequest, "test", "global", nil)
	bus.Publish(event)

	select {
	case received := <-sub.Ch:
		if received.Type != types.EventMCPRequest {
			t.Errorf("got type %s, want %s", received.Type, types.EventMCPRequest)
		}
		if received.SequenceID == 0 {
			t.Error("SequenceID should be > 0 after publish")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBus_SubscribeTypes(t *testing.T) {
	bus := NewEventBus(16)

	sub := bus.SubscribeTypes("typed-sub", types.EventPipelineStart, types.EventPipelineComplete)
	defer bus.Unsubscribe("typed-sub")

	// Should receive pipeline events
	bus.Publish(NewEvent(types.EventPipelineStart, "test", "global", nil))
	// Should NOT receive MCP events
	bus.Publish(NewEvent(types.EventMCPRequest, "test", "global", nil))
	// Should receive pipeline complete
	bus.Publish(NewEvent(types.EventPipelineComplete, "test", "global", nil))

	received := drainEvents(sub.Ch, 2, time.Second)
	if len(received) != 2 {
		t.Fatalf("got %d events, want 2", len(received))
	}
	if received[0].Type != types.EventPipelineStart {
		t.Errorf("event 0: got %s, want %s", received[0].Type, types.EventPipelineStart)
	}
	if received[1].Type != types.EventPipelineComplete {
		t.Errorf("event 1: got %s, want %s", received[1].Type, types.EventPipelineComplete)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus(16)

	sub := bus.Subscribe("unsub-test", nil)
	bus.Unsubscribe("unsub-test")

	// Channel should be closed
	_, ok := <-sub.Ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}

	if bus.SubscriberCount() != 0 {
		t.Errorf("subscriber count = %d, want 0", bus.SubscriberCount())
	}
}

func TestEventBus_LamportClock(t *testing.T) {
	bus := NewEventBus(16)
	sub := bus.Subscribe("lamport-sub", nil)
	defer bus.Unsubscribe("lamport-sub")

	bus.Publish(NewEvent(types.EventMCPRequest, "a", "global", nil))
	bus.Publish(NewEvent(types.EventMCPResponse, "b", "global", nil))

	events := drainEvents(sub.Ch, 2, time.Second)
	if len(events) < 2 {
		t.Fatal("expected 2 events")
	}
	if events[0].SequenceID >= events[1].SequenceID {
		t.Errorf("Lamport clock should increase: %d >= %d", events[0].SequenceID, events[1].SequenceID)
	}
}

func TestEventBus_Merge(t *testing.T) {
	bus := NewEventBus(16)

	// Publish a local event to set clock to 1
	bus.Publish(NewEvent(types.EventMCPRequest, "local", "global", nil))

	// Merge with a remote clock of 100
	result := bus.Merge(100)
	if result != 101 {
		t.Errorf("merge result = %d, want 101", result)
	}

	// Next publish should be > 101
	sub := bus.Subscribe("post-merge", nil)
	defer bus.Unsubscribe("post-merge")

	bus.Publish(NewEvent(types.EventMCPRequest, "local", "global", nil))
	event := <-sub.Ch
	if event.SequenceID <= 101 {
		t.Errorf("post-merge sequence = %d, should be > 101", event.SequenceID)
	}
}

func TestEventBus_Backpressure(t *testing.T) {
	bus := NewEventBus(1) // tiny buffer

	sub := bus.Subscribe("slow-sub", nil)
	defer bus.Unsubscribe("slow-sub")

	// Fill the buffer
	bus.Publish(NewEvent(types.EventMCPRequest, "test", "global", nil))
	// This should be dropped (non-blocking)
	bus.Publish(NewEvent(types.EventMCPResponse, "test", "global", nil))

	select {
	case e := <-sub.Ch:
		if e.Type != types.EventMCPRequest {
			t.Errorf("expected first event, got %s", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus(1024)
	sub := bus.Subscribe("concurrent-sub", nil)
	defer bus.Unsubscribe("concurrent-sub")

	const goroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				bus.Publish(NewEvent(types.EventMCPRequest, "concurrent", "global", nil))
			}
		}()
	}

	wg.Wait()

	total := goroutines * eventsPerGoroutine
	received := drainEvents(sub.Ch, total, 2*time.Second)
	if len(received) != total {
		t.Errorf("got %d events, want %d", len(received), total)
	}

	// Verify all sequence IDs are unique
	seen := make(map[uint64]bool, len(received))
	for _, e := range received {
		if seen[e.SequenceID] {
			t.Errorf("duplicate sequence ID: %d", e.SequenceID)
		}
		seen[e.SequenceID] = true
	}
}

func TestEventBus_Run(t *testing.T) {
	bus := NewEventBus(16)
	_ = bus.Subscribe("run-sub", nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		bus.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	if bus.SubscriberCount() != 0 {
		t.Errorf("subscribers not cleaned up: %d", bus.SubscriberCount())
	}
}

func TestDriftGuard_NoDrift(t *testing.T) {
	bus := NewEventBus(16)
	guard := NewDriftGuard(bus, DefaultDriftThreshold)

	now := time.Now()
	e1 := types.NervousEvent{Type: types.EventMCPRequest, SequenceID: 1, Timestamp: now}
	e2 := types.NervousEvent{Type: types.EventMCPResponse, SequenceID: 2, Timestamp: now.Add(100 * time.Millisecond)}

	if guard.Check(e1) {
		t.Error("first event should not trigger drift")
	}
	if guard.Check(e2) {
		t.Error("ordered events should not trigger drift")
	}
}

func TestDriftGuard_DetectsDrift(t *testing.T) {
	bus := NewEventBus(16)
	sub := bus.SubscribeTypes("drift-watcher", types.EventNervousDriftDetected)
	defer bus.Unsubscribe("drift-watcher")

	guard := NewDriftGuard(bus, DefaultDriftThreshold)

	now := time.Now()
	// Lamport says e1 < e2, but wall-clock says e1 is 10s AFTER e2
	e1 := types.NervousEvent{Type: types.EventMCPRequest, SequenceID: 1, Timestamp: now.Add(10 * time.Second)}
	e2 := types.NervousEvent{Type: types.EventMCPResponse, SequenceID: 2, Timestamp: now}

	guard.Check(e1)
	drifted := guard.Check(e2)
	if !drifted {
		t.Error("expected drift detection")
	}

	select {
	case event := <-sub.Ch:
		if event.Type != types.EventNervousDriftDetected {
			t.Errorf("expected drift event, got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for drift event")
	}
}

func TestNewEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventType  types.EventType
		source     string
		scope      string
		payload    any
		wantNilPay bool
	}{
		{
			name:       "with nil payload",
			eventType:  types.EventMCPRequest,
			source:     "test",
			scope:      "global",
			payload:    nil,
			wantNilPay: true,
		},
		{
			name:      "with map payload",
			eventType: types.EventPipelineStart,
			source:    "pipeline",
			scope:     "workspace-1",
			payload:   map[string]string{"name": "build"},
		},
		{
			name:      "with string payload",
			eventType: types.EventCommMessage,
			source:    "comm",
			scope:     "agent-1",
			payload:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewEvent(tt.eventType, tt.source, tt.scope, tt.payload)

			if event.Type != tt.eventType {
				t.Errorf("type = %s, want %s", event.Type, tt.eventType)
			}
			if event.Source != tt.source {
				t.Errorf("source = %s, want %s", event.Source, tt.source)
			}
			if event.Scope != tt.scope {
				t.Errorf("scope = %s, want %s", event.Scope, tt.scope)
			}
			if tt.wantNilPay && event.Payload != nil {
				t.Errorf("payload should be nil, got %s", string(event.Payload))
			}
			if !tt.wantNilPay && event.Payload == nil {
				t.Error("payload should not be nil")
			}
			if event.Timestamp.IsZero() {
				t.Error("timestamp should not be zero")
			}
			if event.SequenceID != 0 {
				t.Error("SequenceID should be 0 before publish")
			}
		})
	}
}

// drainEvents reads up to n events from ch within the timeout.
func drainEvents(ch <-chan types.NervousEvent, n int, timeout time.Duration) []types.NervousEvent {
	var events []types.NervousEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for i := 0; i < n; i++ {
		select {
		case e, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, e)
		case <-timer.C:
			return events
		}
	}
	return events
}
