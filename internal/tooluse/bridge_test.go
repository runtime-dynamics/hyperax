package tooluse

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// ── ProcessMessage tests ────────────────────────────────────────────────────

func TestBridge_ProcessMessage_SimpleCompletion(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIFinalResponse("Hello from the bridge!"),
	})

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		UserMessage:  "Hi",
		PersonaID:    "agent-1",
	}

	result, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.Content != "Hello from the bridge!" {
		t.Errorf("content = %s", result.Response.Content)
	}
}

func TestBridge_ProcessMessage_WithToolUse(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	bridge := NewBridge(resolver, mockDispatch("task_data"), slog.Default())

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
		}),
		openAIFinalResponse("Here are the tasks."),
	})

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		UserMessage:  "Show tasks",
		PersonaID:    "agent-1",
	}

	result, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
	if result.Response.Content != "Here are the tasks." {
		t.Errorf("content = %s", result.Response.Content)
	}
}

func TestBridge_ProcessMessage_WithSystemPrompt(t *testing.T) {
	resolver := NewResolver(nil) // No tools → passthrough
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	var capturedReq *provider.CompletionRequest
	completeFn := func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		capturedReq = req
		return openAIFinalResponse("OK"), nil
	}

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		SystemPrompt: "You are a helpful assistant.",
		UserMessage:  "Hi",
		PersonaID:    "agent-1",
	}

	_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "system" {
		t.Errorf("message[0].role = %s, want system", capturedReq.Messages[0].Role)
	}
	if capturedReq.Messages[0].Content != "You are a helpful assistant." {
		t.Errorf("message[0].content = %s", capturedReq.Messages[0].Content)
	}
	if capturedReq.Messages[1].Role != "user" {
		t.Errorf("message[1].role = %s, want user", capturedReq.Messages[1].Role)
	}
}

func TestBridge_ProcessMessage_WithHistory(t *testing.T) {
	resolver := NewResolver(nil)
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	var capturedReq *provider.CompletionRequest
	completeFn := func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		capturedReq = req
		return openAIFinalResponse("OK"), nil
	}

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		SystemPrompt: "Be helpful.",
		History: []provider.ChatMessage{
			{Role: "user", Content: "Previous question"},
			{Role: "assistant", Content: "Previous answer"},
		},
		UserMessage: "New question",
		PersonaID:   "agent-1",
	}

	_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// system + 2 history + user = 4
	if len(capturedReq.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "system" {
		t.Errorf("messages[0].role = %s", capturedReq.Messages[0].Role)
	}
	if capturedReq.Messages[1].Content != "Previous question" {
		t.Errorf("messages[1].content = %s", capturedReq.Messages[1].Content)
	}
	if capturedReq.Messages[3].Content != "New question" {
		t.Errorf("messages[3].content = %s", capturedReq.Messages[3].Content)
	}
}

func TestBridge_ProcessMessage_BadProviderKind(t *testing.T) {
	resolver := NewResolver(nil)
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIFinalResponse("OK"),
	})

	cfg := ProcessMessageConfig{
		ProviderKind: "nonexistent",
		Model:        "model",
		UserMessage:  "Hi",
	}

	_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err == nil {
		t.Fatal("expected error for bad provider kind")
	}
}

func TestBridge_ProcessMessage_AllProviderKinds(t *testing.T) {
	kinds := []string{"anthropic", "openai", "ollama", "azure", "custom", "google", "bedrock"}
	resolver := NewResolver(nil)

	for _, kind := range kinds {
		bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())
		completeFn := mockCompletionFn([]*provider.CompletionResponse{
			openAIFinalResponse("OK"),
		})

		cfg := ProcessMessageConfig{
			ProviderKind: kind,
			Model:        "model",
			UserMessage:  "Hi",
		}

		_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
		if err != nil {
			t.Errorf("provider %q: unexpected error: %v", kind, err)
		}
	}
}

// ── Delegation scope propagation ────────────────────────────────────────────

func TestResolveDelegationScopes_ActiveScopeAccess(t *testing.T) {
	delegations := []types.Delegation{
		{
			ID:        "d1",
			GrantType: types.GrantScopeAccess,
			Scopes:    []string{"tools:admin:*", "tools:write:create_pipeline"},
		},
		{
			ID:        "d2",
			GrantType: types.GrantScopeAccess,
			Scopes:    []string{"tools:execute:run_pipeline"},
		},
	}

	scopes := ResolveDelegationScopes(delegations)
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes, got %d: %v", len(scopes), scopes)
	}
}

func TestResolveDelegationScopes_SkipsRevoked(t *testing.T) {
	delegations := []types.Delegation{
		{
			ID:        "d1",
			GrantType: types.GrantScopeAccess,
			Scopes:    []string{"tools:admin:*"},
			RevokedAt: "2026-01-01T00:00:00Z",
		},
	}

	scopes := ResolveDelegationScopes(delegations)
	if len(scopes) != 0 {
		t.Fatalf("expected 0 scopes (revoked), got %d", len(scopes))
	}
}

