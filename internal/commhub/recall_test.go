package commhub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockRecallFunc creates a RecallFunc that returns the given memories.
func mockRecallFunc(memories []types.MemoryContext, err error) RecallFunc {
	return func(ctx context.Context, query, workspaceID, personaID string) ([]types.MemoryContext, error) {
		return memories, err
	}
}

// slowRecallFunc creates a RecallFunc that takes the specified duration,
// respecting context cancellation.
func slowRecallFunc(delay time.Duration, memories []types.MemoryContext) RecallFunc {
	return func(ctx context.Context, query, workspaceID, personaID string) ([]types.MemoryContext, error) {
		select {
		case <-time.After(delay):
			return memories, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// mockAgentResolver returns fixed workspace and agent IDs.
func mockAgentResolver(workspaceID, agentID string) AgentResolver {
	return func(ctx context.Context, targetAgentID string) (string, string, error) {
		return workspaceID, agentID, nil
	}
}

func newRecallTestHub() *CommHub {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewCommHub(bus, logger)
}

func TestSend_WithMemoryRecall_AttachesMemories(t *testing.T) {
	hub := newRecallTestHub()

	memories := []types.MemoryContext{
		{
			Memory: types.Memory{ID: "mem-1", Content: "Go error handling patterns", Scope: types.MemoryScopePersona},
			Score:  0.95,
			Rank:   1,
			Source: "proactive",
		},
		{
			Memory: types.Memory{ID: "mem-2", Content: "Use context.WithTimeout for latency", Scope: types.MemoryScopeProject},
			Score:  0.8,
			Rank:   2,
			Source: "proactive",
		},
	}

	hub.SetRecallFunc(mockRecallFunc(memories, nil))
	hub.SetAgentResolver(mockAgentResolver("ws-1", "persona-1"))

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:          "msg-recall-1",
		From:        "agent-a",
		To:          "agent-b",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "How should I handle errors in Go?",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	msgs := hub.Receive("agent-b", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	raw, ok := msgs[0].Metadata["related_memories"]
	if !ok {
		t.Fatal("expected related_memories metadata to be present")
	}

	var recalled []types.MemoryContext
	if err := json.Unmarshal([]byte(raw), &recalled); err != nil {
		t.Fatalf("failed to parse related_memories: %v", err)
	}
	if len(recalled) != 2 {
		t.Errorf("expected 2 recalled memories, got %d", len(recalled))
	}
	if recalled[0].Memory.ID != "mem-1" {
		t.Errorf("expected first memory ID 'mem-1', got %q", recalled[0].Memory.ID)
	}
}

func TestSend_WithoutRecallFunc_NoMetadata(t *testing.T) {
	hub := newRecallTestHub()
	// No recall func set — should behave normally.

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-no-recall",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "Hello",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	msgs := hub.Receive("agent-b", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata != nil {
		if _, ok := msgs[0].Metadata["related_memories"]; ok {
			t.Error("expected no related_memories metadata when recall is not configured")
		}
	}
}

func TestSend_RecallError_GracefulDegradation(t *testing.T) {
	hub := newRecallTestHub()

	hub.SetRecallFunc(mockRecallFunc(nil, errors.New("memory engine unavailable")))

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-recall-err",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "This should still deliver",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	msgs := hub.Receive("agent-b", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata != nil {
		if _, ok := msgs[0].Metadata["related_memories"]; ok {
			t.Error("expected no related_memories when recall errors")
		}
	}
}

func TestSend_RecallExceedsLatencyBudget_DeliversImmediately(t *testing.T) {
	hub := newRecallTestHub()

	memories := []types.MemoryContext{
		{
			Memory: types.Memory{ID: "slow-mem", Content: "eventually recalled"},
			Score:  0.9,
			Rank:   1,
			Source: "proactive",
		},
	}

	// Set recall to take 200ms — well beyond the 50ms budget.
	hub.SetRecallFunc(slowRecallFunc(200*time.Millisecond, memories))
	hub.SetAgentResolver(mockAgentResolver("ws-1", "persona-1"))

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-slow-recall",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "This should be delivered quickly despite slow recall",
	}

	start := time.Now()
	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}
	elapsed := time.Since(start)

	// Send should complete within ~60ms (50ms timeout + some overhead), not 200ms.
	if elapsed > 100*time.Millisecond {
		t.Errorf("Send took %v, expected < 100ms (latency budget enforcement)", elapsed)
	}

	msgs := hub.Receive("agent-b", 10)
	if len(msgs) < 1 {
		t.Fatalf("expected at least 1 message, got %d", len(msgs))
	}

	// The original message should be delivered without memory metadata
	// (since recall timed out; follow-up delivered asynchronously).
	if msgs[0].Metadata != nil {
		if _, ok := msgs[0].Metadata["related_memories"]; ok {
			t.Error("expected no related_memories on timed-out recall")
		}
	}

	// Wait for the follow-up to be delivered asynchronously.
	time.Sleep(500 * time.Millisecond)

	followUp := hub.Receive("agent-b", 10)
	if len(followUp) != 1 {
		t.Fatalf("expected 1 follow-up message, got %d", len(followUp))
	}
	if followUp[0].ContentType != "memory_context" {
		t.Errorf("expected follow-up content_type 'memory_context', got %q", followUp[0].ContentType)
	}
	if followUp[0].From != "system:memory" {
		t.Errorf("expected follow-up from 'system:memory', got %q", followUp[0].From)
	}
}

