package budget

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// --- Mock BudgetRepo ---

type mockBudgetRepo struct {
	thresholds map[string]float64
	costs      map[string]float64
}

func newMockBudgetRepo() *mockBudgetRepo {
	return &mockBudgetRepo{
		thresholds: make(map[string]float64),
		costs:      make(map[string]float64),
	}
}

func (m *mockBudgetRepo) GetCumulativeEnergyCost(_ context.Context, scope string) (float64, error) {
	return m.costs[scope], nil
}

func (m *mockBudgetRepo) GetBudgetThreshold(_ context.Context, scope string) (float64, error) {
	if t, ok := m.thresholds[scope]; ok {
		return t, nil
	}
	return 0, nil
}

func (m *mockBudgetRepo) SetBudgetThreshold(_ context.Context, scope string, threshold float64) error {
	m.thresholds[scope] = threshold
	return nil
}

func (m *mockBudgetRepo) RecordEnergyCost(_ context.Context, scope string, cost float64, _ string, _ string) error {
	m.costs[scope] += cost
	return nil
}

func (m *mockBudgetRepo) ListThresholdScopes(_ context.Context) ([]string, error) {
	scopes := make([]string, 0, len(m.thresholds))
	for s := range m.thresholds {
		scopes = append(scopes, s)
	}
	return scopes, nil
}

// --- Helpers ---

// collectEvents drains all events from a subscriber channel within a timeout.
func collectEvents(sub *nervous.Subscriber, timeout time.Duration) []types.NervousEvent {
	var events []types.NervousEvent
	deadline := time.After(timeout)
	for {
		select {
		case e, ok := <-sub.Ch:
			if !ok {
				return events
			}
			events = append(events, e)
		case <-deadline:
			return events
		}
	}
}

// --- Tests ---

func TestMonitor_WarningEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["workspace:dev"] = 100.0
	repo.costs["workspace:dev"] = 82.0 // 82% → warning

	mon := NewMonitor(repo, nil, bus, slogDiscard(), WithCheckInterval(time.Minute))

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventBudgetWarning {
		t.Errorf("expected event type %s, got %s", types.EventBudgetWarning, events[0].Type)
	}
	if events[0].Scope != "workspace:dev" {
		t.Errorf("expected scope workspace:dev, got %s", events[0].Scope)
	}

	// Verify payload.
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if pct, ok := payload["percent_used"].(float64); !ok || pct < 80.0 {
		t.Errorf("expected percent_used >= 80, got %v", payload["percent_used"])
	}
}

func TestMonitor_CriticalInterjection(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["agent:planner"] = 50.0
	repo.costs["agent:planner"] = 48.0 // 96% → critical

	// nil ijMgr — just verify event and no panic.
	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventBudgetCritical {
		t.Errorf("expected event type %s, got %s", types.EventBudgetCritical, events[0].Type)
	}
}

func TestMonitor_ExhaustedInterjection(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["global"] = 200.0
	repo.costs["global"] = 210.0 // 105% → exhausted

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventBudgetExhausted {
		t.Errorf("expected event type %s, got %s", types.EventBudgetExhausted, events[0].Type)
	}
}

func TestMonitor_NoDuplicateInterjections(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["agent:coder"] = 100.0
	repo.costs["agent:coder"] = 96.0 // 96% → critical

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	// Pre-populate an active alert for this scope.
	mon.mu.Lock()
	mon.activeAlerts["agent:coder"] = "existing-ij-id"
	mon.mu.Unlock()

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	// Should still get the event, but no new interjection attempt (nil ijMgr so
	// no panic, but the duplicate guard still fires).
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Active alerts should still contain the original ID, not a new one.
	alerts := mon.ActiveAlerts()
	if alerts["agent:coder"] != "existing-ij-id" {
		t.Errorf("expected existing-ij-id, got %s", alerts["agent:coder"])
	}
}

func TestMonitor_ContextCancellation(t *testing.T) {
	bus := nervous.NewEventBus(64)
	repo := newMockBudgetRepo()

	mon := NewMonitor(repo, nil, bus, slogDiscard(), WithCheckInterval(50*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	// Let it tick once.
	time.Sleep(80 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// OK — Run exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestMonitor_BelowWarningNoEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["workspace:prod"] = 100.0
	repo.costs["workspace:prod"] = 50.0 // 50% → no event

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for 50%% utilization, got %d", len(events))
	}
}

func TestMonitor_ClearAlert(t *testing.T) {
	mon := NewMonitor(newMockBudgetRepo(), nil, nervous.NewEventBus(8), slogDiscard())

	mon.mu.Lock()
	mon.activeAlerts["scope-a"] = "ij-123"
	mon.mu.Unlock()

	mon.ClearAlert("scope-a")

	alerts := mon.ActiveAlerts()
	if _, exists := alerts["scope-a"]; exists {
		t.Error("expected scope-a to be cleared from active alerts")
	}
}

func TestMonitor_NoScopesNoError(t *testing.T) {
	bus := nervous.NewEventBus(64)
	repo := newMockBudgetRepo() // no thresholds configured

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() should succeed with no scopes, got: %v", err)
	}
}

func TestMonitor_ExactBoundaryWarning(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["scope-x"] = 100.0
	repo.costs["scope-x"] = 80.0 // Exactly 80% → warning

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event at exact 80%%, got %d", len(events))
	}
	if events[0].Type != types.EventBudgetWarning {
		t.Errorf("expected warning at exactly 80%%, got %s", events[0].Type)
	}
}

func TestMonitor_ExactBoundaryCritical(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	repo := newMockBudgetRepo()
	repo.thresholds["scope-y"] = 200.0
	repo.costs["scope-y"] = 190.0 // Exactly 95% → critical

	mon := NewMonitor(repo, nil, bus, slogDiscard())

	err := mon.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	events := collectEvents(sub, 100*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event at exact 95%%, got %d", len(events))
	}
	if events[0].Type != types.EventBudgetCritical {
		t.Errorf("expected critical at exactly 95%%, got %s", events[0].Type)
	}
}

// slogDiscard returns a logger that discards all output.
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, nil))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