func TestResolveDelegationScopes_SkipsExpired(t *testing.T) {
	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	delegations := []types.Delegation{
		{
			ID:        "d1",
			GrantType: types.GrantScopeAccess,
			Scopes:    []string{"tools:admin:*"},
			ExpiresAt: past,
		},
	}

	scopes := ResolveDelegationScopes(delegations)
	if len(scopes) != 0 {
		t.Fatalf("expected 0 scopes (expired), got %d", len(scopes))
	}
}

func TestResolveDelegationScopes_SkipsNonScopeGrants(t *testing.T) {
	delegations := []types.Delegation{
		{
			ID:            "d1",
			GrantType:     types.GrantClearanceElevation,
			ElevatedLevel: 2,
		},
		{
			ID:        "d2",
			GrantType: types.GrantCredentialPassthrough,
		},
	}

	scopes := ResolveDelegationScopes(delegations)
	if len(scopes) != 0 {
		t.Fatalf("expected 0 scopes (non-scope grants), got %d", len(scopes))
	}
}

func TestResolveDelegationScopes_Empty(t *testing.T) {
	scopes := ResolveDelegationScopes(nil)
	if scopes != nil {
		t.Errorf("expected nil, got %v", scopes)
	}
}

func TestBridge_DelegationScopePropagation(t *testing.T) {
	// Verify that delegation scopes flow through to the resolver.
	// Schema: set_config requires clearance 2, action admin.
	schemas := []ToolSchema{
		{Name: "list_tasks", Description: "List", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: true},
		{Name: "set_config", Description: "Config", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 2, RequiredAction: "admin", ExposedToLLM: true},
	}
	resolver := NewResolver(schemas)
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	var capturedReq *provider.CompletionRequest
	completeFn := func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		capturedReq = req
		return openAIFinalResponse("OK"), nil
	}

	// Clearance 0, but delegation grants admin:* — should see both tools.
	cfg := ProcessMessageConfig{
		ProviderKind:     "openai",
		Model:            "gpt-4o",
		UserMessage:      "Config",
		ClearanceLevel:   0,
		DelegationScopes: []string{"tools:admin:*"},
		PersonaID:        "agent-1",
	}

	_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tools should have been formatted and included in the request.
	if capturedReq.Tools == nil {
		t.Fatal("expected tools in request (delegation should grant access)")
	}

	// Verify both tools are in the formatted output.
	var tools []map[string]any
	if err := json.Unmarshal(capturedReq.Tools, &tools); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

// ── Event emission ──────────────────────────────────────────────────────────

func TestNewEventEmitter_PublishesEvents(t *testing.T) {
	bus := nervous.NewEventBus(16)
	sub := bus.Subscribe("test", nil)

	emitter := NewEventEmitter(bus, "agent-1")
	emitter(types.EventToolUseLoopStart, map[string]string{"test": "value"})

	select {
	case event := <-sub.Ch:
		if event.Type != types.EventToolUseLoopStart {
			t.Errorf("event type = %s", event.Type)
		}
		if event.Source != "tooluse" {
			t.Errorf("source = %s", event.Source)
		}
		if event.Scope != "agent-1" {
			t.Errorf("scope = %s", event.Scope)
		}
		// Verify payload.
		var payload map[string]string
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["test"] != "value" {
			t.Errorf("payload[test] = %s", payload["test"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBridge_ProcessMessage_EventEmission(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.Subscribe("test", nil)

	resolver := NewResolver(executorTestSchemas())
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
		}),
		openAIFinalResponse("Done."),
	})

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		UserMessage:  "Events",
		PersonaID:    "agent-1",
		Bus:          bus,
	}

	_, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain events.
	var events []types.EventType
	timeout := time.After(time.Second)
	for {
		select {
		case event := <-sub.Ch:
			events = append(events, event.Type)
		case <-timeout:
			goto done
		}
	}
done:

	expected := []types.EventType{
		types.EventToolUseLoopStart,
		types.EventToolUseLoopIteration,
		types.EventToolUseToolDispatch,
		types.EventToolUseLoopComplete,
	}
	if len(events) != len(expected) {
		t.Fatalf("events = %v, want %v", events, expected)
	}
	for i, e := range expected {
		if events[i] != e {
			t.Errorf("events[%d] = %s, want %s", i, events[i], e)
		}
	}
}

func TestBridge_ProcessMessage_NoEventBus(t *testing.T) {
	// No bus configured should not panic.
	resolver := NewResolver(executorTestSchemas())
	bridge := NewBridge(resolver, mockDispatch("ok"), slog.Default())

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
		}),
		openAIFinalResponse("Done."),
	})

	cfg := ProcessMessageConfig{
		ProviderKind: "openai",
		Model:        "gpt-4o",
		UserMessage:  "No bus",
		PersonaID:    "agent-1",
		// Bus is nil.
	}

	result, err := bridge.ProcessMessageWithCompleteFn(context.Background(), cfg, completeFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.Content != "Done." {
		t.Errorf("content = %s", result.Response.Content)
	}
}

// ── Nil logger ──────────────────────────────────────────────────────────────

func TestNewBridge_NilLogger(t *testing.T) {
	bridge := NewBridge(NewResolver(nil), mockDispatch("ok"), nil)
	if bridge.logger == nil {
		t.Error("expected default logger when nil passed")
	}
}