func TestSend_EmptyContent_SkipsRecall(t *testing.T) {
	hub := newRecallTestHub()

	called := false
	hub.SetRecallFunc(func(ctx context.Context, query, wsID, pID string) ([]types.MemoryContext, error) {
		called = true
		return nil, nil
	})

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-empty",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "", // Empty content — recall should be skipped.
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if called {
		t.Error("recall function should not be called for empty content")
	}
}

func TestSend_RecallReturnsEmpty_NoMetadata(t *testing.T) {
	hub := newRecallTestHub()

	hub.SetRecallFunc(mockRecallFunc(nil, nil)) // No memories, no error.

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-no-results",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "Query that matches nothing",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	msgs := hub.Receive("agent-b", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata != nil {
		if _, ok := msgs[0].Metadata["related_memories"]; ok {
			t.Error("expected no related_memories when recall returns empty")
		}
	}
}

func TestSend_ScopeCascade_PersonaProjectGlobal(t *testing.T) {
	hub := newRecallTestHub()

	var queriedWsID, queriedPersonaID string
	hub.SetRecallFunc(func(ctx context.Context, query, workspaceID, personaID string) ([]types.MemoryContext, error) {
		queriedWsID = workspaceID
		queriedPersonaID = personaID
		return []types.MemoryContext{
			{Memory: types.Memory{ID: "m1", Content: "test"}, Score: 1.0, Source: "proactive"},
		}, nil
	})
	hub.SetAgentResolver(mockAgentResolver("workspace-alpha", "persona-beta"))

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-scope",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "scope cascade test",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if queriedWsID != "workspace-alpha" {
		t.Errorf("expected workspace_id 'workspace-alpha', got %q", queriedWsID)
	}
	if queriedPersonaID != "persona-beta" {
		t.Errorf("expected persona_id 'persona-beta', got %q", queriedPersonaID)
	}
}

func TestSend_AgentResolverFails_UsesUnscopedRecall(t *testing.T) {
	hub := newRecallTestHub()

	var queriedWsID, queriedPersonaID string
	hub.SetRecallFunc(func(ctx context.Context, query, workspaceID, personaID string) ([]types.MemoryContext, error) {
		queriedWsID = workspaceID
		queriedPersonaID = personaID
		return []types.MemoryContext{
			{Memory: types.Memory{ID: "m-unscoped", Content: "global memory"}, Score: 0.5, Source: "proactive"},
		}, nil
	})
	hub.SetAgentResolver(func(ctx context.Context, agentID string) (string, string, error) {
		return "", "", errors.New("persona not found")
	})

	ctx := context.Background()
	env := &types.MessageEnvelope{
		ID:      "msg-unscoped",
		From:    "agent-a",
		To:      "agent-b",
		Trust:   types.TrustInternal,
		Content: "should fallback to unscoped",
	}

	if err := hub.Send(ctx, env); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	// Resolver failed, so workspace and persona should be empty.
	if queriedWsID != "" {
		t.Errorf("expected empty workspace_id on resolver failure, got %q", queriedWsID)
	}
	if queriedPersonaID != "" {
		t.Errorf("expected empty persona_id on resolver failure, got %q", queriedPersonaID)
	}

	// Message should still be delivered with memory metadata.
	msgs := hub.Receive("agent-b", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata == nil {
		t.Fatal("expected metadata with related_memories")
	}
	if _, ok := msgs[0].Metadata["related_memories"]; !ok {
		t.Error("expected related_memories to be set even with unscoped recall")
	}
}
