//go:build e2e

// Package e2e contains end-to-end integration tests that exercise the full
// Hyperax stack from HTTP request through database and back. These tests use
// real SQLite databases (in-memory) and the full router wiring to validate
// cross-component interactions.
//
// Run with: go test -tags e2e -count=1 ./internal/e2e/...
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hyperax/hyperax/internal/app"
	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/hyperax/hyperax/internal/web"
	"github.com/hyperax/hyperax/pkg/types"
)

// testHarness encapsulates the full application stack for E2E tests.
// Each test gets its own isolated in-memory SQLite database.
type testHarness struct {
	app    *app.HyperaxApp
	server *httptest.Server
	bus    *nervous.EventBus
}

// newTestHarness creates a fully-wired Hyperax application with an in-memory
// SQLite database, mock UI filesystem, and httptest server.
func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// In-memory SQLite databases are per-connection. Constrain the pool to
	// a single connection so all queries hit the same migrated database.
	db.SqlDB().SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore()
	bus := nervous.NewEventBus(256)

	tmpDir := t.TempDir()
	bootstrap := &config.BootstrapConfig{
		ListenAddr:      ":0",
		DataDir:         tmpDir,
		OrgWorkspaceDir: tmpDir,
		Storage: config.BootstrapStorage{
			Backend: "sqlite",
			DSN:     ":memory:",
		},
		LogLevel: "warn",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application := app.New(bootstrap, store, bus, logger)

	mockUI := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>test</html>")},
	}

	router := web.BuildRouter(application, mockUI, db.SqlDB())
	application.SetRouter(router)

	server := httptest.NewServer(router)

	t.Cleanup(func() {
		server.Close()
		_ = store.Close()
	})

	return &testHarness{
		app:    application,
		server: server,
		bus:    bus,
	}
}

// collectEvents subscribes to all EventBus events and collects them into a
// slice. Returns a function to retrieve collected events (thread-safe).
// The caller must call busCancel() when done to stop collection.
func collectEvents(t *testing.T, bus *nervous.EventBus) (getEvents func() []types.NervousEvent, waitFor func(eventType types.EventType, timeout time.Duration) bool) {
	t.Helper()

	sub := bus.Subscribe("e2e-test-collector", nil)
	var collected []types.NervousEvent
	var mu sync.Mutex

	go func() {
		for ev := range sub.Ch {
			mu.Lock()
			collected = append(collected, ev)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		bus.Unsubscribe("e2e-test-collector")
	})

	getEvents = func() []types.NervousEvent {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]types.NervousEvent, len(collected))
		copy(cp, collected)
		return cp
	}

	waitFor = func(eventType types.EventType, timeout time.Duration) bool {
		deadline := time.After(timeout)
		for {
			select {
			case <-deadline:
				return false
			case <-time.After(20 * time.Millisecond):
				mu.Lock()
				for _, ev := range collected {
					if ev.Type == eventType {
						mu.Unlock()
						return true
					}
				}
				mu.Unlock()
			}
		}
	}

	return getEvents, waitFor
}

