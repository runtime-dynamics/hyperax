package lifecycle

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockLifecycleRepo is a minimal mock for testing the stall detector.
type mockLifecycleRepo struct {
	agents []*repo.AgentState
}

func (m *mockLifecycleRepo) LogTransition(_ context.Context, _ *repo.LifecycleTransition) error {
	return nil
}

func (m *mockLifecycleRepo) GetState(_ context.Context, agentID string) (string, error) {
	for _, a := range m.agents {
		if a.AgentID == agentID {
			return a.State, nil
		}
	}
	return "", repo.ErrNotFound
}

func (m *mockLifecycleRepo) WriteHeartbeat(_ context.Context, _ string) error {
	return nil
}

func (m *mockLifecycleRepo) GetStaleAgents(_ context.Context, _ time.Duration) ([]string, error) {
	return nil, nil
}

func (m *mockLifecycleRepo) ListAgentStates(_ context.Context) ([]*repo.AgentState, error) {
	result := make([]*repo.AgentState, len(m.agents))
	copy(result, m.agents)
	return result, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStallDetector_DetectsStall(t *testing.T) {
	now := time.Now()
	mockRepo := &mockLifecycleRepo{
		agents: []*repo.AgentState{
			{
				AgentID:   "agent-1",
				State:     string(StateOnboarding),
				UpdatedAt: now.Add(-10 * time.Minute), // stuck for 10 minutes
			},
			{
				AgentID:   "agent-2",
				State:     string(StateActive),
				UpdatedAt: now.Add(-30 * time.Minute), // active, not transient
			},
		},
	}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-sub", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test-sub")

	sd := NewStallDetector(mockRepo, bus, discardLogger(),
		WithStallTimeout(5*time.Minute),
		WithCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sd.Start(ctx)

	// Wait for at least one check cycle.
	select {
	case event := <-sub.Ch:
		if event.Type != types.EventLifecycleStalled {
			t.Errorf("expected lifecycle.stalled event, got %s", event.Type)
		}
		// Verify the payload contains agent-1 info.
		if event.Source != "stall_detector" {
			t.Errorf("expected source 'stall_detector', got %s", event.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stall event")
	}
}

func TestStallDetector_NoStallForNonTransient(t *testing.T) {
	now := time.Now()
	mockRepo := &mockLifecycleRepo{
		agents: []*repo.AgentState{
			{
				AgentID:   "agent-1",
				State:     string(StateActive),
				UpdatedAt: now.Add(-30 * time.Minute),
			},
			{
				AgentID:   "agent-2",
				State:     string(StateError),
				UpdatedAt: now.Add(-30 * time.Minute),
			},
		},
	}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-sub", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test-sub")

	sd := NewStallDetector(mockRepo, bus, discardLogger(),
		WithStallTimeout(5*time.Minute),
		WithCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sd.Start(ctx)

	select {
	case event := <-sub.Ch:
		t.Errorf("unexpected stall event: %v", event)
	case <-time.After(200 * time.Millisecond):
		// Good — no stall events.
	}
}

func TestStallDetector_NoStallWithinTimeout(t *testing.T) {
	now := time.Now()
	mockRepo := &mockLifecycleRepo{
		agents: []*repo.AgentState{
			{
				AgentID:   "agent-1",
				State:     string(StateRecovering),
				UpdatedAt: now.Add(-1 * time.Minute), // only 1 minute, under 5-min timeout
			},
		},
	}

	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-sub", types.EventLifecycleStalled)
	defer bus.Unsubscribe("test-sub")

	sd := NewStallDetector(mockRepo, bus, discardLogger(),
		WithStallTimeout(5*time.Minute),
		WithCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sd.Start(ctx)

	select {
	case event := <-sub.Ch:
		t.Errorf("unexpected stall event for agent within timeout: %v", event)
	case <-time.After(200 * time.Millisecond):
		// Good — no stall events.
	}
}

func TestStallDetector_Stops(t *testing.T) {
	mockRepo := &mockLifecycleRepo{}
	bus := nervous.NewEventBus(64)

	sd := NewStallDetector(mockRepo, bus, discardLogger(),
		WithCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sd.Start(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("stall detector did not stop within timeout")
	}
}
