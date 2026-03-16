package pulse

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestWatchdog_TriggerOnStaleHeartbeat(t *testing.T) {
	bus := nervous.NewEventBus(64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.Run(ctx)

	engine := NewEngine(bus, testLogger())
	repo := newMockInterjectionRepo()
	mgr := interject.NewManager(repo, bus, testLogger())

	wd := NewWatchdog(engine, mgr, bus, testLogger())
	wd.threshold = 100 * time.Millisecond
	wd.interval = 50 * time.Millisecond

	// Set a heartbeat that is already stale (10 seconds ago).
	staleTime := time.Now().Add(-10 * time.Second)
	engine.lastHeartbeat.Store(staleTime.UnixNano())

	// Subscribe for watchdog events.
	getEvents, stopCollect := collectEvents(bus, types.EventWatchdogTriggered)
	defer stopCollect()

	// Run watchdog briefly.
	wdCtx, wdCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer wdCancel()
	go wd.Run(wdCtx)

	// Wait for the watchdog to trigger.
	time.Sleep(150 * time.Millisecond)

	// Should have triggered.
	if !wd.triggered.Load() {
		t.Error("expected watchdog to be triggered")
	}

	// Should have published a watchdog event.
	events := getEvents()
	if len(events) == 0 {
		t.Error("expected at least one watchdog.triggered event")
	}

	// Should have created a global halt interjection.
	active, err := mgr.GetAllActive(context.Background())
	if err != nil {
		t.Fatalf("GetAllActive() error: %v", err)
	}
	if len(active) == 0 {
		t.Error("expected at least one active interjection from watchdog")
	} else {
		ij := active[0]
		if ij.Source != "watchdog" {
			t.Errorf("expected source=watchdog, got %q", ij.Source)
		}
		if ij.Scope != "global" {
			t.Errorf("expected scope=global, got %q", ij.Scope)
		}
		if ij.Severity != "fatal" {
			t.Errorf("expected severity=fatal, got %q", ij.Severity)
		}
		if ij.SourceClearance != 3 {
			t.Errorf("expected source_clearance=3, got %d", ij.SourceClearance)
		}
	}
}

func TestWatchdog_NoTriggerOnFreshHeartbeat(t *testing.T) {
	bus := nervous.NewEventBus(64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.Run(ctx)

	engine := NewEngine(bus, testLogger())
	repo := newMockInterjectionRepo()
	mgr := interject.NewManager(repo, bus, testLogger())

	wd := NewWatchdog(engine, mgr, bus, testLogger())
	wd.threshold = 2 * time.Second
	wd.interval = 50 * time.Millisecond

	// Set a fresh heartbeat.
	engine.lastHeartbeat.Store(time.Now().UnixNano())

	wdCtx, wdCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer wdCancel()
	go wd.Run(wdCtx)

	time.Sleep(150 * time.Millisecond)

	if wd.triggered.Load() {
		t.Error("watchdog should NOT be triggered with fresh heartbeat")
	}
}

func TestWatchdog_RecoveryAfterTrigger(t *testing.T) {
	bus := nervous.NewEventBus(64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.Run(ctx)

	engine := NewEngine(bus, testLogger())
	repo := newMockInterjectionRepo()
	mgr := interject.NewManager(repo, bus, testLogger())

	wd := NewWatchdog(engine, mgr, bus, testLogger())
	wd.threshold = 2 * time.Second
	wd.interval = 50 * time.Millisecond

	getRecovery, stopCollect := collectEvents(bus, types.EventWatchdogRecovered)
	defer stopCollect()

	// Start with very stale heartbeat (10 seconds ago) — way past 2s threshold.
	engine.lastHeartbeat.Store(time.Now().Add(-10 * time.Second).UnixNano())

	wdCtx, wdCancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer wdCancel()
	go wd.Run(wdCtx)

	// Wait for trigger.
	time.Sleep(80 * time.Millisecond)
	if !wd.triggered.Load() {
		t.Error("expected watchdog to trigger")
	}

	// Refresh the heartbeat to now. With 2s threshold this will stay fresh.
	engine.lastHeartbeat.Store(time.Now().UnixNano())

	// Wait for recovery detection.
	time.Sleep(150 * time.Millisecond)

	if wd.triggered.Load() {
		t.Error("expected watchdog to recover after fresh heartbeat")
	}

	events := getRecovery()
	if len(events) == 0 {
		t.Error("expected at least one watchdog.recovered event")
	}
}

func TestWatchdog_SkipsCheckWhenEngineNotStarted(t *testing.T) {
	bus := nervous.NewEventBus(64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.Run(ctx)

	engine := NewEngine(bus, testLogger())
	// Do NOT set heartbeat — engine not started.

	wd := NewWatchdog(engine, nil, bus, testLogger())
	wd.threshold = 50 * time.Millisecond
	wd.interval = 30 * time.Millisecond

	wdCtx, wdCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer wdCancel()
	go wd.Run(wdCtx)

	time.Sleep(80 * time.Millisecond)

	if wd.triggered.Load() {
		t.Error("watchdog should NOT trigger when engine hasn't started (zero heartbeat)")
	}
}

func TestEngine_LastHeartbeatUpdatedOnTick(t *testing.T) {
	bus := nervous.NewEventBus(64)

	engine := NewEngine(bus, testLogger())

	// Before run, heartbeat should be zero.
	hb := engine.LastHeartbeat()
	if !hb.IsZero() {
		t.Error("expected zero heartbeat before Run")
	}

	// Manually call processTick to simulate a tick.
	engine.processTick()

	hb = engine.LastHeartbeat()
	if hb.IsZero() {
		t.Error("expected non-zero heartbeat after processTick")
	}
	if time.Since(hb) > 1*time.Second {
		t.Errorf("heartbeat too old: %v", hb)
	}
}

// --- Mock interjection repo for watchdog tests ---

type mockInterjectionRepo struct {
	mu            sync.Mutex
	interjections map[string]*types.Interjection
	clearances    map[string]int
	bypasses      map[string]*types.SieveBypass
	dlq           map[string]*types.DLQEntry
	counter       int
}

func newMockInterjectionRepo() *mockInterjectionRepo {
	return &mockInterjectionRepo{
		interjections: make(map[string]*types.Interjection),
		clearances:    make(map[string]int),
		bypasses:      make(map[string]*types.SieveBypass),
		dlq:           make(map[string]*types.DLQEntry),
	}
}

func (m *mockInterjectionRepo) Create(_ context.Context, ij *types.Interjection) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter++
	ij.ID = "ij-" + intToStr(m.counter)
	ij.Status = "active"
	ij.CreatedAt = time.Now()
	m.interjections[ij.ID] = ij
	return ij.ID, nil
}

func (m *mockInterjectionRepo) GetByID(_ context.Context, id string) (*types.Interjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ij, ok := m.interjections[id]; ok {
		return ij, nil
	}
	return nil, errNotFound(id)
}

func (m *mockInterjectionRepo) GetActive(_ context.Context, scope string) ([]*types.Interjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*types.Interjection
	for _, ij := range m.interjections {
		if ij.Scope == scope && ij.Status == "active" {
			result = append(result, ij)
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) GetAllActive(_ context.Context) ([]*types.Interjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*types.Interjection
	for _, ij := range m.interjections {
		if ij.Status == "active" {
			result = append(result, ij)
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) GetHistory(_ context.Context, _ string, _ int) ([]*types.Interjection, error) {
	return nil, nil
}

func (m *mockInterjectionRepo) Resolve(_ context.Context, id string, action *types.ResolutionAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ij, ok := m.interjections[id]; ok {
		ij.Status = "resolved"
		ij.Resolution = action.Resolution
		ij.Action = action.Action
		return nil
	}
	return errNotFound(id)
}

func (m *mockInterjectionRepo) Expire(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ij, ok := m.interjections[id]; ok {
		ij.Status = "expired"
		return nil
	}
	return errNotFound(id)
}

func (m *mockInterjectionRepo) GetClearanceLevel(_ context.Context, personaID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.clearances[personaID]; ok {
		return v, nil
	}
	return 0, errNotFound(personaID)
}

func (m *mockInterjectionRepo) CreateBypass(_ context.Context, b *types.SieveBypass) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter++
	b.ID = "bp-" + intToStr(m.counter)
	m.bypasses[b.ID] = b
	return b.ID, nil
}

func (m *mockInterjectionRepo) GetActiveBypass(_ context.Context, _ string) ([]*types.SieveBypass, error) {
	return nil, nil
}

func (m *mockInterjectionRepo) RevokeBypass(_ context.Context, _ string) error { return nil }

func (m *mockInterjectionRepo) ExpireBypasses(_ context.Context) (int, error) { return 0, nil }

func (m *mockInterjectionRepo) EnqueueDLQ(_ context.Context, e *types.DLQEntry) (string, error) {
	m.counter++
	e.ID = "dlq-" + intToStr(m.counter)
	e.Status = "queued"
	m.dlq[e.ID] = e
	return e.ID, nil
}

func (m *mockInterjectionRepo) ListDLQ(_ context.Context, _ string, _ int) ([]*types.DLQEntry, error) {
	return nil, nil
}

func (m *mockInterjectionRepo) ReplayDLQ(_ context.Context, _ string) error { return nil }
func (m *mockInterjectionRepo) DismissDLQ(_ context.Context, _ string) error { return nil }
func (m *mockInterjectionRepo) CountDLQ(_ context.Context, _ string) (int, error) {
	return 0, nil
}

type mockNotFoundErr struct{ id string }

func (e *mockNotFoundErr) Error() string { return "not found: " + e.id }

func errNotFound(id string) error { return &mockNotFoundErr{id: id} }

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