// TestE2E_PersonaProviderChatToolUseAudit exercises the full chat completion
// pipeline: create an agent with clearance 1, configure a mock LLM provider,
// send a chat message via the REST API, verify the tool-use bridge is invoked
// (via a mock provider that returns a tool_call then a final response), and
// confirm that the expected EventBus events fire (chat.completion.start,
// chat.completion.done).
func TestE2E_PersonaProviderChatToolUseAudit(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// --- 1. Create a mock LLM provider HTTP server ---
	callCount := 0
	var mu sync.Mutex
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		current := callCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if current == 1 {
			// First call: return a tool_call response (OpenAI format).
			resp := map[string]any{
				"id":      "chatcmpl-test-1",
				"object":  "chat.completion",
				"model":   "test-model",
				"choices": []map[string]any{
					{
						"index":         0,
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]any{
								{
									"id":   "call_test_1",
									"type": "function",
									"function": map[string]any{
										"name":      "list_workspaces",
										"arguments": "{}",
									},
								},
							},
						},
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     100,
					"completion_tokens": 20,
					"total_tokens":      120,
				},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		// Subsequent calls: return final text response.
		resp := map[string]any{
			"id":      "chatcmpl-test-2",
			"object":  "chat.completion",
			"model":   "test-model",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "I found 1 workspace. How can I help you?",
					},
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     150,
				"completion_tokens": 15,
				"total_tokens":      165,
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer mockLLM.Close()

	// --- 2. Create a provider pointing to the mock LLM ---
	providerID, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "test-provider",
		Kind:      "openai",
		BaseURL:   mockLLM.URL,
		IsDefault: true,
		IsEnabled: true,
		Models:    `["test-model"]`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// --- 3. Create an agent with clearance 1, linked to the provider ---
	agentID, err := h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "TestAssistant",
		Personality:    "E2E test assistant",
		ClearanceLevel: 1,
		ProviderID:     providerID,
		DefaultModel:   "test-model",
		SystemPrompt:   "You are a helpful test assistant.",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if agentID == "" {
		t.Fatal("agent ID is empty")
	}

	// --- 4. Subscribe to EventBus to capture audit trail events ---
	getEvents, waitFor := collectEvents(t, h.bus)

	// --- 5. Send a chat message via REST API ---
	chatBody := map[string]string{
		"from":    "user:test",
		"to":      "TestAssistant",
		"content": "What workspaces are available?",
	}
	bodyJSON, err := json.Marshal(chatBody)
	if err != nil {
		t.Fatalf("marshal chat body: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/send: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var sendResult map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&sendResult); err != nil {
		t.Fatalf("decode send response: %v", err)
	}

	if sendResult["status"] != "delivered" {
		t.Errorf("expected status=delivered, got %q", sendResult["status"])
	}

	// --- 6. Wait for the async LLM completion to finish ---
	if !waitFor(types.EventChatCompletionDone, 10*time.Second) {
		t.Fatal("timed out waiting for chat.completion.done event")
	}

	// Small grace period for remaining events.
	time.Sleep(100 * time.Millisecond)

	// --- 7. Verify audit trail events ---
	events := getEvents()
	foundStart := false
	foundDone := false
	for _, ev := range events {
		switch ev.Type {
		case types.EventChatCompletionStart:
			foundStart = true
			var payload map[string]string
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["agent"] != "TestAssistant" {
					t.Errorf("completion start: agent=%q, want TestAssistant", payload["agent"])
				}
				if payload["provider"] != "test-provider" {
					t.Errorf("completion start: provider=%q, want test-provider", payload["provider"])
				}
			}
		case types.EventChatCompletionDone:
			foundDone = true
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["agent"] != "TestAssistant" {
					t.Errorf("completion done: agent=%v, want TestAssistant", payload["agent"])
				}
				if iters, ok := payload["tool_use_iterations"]; ok {
					if v, ok := iters.(float64); ok && v < 1 {
						t.Errorf("expected tool_use_iterations >= 1, got %v", v)
					}
				}
			}
		}
	}

	if !foundStart {
		t.Error("missing chat.completion.start event on EventBus")
	}
	if !foundDone {
		t.Error("missing chat.completion.done event on EventBus")
	}

	// --- 8. Verify the mock LLM was called at least twice (tool call + final) ---
	mu.Lock()
	finalCallCount := callCount
	mu.Unlock()
	if finalCallCount < 2 {
		t.Errorf("expected mock LLM to be called >= 2 times (tool-use loop), got %d", finalCallCount)
	}
}

