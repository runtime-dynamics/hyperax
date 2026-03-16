package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// -- Test helpers -------------------------------------------------------------

// testSchemas returns schemas for the executor tests.
func executorTestSchemas() []ToolSchema {
	return []ToolSchema{
		{Name: "list_tasks", Description: "List tasks", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: true},
		{Name: "run_pipeline", Description: "Run pipeline", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: true},
	}
}

// mockDispatch returns a DispatchFunc that returns a canned result for any tool.
func mockDispatch(content string) DispatchFunc {
	return func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		return types.NewToolResult(map[string]string{"result": content}), nil
	}
}

// mockDispatchError returns a DispatchFunc that always errors.
func mockDispatchError(msg string) DispatchFunc {
	return func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		return nil, fmt.Errorf("%s", msg)
	}
}

// mockCompletionFn builds a CompletionFunc that returns responses from a
// sequence. Each call pops the next response. The first N-1 responses should
// have StopReason="tool_calls" and include tool calls in RawResponse, while
// the last response has StopReason="stop" with final text.
func mockCompletionFn(responses []*provider.CompletionResponse) CompletionFunc {
	idx := 0
	return func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		if idx >= len(responses) {
			return nil, fmt.Errorf("mock: no more responses (called %d times)", idx+1)
		}
		resp := responses[idx]
		idx++
		return resp, nil
	}
}

// openAIToolCallResponse builds an OpenAI-format response with tool calls.
func openAIToolCallResponse(calls []struct{ id, name, args string }) *provider.CompletionResponse {
	toolCalls := make([]map[string]any, len(calls))
	for i, c := range calls {
		toolCalls[i] = map[string]any{
			"id":   c.id,
			"type": "function",
			"function": map[string]string{
				"name":      c.name,
				"arguments": c.args,
			},
		}
	}
	raw, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content":    "I'll call a tool.",
					"tool_calls": toolCalls,
				},
				"finish_reason": "tool_calls",
			},
		},
		"model": "gpt-4o",
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	return &provider.CompletionResponse{
		Content:     "I'll call a tool.",
		Model:       "gpt-4o",
		StopReason:  "tool_calls",
		RawResponse: raw,
		Usage:       &provider.UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// openAIFinalResponse builds an OpenAI-format final response (no tool calls).
func openAIFinalResponse(content string) *provider.CompletionResponse {
	raw, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{
				"message":       map[string]string{"content": content},
				"finish_reason": "stop",
			},
		},
		"model": "gpt-4o",
		"usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 12, "total_tokens": 20},
	})
	return &provider.CompletionResponse{
		Content:     content,
		Model:       "gpt-4o",
		StopReason:  "stop",
		RawResponse: raw,
		Usage:       &provider.UsageInfo{PromptTokens: 8, CompletionTokens: 12, TotalTokens: 20},
	}
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestExecutor_NoToolsPassthrough(t *testing.T) {
	// When resolver returns no tools, execute should pass through directly.
	resolver := NewResolver(nil) // No schemas → no tools
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatch("ok")},
		adapter, resolver, slog.Default(),
	)

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIFinalResponse("Hello!"),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Hi"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.Content != "Hello!" {
		t.Errorf("content = %s", result.Response.Content)
	}
	if result.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", result.Iterations)
	}
}

