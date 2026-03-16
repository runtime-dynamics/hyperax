package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestChatCompletion_Ollama verifies the Ollama chat completion flow including
// request format, response parsing, and model passthrough.
func TestChatCompletion_Ollama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/chat") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "llama3" {
			t.Errorf("expected model llama3, got %s", body.Model)
		}
		if body.Stream {
			t.Error("expected stream=false")
		}
		if len(body.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(body.Messages))
		}

		resp := ollamaNativeResponse{Model: "llama3"}
		resp.Message.Role = "assistant"
		resp.Message.Content = "Hello from Ollama!"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	resp, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "ollama",
		BaseURL: srv.URL,
		Model:   "llama3",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Ollama!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.Model != "llama3" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
}

// TestChatCompletion_OpenAI verifies the OpenAI SDK chat completion flow including
// Bearer auth, request format, choice parsing, and usage stats.
func TestChatCompletion_OpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key-123" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		// The SDK sends its own serialized body — decode as generic map.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if model, _ := body["model"].(string); model != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %s", model)
		}

		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": "Hello from OpenAI!"},
					"finish_reason": "stop",
				},
			},
			"model": "gpt-4o",
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	resp, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "openai",
		BaseURL: srv.URL,
		APIKey:  "test-key-123",
		Model:   "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from OpenAI!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage info")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected 5 completion tokens, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

// TestChatCompletion_Anthropic verifies the Anthropic SDK Messages API flow including
// system prompt extraction, x-api-key auth, prompt caching, and content block parsing.
func TestChatCompletion_Anthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		apiKey := r.Header.Get("x-api-key")
		if apiKey != "ant-key-456" {
			t.Errorf("unexpected x-api-key: %s", apiKey)
		}

		// The SDK sends the body — decode as generic map to validate structure.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		// Verify system field is present (SDK extracts system messages to top-level field).
		systemField, ok := body["system"]
		if !ok {
			t.Fatal("expected system field in request body")
		}
		systemBlocks, ok := systemField.([]any)
		if !ok || len(systemBlocks) < 1 {
			t.Fatalf("expected at least 1 system block, got %v", systemField)
		}

		// Verify model is set.
		if model, _ := body["model"].(string); model != "claude-sonnet-4-20250514" {
			t.Errorf("expected model claude-sonnet-4-20250514, got %s", model)
		}

		// Verify max_tokens is set.
		if maxTokens, _ := body["max_tokens"].(float64); maxTokens != 4096 {
			t.Errorf("expected max_tokens=4096, got %v", maxTokens)
		}

		// Verify messages array does not contain system messages.
		messages, _ := body["messages"].([]any)
		for _, m := range messages {
			msg, _ := m.(map[string]any)
			if msg["role"] == "system" {
				t.Error("system message should not be in messages array for Anthropic")
			}
		}

		if len(messages) != 1 {
			t.Errorf("expected 1 non-system message, got %d", len(messages))
		}

		resp := map[string]any{
			"id":   "msg-test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Hello from Anthropic!"},
			},
			"model":        "claude-sonnet-4-20250514",
			"stop_reason":  "end_turn",
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                12,
				"output_tokens":               8,
				"cache_creation_input_tokens": 10,
				"cache_read_input_tokens":     2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	resp, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "anthropic",
		BaseURL: srv.URL,
		APIKey:  "ant-key-456",
		Model:   "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Anthropic!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage info")
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("expected 12 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("expected 8 completion tokens, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 20 {
		t.Errorf("expected 20 total tokens, got %d", resp.Usage.TotalTokens)
	}
	if resp.Usage.CacheCreationTokens != 10 {
		t.Errorf("expected 10 cache creation tokens, got %d", resp.Usage.CacheCreationTokens)
	}
	if resp.Usage.CacheReadTokens != 2 {
		t.Errorf("expected 2 cache read tokens, got %d", resp.Usage.CacheReadTokens)
	}
}

// TestChatCompletion_Azure verifies the Azure OpenAI Service completion flow
// including the deployment-based URL pattern and api-key header auth.
func TestChatCompletion_Azure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify Azure deployment URL pattern (SDK appends /chat/completions).
		if !strings.Contains(r.URL.Path, "/openai/deployments/gpt-4o/chat/completions") {
			t.Errorf("unexpected path: got %s", r.URL.Path)
		}

		apiVersion := r.URL.Query().Get("api-version")
		if apiVersion != "2024-02-01" {
			t.Errorf("unexpected api-version: %s", apiVersion)
		}

		apiKey := r.Header.Get("api-key")
		if apiKey != "azure-key-789" {
			t.Errorf("unexpected api-key header: %s", apiKey)
		}

		resp := map[string]any{
			"id":      "chatcmpl-azure-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": "Hello from Azure!"},
					"finish_reason": "stop",
				},
			},
			"model": "gpt-4o",
			"usage": map[string]any{
				"prompt_tokens":     8,
				"completion_tokens": 4,
				"total_tokens":      12,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	resp, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "azure",
		BaseURL: srv.URL,
		APIKey:  "azure-key-789",
		Model:   "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Azure!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
}