// TestE2E_ChatSendReturnsDelivered verifies the basic happy path: sending a
// chat message returns status=delivered even when the target is not a
// configured agent (no LLM completion triggered).
func TestE2E_ChatSendReturnsDelivered(t *testing.T) {
	h := newTestHarness(t)

	chatBody := map[string]string{
		"from":    "user:alice",
		"to":      "user:bob",
		"content": "Hello Bob!",
	}
	bodyJSON, err := json.Marshal(chatBody)
	if err != nil {
		t.Fatalf("marshal chat body: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/send: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if result["status"] != "delivered" {
		t.Errorf("expected status=delivered, got %q", result["status"])
	}
}

// TestE2E_HealthEndpoint verifies the /health endpoint returns 200 OK.
func TestE2E_HealthEndpoint(t *testing.T) {
	h := newTestHarness(t)

	resp, err := http.Get(h.server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

// TestE2E_StatusEndpoint verifies the /api/status endpoint returns valid JSON
// with expected fields.
func TestE2E_StatusEndpoint(t *testing.T) {
	h := newTestHarness(t)

	resp, err := http.Get(h.server.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	for _, field := range []string{"version", "storage", "tool_count", "uptime_seconds"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in /api/status response", field)
		}
	}

	if tc, ok := body["tool_count"].(float64); ok && tc == 0 {
		t.Error("tool_count should be > 0")
	}
}

// TestE2E_ChatMissingFields verifies that the chat endpoint rejects incomplete
// requests with 400 Bad Request.
func TestE2E_ChatMissingFields(t *testing.T) {
	h := newTestHarness(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing_from", map[string]string{"to": "bob", "content": "hi"}},
		{"missing_to", map[string]string{"from": "alice", "content": "hi"}},
		{"missing_content", map[string]string{"from": "alice", "to": "bob"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyJSON, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

// TestE2E_DelegationToDisabledProvider verifies that when an agent's provider
// is disabled, an auto-reply is sent and delegation is attempted.
func TestE2E_DelegationToDisabledProvider(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// --- Setup: Create providers and agents ---
	provider1, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "disabled-provider",
		Kind:      "openai",
		BaseURL:   "http://disabled",
		IsDefault: false,
		IsEnabled: false, // DISABLED
		Models:    `["gpt-4"]`,
	})
	if err != nil {
		t.Fatalf("create disabled provider: %v", err)
	}

	provider2, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "enabled-provider",
		Kind:      "openai",
		BaseURL:   "http://enabled",
		IsDefault: true,
		IsEnabled: true,
		Models:    `["gpt-4"]`,
	})
	if err != nil {
		t.Fatalf("create enabled provider: %v", err)
	}

	// Create disabled agent (using disabled provider)
	_, err = h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "DisabledAgent",
		Personality:    "Has disabled provider",
		ClearanceLevel: 1,
		ProviderID:     provider1,
		DefaultModel:   "gpt-4",
		Status:         "idle",
	})
	if err != nil {
		t.Fatalf("create disabled agent: %v", err)
	}

	// Create child agent (capable delegate)
	_, err = h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "ChildDelegate",
		Personality:    "Capable backup",
		ClearanceLevel: 1,
		ProviderID:     provider2,
		DefaultModel:   "gpt-4",
		Status:         "idle",
	})
	if err != nil {
		t.Fatalf("create child delegate agent: %v", err)
	}

	// Setup hierarchy: DisabledAgent -> ChildDelegate
	if err := h.app.Store.CommHub.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "DisabledAgent",
		ChildAgent:  "ChildDelegate",
	}); err != nil {
		t.Fatalf("set relationship: %v", err)
	}

	// Subscribe to events
	getEvents, waitFor := collectEvents(t, h.bus)

	// --- Send message to disabled agent ---
	chatBody := map[string]string{
		"from":    "user:test",
		"to":      "DisabledAgent",
		"content": "Please help me",
	}
	bodyJSON, err := json.Marshal(chatBody)
	if err != nil {
		t.Fatalf("marshal chat body: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/send: %v", err)
	}
	defer resp.Body.Close()

	// Should return 200 OK (message delivered)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var sendResult map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&sendResult); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if sendResult["status"] != "delivered" {
		t.Errorf("expected delivered, got %q", sendResult["status"])
	}

	// --- Verify events were published ---
	if !waitFor(types.EventProviderUnavailable, 2*time.Second) {
		t.Error("expected EventProviderUnavailable event")
	}

	if !waitFor(types.EventDelegationAttempted, 2*time.Second) {
		t.Error("expected EventDelegationAttempted event")
	}

	// --- Verify agent was suspended ---
	agent, err := h.app.Store.Agents.GetByName(ctx, "DisabledAgent")
	if err != nil {
		t.Fatalf("get disabled agent: %v", err)
	}
	if agent.Status != "suspended" {
		t.Errorf("expected agent status=suspended, got %q", agent.Status)
	}
	if agent.StatusReason != "provider disabled" {
		t.Errorf("expected status_reason='provider disabled', got %q", agent.StatusReason)
	}

	// Give async delegation time to work
	time.Sleep(500 * time.Millisecond)

	_ = getEvents // Suppress unused warning
}

// TestE2E_DelegationFailureToTerminal verifies that when all agents in the
// hierarchy are unavailable, a terminal failure is reported.
func TestE2E_DelegationFailureToTerminal(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Create disabled provider
	disabledProv, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "disabled-provider",
		Kind:      "openai",
		BaseURL:   "http://disabled",
		IsEnabled: false,
		Models:    `["gpt-4"]`,
	})
	if err != nil {
		t.Fatalf("create disabled provider: %v", err)
	}

	// Create isolated agent (no children, no parent, disabled provider)
	_, err = h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:         "IsolatedDisabled",
		ProviderID:   disabledProv,
		DefaultModel: "gpt-4",
		Status:       "idle",
	})
	if err != nil {
		t.Fatalf("create isolated agent: %v", err)
	}

	getEvents, waitFor := collectEvents(t, h.bus)

	// Send message
	chatBody := map[string]string{
		"from":    "user:test",
		"to":      "IsolatedDisabled",
		"content": "Help me",
	}
	bodyJSON, err := json.Marshal(chatBody)
	if err != nil {
		t.Fatalf("marshal chat body: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/send: %v", err)
	}
	defer resp.Body.Close()

	// Should see delegation exhausted event
	if !waitFor(types.EventDelegationExhausted, 2*time.Second) {
		t.Error("expected EventDelegationExhausted event")
	}

	events := getEvents()
	foundExhausted := false
	for _, ev := range events {
		if ev.Type == types.EventDelegationExhausted {
			foundExhausted = true
			// Verify payload contains the agent name
			var payload map[string]string
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["agent"] != "IsolatedDisabled" {
					t.Errorf("exhausted event agent=%q, want IsolatedDisabled", payload["agent"])
				}
			}
		}
	}
	if !foundExhausted {
		t.Error("EventDelegationExhausted not found in events")
	}
}

