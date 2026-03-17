package pulse

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// testLogger returns a silent logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// collectEvents subscribes to the bus and collects events of the given types
// into a slice. Returns a function to retrieve collected events and a cancel
// function to stop collection.
func collectEvents(bus *nervous.EventBus, eventTypes ...types.EventType) (get func() []types.NervousEvent, cancel func()) {
	sub := bus.SubscribeTypes("test-collector", eventTypes...)
	var mu sync.Mutex
	var events []types.NervousEvent

	ctx, stop := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.Ch:
				if !ok {
					return
				}
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			}
		}
	}()

	return func() []types.NervousEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]types.NervousEvent, len(events))
		copy(out, events)
		return out
	}, func() {
		stop()
		bus.Unsubscribe("test-collector")
	}
}

// --- CRUD Tests ---

func TestCreateCadence_Success(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadence("heartbeat", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.Name != "heartbeat" {
		t.Errorf("expected name 'heartbeat', got %q", c.Name)
	}
	if c.Schedule != "@every 10s" {
		t.Errorf("expected schedule '@every 10s', got %q", c.Schedule)
	}
	if c.Priority != types.PriorityStandard {
		t.Errorf("expected priority 'standard', got %q", c.Priority)
	}
	if !c.Enabled {
		t.Error("expected cadence to be enabled")
	}
	if c.NextFire == nil {
		t.Fatal("expected NextFire to be set")
	}
}

func TestCreateCadence_InvalidSchedule(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.CreateCadence("bad", "not-a-cron", types.PriorityStandard, nil)
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

func TestCreateCadence_EmptyName(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.CreateCadence("", "@every 5s", types.PriorityStandard, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateCadence_InvalidPriority(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.CreateCadence("test", "@every 5s", "invalid", nil)
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
}

func TestGetCadence(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadence("test", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "test" {
		t.Errorf("expected name 'test', got %q", got.Name)
	}

	// Verify it's a copy by modifying it.
	got.Name = "modified"
	original, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("GetCadence: %v", err)
	}
	if original.Name != "test" {
		t.Error("GetCadence should return a copy, not a reference")
	}
}

func TestGetCadence_NotFound(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.GetCadence("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent cadence")
	}
}

func TestUpdateCadence(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadence("original", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = engine.UpdateCadence(c.ID, "updated", "@every 30s", types.PriorityUrgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("GetCadence: %v", err)
	}
	if got.Name != "updated" {
		t.Errorf("expected name 'updated', got %q", got.Name)
	}
	if got.Schedule != "@every 30s" {
		t.Errorf("expected schedule '@every 30s', got %q", got.Schedule)
	}
	if got.Priority != types.PriorityUrgent {
		t.Errorf("expected priority 'urgent', got %q", got.Priority)
	}
}

func TestUpdateCadence_PartialUpdate(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadence("original", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Update only the name, leave schedule and priority empty.
	err = engine.UpdateCadence(c.ID, "renamed", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("GetCadence: %v", err)
	}
	if got.Name != "renamed" {
		t.Errorf("expected name 'renamed', got %q", got.Name)
	}
	if got.Schedule != "@every 10s" {
		t.Errorf("expected schedule unchanged, got %q", got.Schedule)
	}
	if got.Priority != types.PriorityStandard {
		t.Errorf("expected priority unchanged, got %q", got.Priority)
	}
}

func TestUpdateCadence_NotFound(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	err := engine.UpdateCadence("nonexistent", "name", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent cadence")
	}
}

func TestDeleteCadence(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadence("doomed", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	err = engine.DeleteCadence(c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = engine.GetCadence(c.ID)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestDeleteCadence_NotFound(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	err := engine.DeleteCadence("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent cadence")
	}
}

func TestListCadences(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	// Empty list.
	cadences := engine.ListCadences()
	if len(cadences) != 0 {
		t.Errorf("expected 0 cadences, got %d", len(cadences))
	}

	// Add two cadences.
	if _, err := engine.CreateCadence("a", "@every 10s", types.PriorityStandard, nil); err != nil {
		t.Fatalf("CreateCadence a: %v", err)
	}
	if _, err := engine.CreateCadence("b", "@every 20s", types.PriorityBackground, nil); err != nil {
		t.Fatalf("CreateCadence b: %v", err)
	}

	cadences = engine.ListCadences()
	if len(cadences) != 2 {
		t.Errorf("expected 2 cadences, got %d", len(cadences))
	}
}

// --- Fire Scheduling Tests ---

func TestProcessTick_FiresDueCadence(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	// Use a fixed time so we can control NextFire.
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	c, err := engine.CreateCadence("tick-test", "@every 5s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Collect pulse.fire events.
	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	// Advance time past the NextFire.
	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	// Give the subscriber goroutine a moment to receive.
	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 fire event, got %d", len(events))
	}

	// Verify the event payload contains the cadence ID.
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["cadence_id"] != c.ID {
		t.Errorf("expected cadence_id %q, got %q", c.ID, payload["cadence_id"])
	}
}

func TestProcessTick_SkipsNotYetDue(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if _, err := engine.CreateCadence("future", "@every 1h", types.PriorityStandard, nil); err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	// Advance only 30 seconds — cadence is due in 1 hour.
	engine.nowFunc = func() time.Time { return now.Add(30 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 0 {
		t.Errorf("expected 0 fire events, got %d", len(events))
	}
}

func TestProcessTick_UpdatesNextFire(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	c, err := engine.CreateCadence("recur", "@every 10s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}
	originalNext := *c.NextFire

	// Advance past the first fire.
	engine.nowFunc = func() time.Time { return now.Add(15 * time.Second) }
	engine.processTick()

	got, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("GetCadence: %v", err)
	}
	if got.NextFire == nil {
		t.Fatal("expected NextFire to be set after fire")
	}
	if !got.NextFire.After(originalNext) {
		t.Errorf("expected NextFire to advance; original=%v, new=%v", originalNext, *got.NextFire)
	}
}

// --- Singleflight Tests ---

func TestProcessTick_SingleflightSkipsRunning(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	c, err := engine.CreateCadence("singleflight", "@every 5s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Manually mark as running to simulate an in-flight invocation.
	engine.mu.Lock()
	engine.cadences[c.ID].Running = true
	engine.mu.Unlock()

	get, cancel := collectEvents(bus, types.EventPulseSkipped)
	defer cancel()

	// Advance past NextFire.
	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 skipped event, got %d", len(events))
	}

	var payload map[string]string
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["reason"] == "" {
		t.Error("expected non-empty reason in skipped event payload")
	}
}

// --- Backpressure Tests ---

func TestProcessTick_BackpressureDefersBackground(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if _, err := engine.CreateCadence("bg-task", "@every 5s", types.PriorityBackground, nil); err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Activate backpressure.
	engine.Backpressure().SetBackpressure(true)

	getFire, cancelFire := collectEvents(bus, types.EventPulseFire)
	defer cancelFire()
	getBP, cancelBP := collectEvents(bus, types.EventPulseBackpressure)
	defer cancelBP()

	// Advance past NextFire.
	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	fires := getFire()
	if len(fires) != 0 {
		t.Errorf("expected 0 fire events under backpressure, got %d", len(fires))
	}

	bps := getBP()
	if len(bps) != 1 {
		t.Fatalf("expected 1 backpressure event, got %d", len(bps))
	}
}

func TestProcessTick_BackpressureDoesNotDeferUrgent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if _, err := engine.CreateCadence("urgent-task", "@every 5s", types.PriorityUrgent, nil); err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Activate backpressure.
	engine.Backpressure().SetBackpressure(true)

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Errorf("expected 1 fire event for urgent cadence under backpressure, got %d", len(events))
	}
}

func TestProcessTick_BackpressureDoesNotDeferStandard(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if _, err := engine.CreateCadence("std-task", "@every 5s", types.PriorityStandard, nil); err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	engine.Backpressure().SetBackpressure(true)

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Errorf("expected 1 fire event for standard cadence under backpressure, got %d", len(events))
	}
}

// --- FireEvent Tests ---

func TestFireEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	if err := engine.FireEvent(types.EventPulseFire, "test-source", map[string]string{"key": "value"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Source != "test-source" {
		t.Errorf("expected source 'test-source', got %q", events[0].Source)
	}
}

// --- GetStatus Tests ---

func TestGetStatus(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	if _, err := engine.CreateCadence("a", "@every 10s", types.PriorityStandard, nil); err != nil {
		t.Fatalf("CreateCadence a: %v", err)
	}
	if _, err := engine.CreateCadence("b", "@every 20s", types.PriorityBackground, nil); err != nil {
		t.Fatalf("CreateCadence b: %v", err)
	}

	status := engine.GetStatus()

	if status["total_cadences"] != 2 {
		t.Errorf("expected total_cadences=2, got %v", status["total_cadences"])
	}
	if status["enabled_cadences"] != 2 {
		t.Errorf("expected enabled_cadences=2, got %v", status["enabled_cadences"])
	}
	if status["backpressure"] != false {
		t.Errorf("expected backpressure=false, got %v", status["backpressure"])
	}
}

// --- BackpressureMonitor Tests ---

func TestBackpressureMonitor(t *testing.T) {
	m := NewBackpressureMonitor(50)

	if m.Check() {
		t.Error("expected no backpressure initially")
	}
	if m.Threshold() != 50 {
		t.Errorf("expected threshold 50, got %d", m.Threshold())
	}

	m.SetBackpressure(true)
	if !m.Check() {
		t.Error("expected backpressure after SetBackpressure(true)")
	}

	m.SetBackpressure(false)
	if m.Check() {
		t.Error("expected no backpressure after SetBackpressure(false)")
	}
}

func TestBackpressureMonitor_DefaultThreshold(t *testing.T) {
	m := NewBackpressureMonitor(0)
	if m.Threshold() != 100 {
		t.Errorf("expected default threshold 100, got %d", m.Threshold())
	}
}

// --- Run Loop Tests ---

func TestRun_StopsOnCancel(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())
	engine.tick = 10 * time.Millisecond // Fast tick for tests.

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(done)
	}()

	// Let it tick a couple times.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — Run returned.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestRun_FiresCadencesOnSchedule(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())
	engine.tick = 10 * time.Millisecond

	// Set nowFunc to a fixed time for cadence creation.
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	if _, err := engine.CreateCadence("fast", "@every 1s", types.PriorityStandard, nil); err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Now advance time so the cadence is immediately due on every tick.
	engine.nowFunc = func() time.Time { return now.Add(5 * time.Second) }

	get, cancelCollect := collectEvents(bus, types.EventPulseFire)
	defer cancelCollect()

	ctx, cancel := context.WithCancel(context.Background())

	go engine.Run(ctx)

	// Wait for a few ticks.
	time.Sleep(100 * time.Millisecond)
	cancel()

	events := get()
	if len(events) == 0 {
		t.Fatal("expected at least one fire event from the Run loop")
	}
}

// --- Cadence with Payload Tests ---

func TestCreateCadence_WithPayload(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	payload := map[string]string{"action": "cleanup", "target": "temp"}
	c, err := engine.CreateCadence("cleanup", "@every 1h", types.PriorityBackground, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := engine.GetCadence(c.ID)
	if err != nil {
		t.Fatalf("GetCadence: %v", err)
	}
	// Payload is stored as-is (any type).
	p, ok := got.Payload.(map[string]string)
	if !ok {
		t.Fatalf("expected payload to be map[string]string, got %T", got.Payload)
	}
	if p["action"] != "cleanup" {
		t.Errorf("expected payload action 'cleanup', got %q", p["action"])
	}
}

// --- Priority Validation Tests ---

func TestPulsePriority_Valid(t *testing.T) {
	tests := []struct {
		p    types.PulsePriority
		want bool
	}{
		{types.PriorityBackground, true},
		{types.PriorityStandard, true},
		{types.PriorityUrgent, true},
		{"invalid", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.p.Valid(); got != tt.want {
			t.Errorf("PulsePriority(%q).Valid() = %v, want %v", tt.p, got, tt.want)
		}
	}
}

// --- Disabled Cadence Tests ---

func TestProcessTick_SkipsDisabledCadence(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	c, err := engine.CreateCadence("disabled", "@every 5s", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("CreateCadence: %v", err)
	}

	// Disable the cadence.
	engine.mu.Lock()
	engine.cadences[c.ID].Enabled = false
	engine.mu.Unlock()

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	engine.nowFunc = func() time.Time { return now.Add(10 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 0 {
		t.Errorf("expected 0 fire events for disabled cadence, got %d", len(events))
	}
}

func TestCreateCadenceWithMode_AgentOrder(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	c, err := engine.CreateCadenceWithMode("review-prs", "@every 1h", types.PriorityStandard, types.ModeAgentOrder, "agent-reviewer", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Mode != types.ModeAgentOrder {
		t.Errorf("expected mode agent_order, got %s", c.Mode)
	}
	if c.TargetAgent != "agent-reviewer" {
		t.Errorf("expected target_agent agent-reviewer, got %s", c.TargetAgent)
	}
}

func TestCreateCadenceWithMode_AgentOrderRequiresTarget(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.CreateCadenceWithMode("review-prs", "@every 1h", types.PriorityStandard, types.ModeAgentOrder, "", nil)
	if err == nil {
		t.Fatal("expected error for agent_order mode without target_agent")
	}
}

func TestCreateCadenceWithMode_InvalidMode(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	_, err := engine.CreateCadenceWithMode("test", "@every 1h", types.PriorityStandard, "invalid_mode", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestAgentOrderDispatch_RoutesViaCommHub(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	var dispatched struct {
		mu          sync.Mutex
		targetAgent string
		cadenceName string
		cadenceID   string
		payload     any
	}

	engine.SetAgentOrderSender(func(_ context.Context, target, name, id string, pl any) error {
		dispatched.mu.Lock()
		dispatched.targetAgent = target
		dispatched.cadenceName = name
		dispatched.cadenceID = id
		dispatched.payload = pl
		dispatched.mu.Unlock()
		return nil
	})

	c, err := engine.CreateCadenceWithMode("daily-summary", "@every 1s", types.PriorityStandard, types.ModeAgentOrder, "agent-summarizer", map[string]string{"action": "summarize"})
	if err != nil {
		t.Fatalf("create cadence: %v", err)
	}

	// Advance time past the next fire.
	engine.nowFunc = func() time.Time { return now.Add(2 * time.Second) }
	engine.processTick()

	dispatched.mu.Lock()
	defer dispatched.mu.Unlock()

	if dispatched.targetAgent != "agent-summarizer" {
		t.Errorf("expected target agent-summarizer, got %q", dispatched.targetAgent)
	}
	if dispatched.cadenceName != "daily-summary" {
		t.Errorf("expected cadence name daily-summary, got %q", dispatched.cadenceName)
	}
	if dispatched.cadenceID != c.ID {
		t.Errorf("expected cadence ID %q, got %q", c.ID, dispatched.cadenceID)
	}
}

func TestAgentOrderDispatch_FallbackToEventWhenNoSender(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	engine.nowFunc = func() time.Time { return now }

	// No AgentOrderSender set — should fall back to event mode.
	_, err := engine.CreateCadenceWithMode("test-fallback", "@every 1s", types.PriorityStandard, types.ModeAgentOrder, "agent-x", nil)
	if err != nil {
		t.Fatalf("create cadence: %v", err)
	}

	get, cancel := collectEvents(bus, types.EventPulseFire)
	defer cancel()

	engine.nowFunc = func() time.Time { return now.Add(2 * time.Second) }
	engine.processTick()

	time.Sleep(50 * time.Millisecond)

	events := get()
	if len(events) != 1 {
		t.Errorf("expected 1 fallback fire event, got %d", len(events))
	}
}

func TestCreateCadence_BackwardsCompatible(t *testing.T) {
	bus := nervous.NewEventBus(64)
	engine := NewEngine(bus, testLogger())

	// The original CreateCadence without mode should default to event mode.
	c, err := engine.CreateCadence("legacy", "@every 1h", types.PriorityStandard, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Mode != types.ModeEvent {
		t.Errorf("expected default mode 'event', got %q", c.Mode)
	}
	if c.TargetAgent != "" {
		t.Errorf("expected empty target_agent, got %q", c.TargetAgent)
	}
}
