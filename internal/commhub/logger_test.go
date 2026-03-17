package commhub

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func newTestCommLogger() (*CommLogger, *mockCommHubRepo) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockCommHubRepo()
	cl := NewCommLogger(repo, logger)
	return cl, repo
}

func TestCommLogger_Log_PersistsEntry(t *testing.T) {
	cl, repo := newTestCommLogger()
	ctx := context.Background()

	env := &types.MessageEnvelope{
		ID:          "msg-001",
		From:        "agent-a",
		To:          "agent-b",
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     "Hello from A",
	}

	if err := cl.Log(ctx, env, "sent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.mu.RLock()
	defer repo.mu.RUnlock()

	if len(repo.logEntries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(repo.logEntries))
	}

	entry := repo.logEntries[0]
	if entry.FromAgent != "agent-a" {
		t.Errorf("from_agent = %q, want %q", entry.FromAgent, "agent-a")
	}
	if entry.ToAgent != "agent-b" {
		t.Errorf("to_agent = %q, want %q", entry.ToAgent, "agent-b")
	}
	if entry.Direction != "sent" {
		t.Errorf("direction = %q, want %q", entry.Direction, "sent")
	}
	if entry.Trust != "internal" {
		t.Errorf("trust = %q, want %q", entry.Trust, "internal")
	}
	if entry.ContentType != "text" {
		t.Errorf("content_type = %q, want %q", entry.ContentType, "text")
	}
	if entry.Content != "Hello from A" {
		t.Errorf("content = %q, want %q", entry.Content, "Hello from A")
	}
}

func TestCommLogger_Log_BounceDirection(t *testing.T) {
	cl, repo := newTestCommLogger()
	ctx := context.Background()

	env := &types.MessageEnvelope{
		ID:      "msg-bounce",
		From:    "agent-x",
		To:      "agent-y",
		Trust:   types.TrustExternal,
		Content: "rejected message",
	}

	if err := cl.Log(ctx, env, "bounced"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.mu.RLock()
	defer repo.mu.RUnlock()

	if len(repo.logEntries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(repo.logEntries))
	}

	if repo.logEntries[0].Direction != "bounced" {
		t.Errorf("direction = %q, want %q", repo.logEntries[0].Direction, "bounced")
	}
	if repo.logEntries[0].Trust != "external" {
		t.Errorf("trust = %q, want %q", repo.logEntries[0].Trust, "external")
	}
}

func TestCommLogger_Log_NilRepoSkips(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cl := NewCommLogger(nil, logger)

	ctx := context.Background()
	env := &types.MessageEnvelope{
		From: "a", To: "b", Content: "test",
	}

	if err := cl.Log(ctx, env, "sent"); err != nil {
		t.Errorf("expected nil error for nil repo, got: %v", err)
	}
}

func TestCommLogger_GetLog(t *testing.T) {
	cl, repo := newTestCommLogger()
	ctx := context.Background()

	// Manually add entries via repo.
	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-a", ToAgent: "agent-b", Direction: "sent",
		ContentType: "text", Content: "msg-1", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 1: %v", err)
	}
	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-c", ToAgent: "agent-a", Direction: "sent",
		ContentType: "text", Content: "msg-2", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 2: %v", err)
	}
	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-c", ToAgent: "agent-d", Direction: "sent",
		ContentType: "text", Content: "msg-3", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 3: %v", err)
	}

	// agent-a involved in 2 messages.
	entries, err := cl.GetLog(ctx, "agent-a", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for agent-a, got %d", len(entries))
	}
}

func TestCommLogger_GetLogBetween(t *testing.T) {
	cl, repo := newTestCommLogger()
	ctx := context.Background()

	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-a", ToAgent: "agent-b", Direction: "sent",
		ContentType: "text", Content: "hello", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 1: %v", err)
	}
	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-b", ToAgent: "agent-a", Direction: "sent",
		ContentType: "text", Content: "hi back", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 2: %v", err)
	}
	if err := repo.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-a", ToAgent: "agent-c", Direction: "sent",
		ContentType: "text", Content: "different pair", Trust: "internal",
	}); err != nil {
		t.Fatalf("log message 3: %v", err)
	}

	entries, err := cl.GetLogBetween(ctx, "agent-a", "agent-b", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries between agent-a and agent-b, got %d", len(entries))
	}
}

func TestCommLogger_GetLog_NilRepoReturnsNil(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cl := NewCommLogger(nil, logger)

	entries, err := cl.GetLog(context.Background(), "agent-a", 10)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got: %v", entries)
	}
}

func TestCommLogger_MultipleEntries(t *testing.T) {
	cl, _ := newTestCommLogger() //nolint:dogsled // repo not needed in this test
	ctx := context.Background()

	// Log multiple messages.
	for i := 0; i < 5; i++ {
		env := &types.MessageEnvelope{
			From:        "sender",
			To:          "receiver",
			Trust:       types.TrustInternal,
			ContentType: "text",
			Content:     "message",
		}
		if err := cl.Log(ctx, env, "sent"); err != nil {
			t.Fatalf("log %d: %v", i, err)
		}
	}

	entries, err := cl.GetLog(ctx, "sender", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}
