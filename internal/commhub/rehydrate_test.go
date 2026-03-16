package commhub

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestRehydrateIfZombie_TriggersRehydration(t *testing.T) {
	hub := newTestHub()

	var rehydrated atomic.Bool
	hub.SetZombieDetector(func(_ context.Context, agentID string) bool {
		return agentID == "zombie-agent"
	})
	hub.SetRehydrationFunc(func(_ context.Context, agentID string) (map[string]any, error) {
		rehydrated.Store(true)
		return map[string]any{"agent_id": agentID, "rehydrated": true}, nil
	})

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:          "msg-001",
		From:        "sender",
		To:          "zombie-agent",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "Hello zombie",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if !rehydrated.Load() {
		t.Error("expected rehydration to be triggered for zombie agent")
	}

	// Message should still be delivered.
	msgs := hub.Receive("zombie-agent", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestRehydrateIfZombie_SkipsNonZombie(t *testing.T) {
	hub := newTestHub()

	var rehydrated atomic.Bool
	hub.SetZombieDetector(func(_ context.Context, agentID string) bool {
		return false // No zombies.
	})
	hub.SetRehydrationFunc(func(_ context.Context, agentID string) (map[string]any, error) {
		rehydrated.Store(true)
		return nil, nil
	})

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:          "msg-002",
		From:        "sender",
		To:          "healthy-agent",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "Hello healthy",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if rehydrated.Load() {
		t.Error("rehydration should not be triggered for healthy agent")
	}
}

func TestRehydrateIfZombie_SkipsSystemTargets(t *testing.T) {
	hub := newTestHub()

	var rehydrated atomic.Bool
	hub.SetZombieDetector(func(_ context.Context, _ string) bool {
		return true // Everything is a zombie.
	})
	hub.SetRehydrationFunc(func(_ context.Context, _ string) (map[string]any, error) {
		rehydrated.Store(true)
		return nil, nil
	})

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:          "msg-003",
		From:        "sender",
		To:          "system:pulse",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "System message",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if rehydrated.Load() {
		t.Error("rehydration should not be triggered for system targets")
	}
}

func TestRehydrateIfZombie_NoFunctionsConfigured(t *testing.T) {
	hub := newTestHub()

	// No zombie detector or rehydration function configured — should be a no-op.
	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:          "msg-004",
		From:        "sender",
		To:          "any-agent",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "Hello",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	msgs := hub.Receive("any-agent", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}