// TestE2E_ProviderReEnable verifies that when a provider is re-enabled,
// all suspended agents with that provider are reactivated to idle.
func TestE2E_ProviderReEnable(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Create provider (enabled)
	provID, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "toggleprov",
		Kind:      "openai",
		BaseURL:   "http://toggle",
		IsEnabled: true,
		Models:    `["gpt-4"]`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Create agent
	agentID, err := h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "TestAgent",
		ProviderID:     provID,
		DefaultModel:   "gpt-4",
		Status:         "idle",
		ClearanceLevel: 1,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Manually suspend the agent with provider disabled reason
	if err := h.app.Store.Agents.Update(ctx, agentID, &repo.Agent{
		Name:           "TestAgent",
		ProviderID:     provID,
		DefaultModel:   "gpt-4",
		Status:         "suspended",
		StatusReason:   "provider disabled",
		ClearanceLevel: 1,
	}); err != nil {
		t.Fatalf("suspend agent: %v", err)
	}

	getEvents, waitFor := collectEvents(t, h.bus)

	// Disable the provider
	disabledProv := &repo.Provider{
		Name:      "toggleprov",
		Kind:      "openai",
		BaseURL:   "http://toggle",
		IsEnabled: false,
		Models:    `["gpt-4"]`,
	}
	if err := h.app.Store.Providers.Update(ctx, provID, disabledProv); err != nil {
		t.Fatalf("disable provider: %v", err)
	}

	// Re-enable the provider
	enabledProv := &repo.Provider{
		Name:      "toggleprov",
		Kind:      "openai",
		BaseURL:   "http://toggle",
		IsEnabled: true,
		Models:    `["gpt-4"]`,
	}
	if err := h.app.Store.Providers.Update(ctx, provID, enabledProv); err != nil {
		t.Fatalf("re-enable provider: %v", err)
	}

	// Wait for re-enable event
	if !waitFor(types.EventProviderReEnabled, 2*time.Second) {
		t.Error("expected EventProviderReEnabled event")
	}

	// Verify agent was reactivated to idle
	agent, err := h.app.Store.Agents.Get(ctx, agentID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected agent status=idle after provider re-enable, got %q", agent.Status)
	}
	if agent.StatusReason != "" {
		t.Errorf("expected status_reason='', got %q", agent.StatusReason)
	}

	events := getEvents()
	foundReEnable := false
	for _, ev := range events {
		if ev.Type == types.EventProviderReEnabled {
			foundReEnable = true
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if reactivated, ok := payload["reactivated"].(float64); ok {
					if reactivated < 1 {
						t.Errorf("expected reactivated count >= 1, got %v", reactivated)
					}
				}
			}
		}
	}
	if !foundReEnable {
		t.Error("EventProviderReEnabled not found in events")
	}
}

// TestE2E_NormalMessageFlowUnaffected verifies that messages to agents with
// enabled providers are not affected by the delegation logic.
func TestE2E_NormalMessageFlowUnaffected(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Create enabled provider
	provID, err := h.app.Store.Providers.Create(ctx, &repo.Provider{
		Name:      "normal-prov",
		Kind:      "openai",
		BaseURL:   "http://normal",
		IsEnabled: true,
		Models:    `["gpt-4"]`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Create agent with enabled provider
	_, err = h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:         "NormalAgent",
		ProviderID:   provID,
		DefaultModel: "gpt-4",
		Status:       "idle",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Send message (no delegation logic should trigger)
	chatBody := map[string]string{
		"from":    "user:test",
		"to":      "NormalAgent",
		"content": "Normal request",
	}
	bodyJSON, err := json.Marshal(chatBody)
	if err != nil {
		t.Fatalf("marshal chat body: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/api/v1/chat/send", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/send: %v", err)
	}
	defer resp.Body.Close()

	// Should succeed with status delivered
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if result["status"] != "delivered" {
		t.Errorf("expected delivered, got %q", result["status"])
	}

	// Agent should remain idle (no suspension)
	agent, err := h.app.Store.Agents.GetByName(ctx, "NormalAgent")
	if err != nil {
		t.Fatalf("get agent by name: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("normal agent status should remain idle, got %q", agent.Status)
	}
}
