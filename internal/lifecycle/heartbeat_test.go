package lifecycle

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// heartbeatMockRepo extends mockLifecycleRepo with heartbeat tracking and
// configurable stale agent responses.
type heartbeatMockRepo struct {
	mu             sync.Mutex
	agents         []*repo.AgentState
	staleAgents    []string
	heartbeats     map[string]int
	transitions    []*repo.LifecycleTransition
}

func newHeartbeatMockRepo() *heartbeatMockRepo {
	return &heartbeatMockRepo{
		heartbeats: make(map[string]int),
	}
}

func (m *heartbeatMockRepo) LogTransition(_ context.Context, entry *repo.LifecycleTransition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitions = append(m.transitions, entry)
	// Also update the agent state in the mock.
	for _, a := range m.agents {
		if a.AgentID == entry.AgentID {
			a.State = entry.ToState
		}
	}
	return nil
}

func (m *heartbeatMockRepo) GetState(_ context.Context, agentID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.agents {
		if a.AgentID == agentID {
			return a.State, nil
		}
	}
	return "", fmt.Errorf("agent %q not found", agentID)
}

func (m *heartbeatMockRepo) WriteHeartbeat(_ context.Context, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeats[agentID]++
	return nil
}

func (m *heartbeatMockRepo) GetStaleAgents(_ context.Context, _ time.Duration) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.staleAgents))
	copy(result, m.staleAgents)
	return result, nil
}

func (m *heartbeatMockRepo) ListAgentStates(_ context.Context) ([]*repo.AgentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*repo.AgentState, len(m.agents))
	copy(result, m.agents)
	return result, nil
}

func (m *heartbeatMockRepo) getHeartbeatCount(agentID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heartbeats[agentID]
}

func (m *heartbeatMockRepo) getTransitions() []*repo.LifecycleTransition {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*repo.LifecycleTransition, len(m.transitions))
	copy(result, m.transitions)
	return result
}

func TestHeartbeatMonitor_WritesHeartbeats(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	bus := nervous.NewEventBus(64)

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(50*time.Millisecond),
		WithLeaseCheckInterval(10*time.Second), // don't check leases in this test
	)
	hm.Register("agent-1")
	hm.Register("agent-2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hm.Start(ctx)

	// Wait for a few heartbeat cycles.
	time.Sleep(200 * time.Millisecond)
	cancel()

	count1 := mockRepo.getHeartbeatCount("agent-1")
	count2 := mockRepo.getHeartbeatCount("agent-2")

	if count1 == 0 {
		t.Error("expected heartbeats for agent-1, got 0")
	}
	if count2 == 0 {
		t.Error("expected heartbeats for agent-2, got 0")
	}
}

func TestHeartbeatMonitor_DeregisterStopsHeartbeats(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	bus := nervous.NewEventBus(64)

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(50*time.Millisecond),
		WithLeaseCheckInterval(10*time.Second),
	)
	hm.Register("agent-1")
	hm.Register("agent-2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hm.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Deregister agent-2 and wait for more cycles.
	hm.Deregister("agent-2")
	countBefore := mockRepo.getHeartbeatCount("agent-2")

	time.Sleep(150 * time.Millisecond)
	countAfter := mockRepo.getHeartbeatCount("agent-2")

	// agent-2 should not have received more heartbeats after deregister.
	if countAfter > countBefore+1 {
		t.Errorf("agent-2 got heartbeats after deregister: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestHeartbeatMonitor_LeaseExpiryTransitionsToError(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	mockRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StateActive)},
	}
	mockRepo.staleAgents = []string{"agent-1"}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test")

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(10*time.Second),
		WithLeaseTTL(30*time.Second),
		WithLeaseCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hm.Start(ctx)

	// Wait for the lease checker to fire.
	select {
	case event := <-sub.Ch:
		if event.Type != types.EventLifecycleStalled {
			t.Errorf("expected lifecycle.stalled, got %s", event.Type)
		}
		if event.Source != "heartbeat_monitor" {
			t.Errorf("expected source heartbeat_monitor, got %s", event.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for lease expiry event")
	}

	// Verify transition was logged.
	transitions := mockRepo.getTransitions()
	if len(transitions) == 0 {
		t.Fatal("expected transition to be logged")
	}
	if transitions[0].ToState != string(StateError) {
		t.Errorf("expected transition to error, got %s", transitions[0].ToState)
	}
	if transitions[0].Reason != "heartbeat lease expired" {
		t.Errorf("unexpected reason: %s", transitions[0].Reason)
	}
}

func TestHeartbeatMonitor_SkipsErrorState(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	mockRepo.agents = []*repo.AgentState{
		{AgentID: "agent-err", State: string(StateError)},
	}
	mockRepo.staleAgents = []string{"agent-err"}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test")

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(10*time.Second),
		WithLeaseCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hm.Start(ctx)

	select {
	case event := <-sub.Ch:
		t.Errorf("unexpected event for already-errored agent: %v", event)
	case <-time.After(200 * time.Millisecond):
		// Good — no events for error-state agents.
	}
}

func TestHeartbeatMonitor_SkipsDecommissioned(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	mockRepo.agents = []*repo.AgentState{
		{AgentID: "agent-decom", State: string(StateDecommissioned)},
	}
	mockRepo.staleAgents = []string{"agent-decom"}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test")

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(10*time.Second),
		WithLeaseCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hm.Start(ctx)

	select {
	case event := <-sub.Ch:
		t.Errorf("unexpected event for decommissioned agent: %v", event)
	case <-time.After(200 * time.Millisecond):
		// Good — decommissioned agents are not transitioned.
	}
}

func TestHeartbeatMonitor_Stops(t *testing.T) {
	mockRepo := newHeartbeatMockRepo()
	bus := nervous.NewEventBus(64)

	hm := NewHeartbeatMonitor(mockRepo, bus, discardLogger(),
		WithHeartbeatInterval(50*time.Millisecond),
		WithLeaseCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hm.Start(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat monitor did not stop within timeout")
	}
}

func TestShouldTransitionOnLeaseExpiry(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateActive, true},
		{StateOnboarding, true},
		{StateRecovering, true},
		{StateRehydrating, true},
		{StateSuspended, true},
		{StateDraining, true},
		{StateError, false},
		{StateDecommissioned, false},
		{StateHalted, false},
		{StatePending, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := shouldTransitionOnLeaseExpiry(tt.state)
			if got != tt.expected {
				t.Errorf("shouldTransitionOnLeaseExpiry(%q) = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}