func TestExecutor_SingleToolCall(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatch("task_data")},
		adapter, resolver, slog.Default(),
	)

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{"status":"pending"}`},
		}),
		openAIFinalResponse("Here are your tasks."),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "List my tasks"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.Content != "Here are your tasks." {
		t.Errorf("content = %s", result.Response.Content)
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
	// Cumulative usage: 10+8=18 prompt, 5+12=17 completion, 15+20=35 total.
	if result.TotalUsage.PromptTokens != 18 {
		t.Errorf("prompt tokens = %d, want 18", result.TotalUsage.PromptTokens)
	}
	if result.TotalUsage.TotalTokens != 35 {
		t.Errorf("total tokens = %d, want 35", result.TotalUsage.TotalTokens)
	}
}

func TestExecutor_MultiToolParallel(t *testing.T) {
	var dispatchedTools []string
	dispatch := func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		dispatchedTools = append(dispatchedTools, name)
		return types.NewToolResult(map[string]string{"ok": "true"}), nil
	}

	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: dispatch},
		adapter, resolver, slog.Default(),
	)

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
			{"call_2", "run_pipeline", `{"name":"build"}`},
		}),
		openAIFinalResponse("Done."),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Do both"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
	if len(dispatchedTools) != 2 {
		t.Fatalf("dispatched %d tools, want 2", len(dispatchedTools))
	}
	if dispatchedTools[0] != "list_tasks" || dispatchedTools[1] != "run_pipeline" {
		t.Errorf("dispatched = %v", dispatchedTools)
	}
}

func TestExecutor_MaxIterationsExceeded(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{
			MaxIterations: 2,
			Dispatch:      mockDispatch("ok"),
		},
		adapter, resolver, slog.Default(),
	)

	// All responses request tool use — never stops.
	toolResp := openAIToolCallResponse([]struct{ id, name, args string }{
		{"call_1", "list_tasks", `{"a":"1"}`},
	})
	toolResp2 := openAIToolCallResponse([]struct{ id, name, args string }{
		{"call_2", "list_tasks", `{"a":"2"}`}, // Different args to avoid cycle detection
	})

	completeFn := mockCompletionFn([]*provider.CompletionResponse{toolResp, toolResp2})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Loop"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The executor should return the last text response instead of erroring
	// when max iterations is reached and a text response is available.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Iterations != 2 {
		t.Errorf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.Response.Content == "" {
		t.Error("expected non-empty content in last text response")
	}
}

func TestExecutor_DispatchError(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatchError("tool broken")},
		adapter, resolver, slog.Default(),
	)

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
		}),
		openAIFinalResponse("I see the tool errored."),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Try it"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("dispatch errors should be captured, not fatal: %v", err)
	}
	if result.Response.Content != "I see the tool errored." {
		t.Errorf("content = %s", result.Response.Content)
	}

	// Verify the error was passed to the LLM as tool result messages.
	// The messages slice should have: user, assistant (tool call), tool (error result).
	// With the new FormatTurnMessages approach, messages use RawMessage for
	// provider-specific formatting.
	if len(req.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(req.Messages))
	}
	// The tool result message(s) should contain the error text somewhere.
	found := false
	for _, msg := range req.Messages[2:] {
		raw := string(msg.RawMessage)
		if raw == "" {
			raw = msg.Content
		}
		if strings.Contains(raw, "tool broken") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool error message 'tool broken' in conversation, not found")
	}
}


func TestExecutor_CycleDetection(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}

	var cycleDetected bool
	emitter := func(eventType types.EventType, payload any) {
		if eventType == types.EventToolUseCycleDetected {
			cycleDetected = true
		}
	}

	exec := NewExecutor(
		ExecutorConfig{
			MaxIterations: 5,
			Dispatch:      mockDispatch("same_result"),
			Emitter:       emitter,
		},
		adapter, resolver, slog.Default(),
	)

	// Same tool + same args on consecutive calls → cycle.
	sameCall := openAIToolCallResponse([]struct{ id, name, args string }{
		{"call_1", "list_tasks", `{"status":"pending"}`},
	})
	sameCall2 := openAIToolCallResponse([]struct{ id, name, args string }{
		{"call_2", "list_tasks", `{"status":"pending"}`}, // Same name + args
	})

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		sameCall, sameCall2, openAIFinalResponse("Never reached"),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Cycle"}},
	}

	result, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("cycle should break gracefully, not error: %v", err)
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2 (broken at cycle)", result.Iterations)
	}
	if !cycleDetected {
		t.Error("expected cycle detection event")
	}
}

func TestExecutor_CompletionError(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatch("ok")},
		adapter, resolver, slog.Default(),
	)

	completeFn := func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		return nil, fmt.Errorf("connection refused")
	}

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Fail"}},
	}

	_, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "tooluse.Executor.Execute: completion (iteration 1): connection refused" {
		t.Errorf("error = %v", err)
	}
}

func TestExecutor_EventEmission(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}

	var events []types.EventType
	emitter := func(eventType types.EventType, payload any) {
		events = append(events, eventType)
	}

	exec := NewExecutor(
		ExecutorConfig{
			Dispatch: mockDispatch("ok"),
			Emitter:  emitter,
		},
		adapter, resolver, slog.Default(),
	)

	completeFn := mockCompletionFn([]*provider.CompletionResponse{
		openAIToolCallResponse([]struct{ id, name, args string }{
			{"call_1", "list_tasks", `{}`},
		}),
		openAIFinalResponse("Done."),
	})

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Events"}},
	}

	_, err := exec.Execute(context.Background(), completeFn, req, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestExecutor_ContextCancellation(t *testing.T) {
	resolver := NewResolver(executorTestSchemas())
	adapter := &OpenAIAdapter{}
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatch("ok")},
		adapter, resolver, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	completeFn := func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
		return nil, ctx.Err()
	}

	req := &provider.CompletionRequest{
		Kind:     "openai",
		Model:    "gpt-4o",
		Messages: []provider.ChatMessage{{Role: "user", Content: "Cancel"}},
	}

	_, err := exec.Execute(ctx, completeFn, req, 0, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestExecutor_DefaultMaxIterations(t *testing.T) {
	exec := NewExecutor(
		ExecutorConfig{Dispatch: mockDispatch("ok")},
		&OpenAIAdapter{},
		NewResolver(nil),
		nil,
	)
	if exec.config.MaxIterations != DefaultMaxIterations {
		t.Errorf("default max iterations = %d, want %d", exec.config.MaxIterations, DefaultMaxIterations)
	}
}

func TestIsToolUseStop(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{"tool_use", true},
		{"tool_calls", true},
		{"stop", false},
		{"end_turn", false},
		{"", false},
		{"length", false},
	}
	for _, tt := range tests {
		got := isToolUseStop(tt.reason)
		if got != tt.want {
			t.Errorf("isToolUseStop(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}
