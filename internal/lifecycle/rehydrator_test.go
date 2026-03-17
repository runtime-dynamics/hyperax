package lifecycle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockCheckpointRepo provides a minimal in-memory checkpoint store for testing.
type mockCheckpointRepo struct {
	checkpoints map[string]*repo.AgentCheckpoint
}

func newMockCheckpointRepo() *mockCheckpointRepo {
	return &mockCheckpointRepo{
		checkpoints: make(map[string]*repo.AgentCheckpoint),
	}
}

func (m *mockCheckpointRepo) Save(_ context.Context, cp *repo.AgentCheckpoint) error {
	if cp.ID == "" {
		cp.ID = fmt.Sprintf("cp-%d", len(m.checkpoints)+1)
	}
	m.checkpoints[cp.AgentID] = cp
	return nil
}

func (m *mockCheckpointRepo) GetLatest(_ context.Context, agentID string) (*repo.AgentCheckpoint, error) {
	cp, ok := m.checkpoints[agentID]
	if !ok {
		return nil, fmt.Errorf("no checkpoint found")
	}
	return cp, nil
}

func (m *mockCheckpointRepo) List(_ context.Context, _ string, _ int) ([]*repo.AgentCheckpoint, error) {
	return nil, nil
}

func (m *mockCheckpointRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockCheckpointRepo) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func TestRehydrator_FullRehydration(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())
	txMgr := refactor.NewTransactionManager(discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StateRehydrating)},
	}

	cpRepo := newMockCheckpointRepo()
	if err := cpRepo.Save(context.Background(), &repo.AgentCheckpoint{
		AgentID:        "agent-1",
		TaskID:         "task-42",
		WorkingContext: `{"key":"value"}`,
		ActiveFiles:    `["/foo/bar.go"]`,
		RefactorTxID:   "", // no active tx
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	// Send a pending message to the agent before rehydration.
	if err := hub.Send(context.Background(), &types.MessageEnvelope{
		From:    "system",
		To:      "agent-1",
		Content: "pending message",
		Trust:   types.TrustInternal,
	}); err != nil {
		t.Fatalf("send pending message: %v", err)
	}

	r := NewRehydrator(lifecycleRepo, cpRepo, txMgr, hub, bus, discardLogger())

	result, err := r.Rehydrate(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("Rehydrate failed: %v", err)
	}

	if !result.CheckpointRestored {
		t.Error("expected checkpoint to be restored")
	}
	if result.FellBackToOnboard {
		t.Error("did not expect fallback to onboard")
	}
	if result.MessagesReplayed != 1 {
		t.Errorf("expected 1 message replayed, got %d", result.MessagesReplayed)
	}

	// Verify agent is now active.
	state, err := lifecycleRepo.GetState(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state != string(StateActive) {
		t.Errorf("expected active state, got %s", state)
	}
}

func TestRehydrator_NoCheckpointFallsBackToOnboarding(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StateRehydrating)},
	}

	cpRepo := newMockCheckpointRepo() // empty — no checkpoints

	r := NewRehydrator(lifecycleRepo, cpRepo, nil, hub, bus, discardLogger())

	result, err := r.Rehydrate(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("Rehydrate failed: %v", err)
	}

	if !result.FellBackToOnboard {
		t.Error("expected fallback to onboard")
	}
	if result.CheckpointRestored {
		t.Error("did not expect checkpoint to be restored")
	}

	// Verify agent is now in onboarding state.
	state, err := lifecycleRepo.GetState(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state != string(StateOnboarding) {
		t.Errorf("expected onboarding state, got %s", state)
	}
}

func TestRehydrator_RollsBackRefactorTx(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())
	txMgr := refactor.NewTransactionManager(discardLogger())

	// Begin a transaction to simulate an active refactor.
	txID, err := txMgr.Begin()
	if err != nil {
		t.Fatalf("Begin tx failed: %v", err)
	}

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StateRehydrating)},
	}

	cpRepo := newMockCheckpointRepo()
	if err := cpRepo.Save(context.Background(), &repo.AgentCheckpoint{
		AgentID:      "agent-1",
		RefactorTxID: txID,
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	r := NewRehydrator(lifecycleRepo, cpRepo, txMgr, hub, bus, discardLogger())

	result, err := r.Rehydrate(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("Rehydrate failed: %v", err)
	}

	if !result.RefactorRolledBack {
		t.Error("expected refactor tx to be rolled back")
	}

	// Verify the transaction is no longer accessible.
	_, getErr := txMgr.Get(txID)
	if getErr == nil {
		t.Error("expected transaction to be gone after rollback")
	}
}

func TestRehydrator_WrongStateFails(t *testing.T) {
	bus := nervous.NewEventBus(64)

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StateActive)},
	}

	cpRepo := newMockCheckpointRepo()

	r := NewRehydrator(lifecycleRepo, cpRepo, nil, nil, bus, discardLogger())

	_, err := r.Rehydrate(context.Background(), "agent-1")
	if err == nil {
		t.Fatal("expected error for wrong state")
	}
}