// TestChatCompletion_Custom verifies that custom providers use the OpenAI-compatible format.
func TestChatCompletion_Custom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("custom provider should use OpenAI path, got: %s", r.URL.Path)
		}

		resp := map[string]any{
			"id":      "chatcmpl-custom-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": "Hello from Custom!"},
					"finish_reason": "stop",
				},
			},
			"model": "custom-model",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	resp, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "custom",
		BaseURL: srv.URL,
		Model:   "custom-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Custom!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

// TestChatCompletion_ErrorNon2xx verifies that non-2xx HTTP responses produce
// descriptive errors when using SDK-based providers.
func TestChatCompletion_ErrorNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
	}))
	defer srv.Close()

	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "openai",
		BaseURL: srv.URL,
		APIKey:  "key",
		Model:   "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	// The SDK wraps error details — verify error is propagated.
	if !strings.Contains(err.Error(), "429") && !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should reference rate limit or 429: %v", err)
	}
}

// TestChatCompletion_Timeout verifies that the completion client respects context
// cancellation (simulating a timeout).
func TestChatCompletion_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow provider.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ChatCompletion(ctx, &CompletionRequest{
		Kind:    "ollama",
		BaseURL: srv.URL,
		Model:   "llama3",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context error, got: %v", err)
	}
}

// TestChatCompletion_MalformedResponse verifies that a malformed JSON response
// produces a descriptive parse error.
func TestChatCompletion_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	// Use Ollama for this test since it does direct HTTP parsing (SDKs have
	// their own error handling for malformed responses).
	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "ollama",
		BaseURL: srv.URL,
		Model:   "llama3",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected parse error for malformed response")
	}
	if !strings.Contains(err.Error(), "parse native response") && !strings.Contains(err.Error(), "parse OpenAI-format response") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

// TestChatCompletion_NilRequest verifies that a nil request produces a clear error.
func TestChatCompletion_NilRequest(t *testing.T) {
	_, err := ChatCompletion(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestChatCompletion_EmptyMessages verifies that a request with no messages
// produces a validation error before any HTTP call.
func TestChatCompletion_EmptyMessages(t *testing.T) {
	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "openai",
		BaseURL: "http://localhost",
		Model:   "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
	if !strings.Contains(err.Error(), "at least one message") {
		t.Errorf("expected messages error, got: %v", err)
	}
}

// TestChatCompletion_UnsupportedKind verifies that an unsupported provider kind
// produces a descriptive error.
func TestChatCompletion_UnsupportedKind(t *testing.T) {
	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "unknown-provider",
		BaseURL: "http://localhost",
		Model:   "model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
	if !strings.Contains(err.Error(), "unsupported provider kind") {
		t.Errorf("expected unsupported kind error, got: %v", err)
	}
}

// TestChatCompletion_OpenAI_NoChoices verifies that an OpenAI response with
// an empty choices array produces a descriptive error.
func TestChatCompletion_OpenAI_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"choices": []map[string]any{},
			"model":   "gpt-4o",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "openai",
		BaseURL: srv.URL,
		APIKey:  "key",
		Model:   "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected no choices error, got: %v", err)
	}
}

// TestChatCompletion_Anthropic_MultipleSystemMessages verifies that multiple
// system messages are represented as separate blocks in the system field,
// when using the Anthropic SDK.
func TestChatCompletion_Anthropic_MultipleSystemMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the SDK-serialized body to validate system blocks.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		// Multiple system messages should produce multiple system blocks.
		systemField, ok := body["system"]
		if !ok {
			t.Fatal("expected system field in request body")
		}
		systemBlocks, ok := systemField.([]any)
		if !ok {
			t.Fatalf("expected system field to be array, got %T", systemField)
		}
		if len(systemBlocks) != 2 {
			t.Fatalf("expected 2 system blocks, got %d", len(systemBlocks))
		}

		// Verify the first block text.
		block0, _ := systemBlocks[0].(map[string]any)
		if text, _ := block0["text"].(string); text != "You are helpful." {
			t.Errorf("expected first system block text %q, got %q", "You are helpful.", text)
		}

		// Verify the second block text.
		block1, _ := systemBlocks[1].(map[string]any)
		if text, _ := block1["text"].(string); text != "Your name is Planner." {
			t.Errorf("expected second system block text %q, got %q", "Your name is Planner.", text)
		}

		// Only the last block should have cache_control.
		if _, hasCacheCtl := block0["cache_control"]; hasCacheCtl {
			t.Error("first system block should not have cache_control")
		}
		if cc, ok := block1["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
			t.Error("last system block should have cache_control=ephemeral")
		}

		// Verify messages array.
		messages, _ := body["messages"].([]any)
		if len(messages) != 1 {
			t.Errorf("expected 1 non-system message, got %d", len(messages))
		}

		resp := map[string]any{
			"id":   "msg-test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "OK"},
			},
			"model":        "claude-sonnet-4-20250514",
			"stop_reason":  "end_turn",
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	_, err := ChatCompletion(context.Background(), &CompletionRequest{
		Kind:    "anthropic",
		BaseURL: srv.URL,
		APIKey:  "key",
		Model:   "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "system", Content: "Your name is Planner."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
