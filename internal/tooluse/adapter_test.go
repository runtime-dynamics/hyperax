package tooluse

import (
	"encoding/json"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

// sampleTools returns a pair of tool definitions for adapter tests.
func sampleTools() []types.ToolDefinition {
	return []types.ToolDefinition{
		{
			Name:        "list_tasks",
			Description: "List all tasks in the project",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`),
		},
		{
			Name:        "run_pipeline",
			Description: "Execute a pipeline by name",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		},
	}
}

// sampleResults returns tool call results for adapter tests.
func sampleResults() []types.ToolCallResult {
	return []types.ToolCallResult{
		{ToolCallID: "call_123", Content: `{"tasks":[]}`, IsError: false},
		{ToolCallID: "call_456", Content: "pipeline not found", IsError: true},
	}
}

// ── Factory tests ───────────────────────────────────────────────────────────

func TestNewToolAdapter_SupportedKinds(t *testing.T) {
	kinds := []string{"anthropic", "openai", "ollama", "azure", "custom", "google", "bedrock"}
	for _, kind := range kinds {
		a, err := NewToolAdapter(kind)
		if err != nil {
			t.Errorf("NewToolAdapter(%q): unexpected error: %v", kind, err)
		}
		if a == nil {
			t.Errorf("NewToolAdapter(%q): returned nil adapter", kind)
		}
	}
}

func TestNewToolAdapter_UnsupportedKind(t *testing.T) {
	_, err := NewToolAdapter("unknown_provider")
	if err == nil {
		t.Error("NewToolAdapter(unknown): expected error, got nil")
	}
}

func TestNewToolAdapter_CaseInsensitive(t *testing.T) {
	a, err := NewToolAdapter("Anthropic")
	if err != nil {
		t.Fatalf("NewToolAdapter(Anthropic): %v", err)
	}
	if _, ok := a.(*AnthropicAdapter); !ok {
		t.Error("expected AnthropicAdapter for 'Anthropic'")
	}
}

func TestNewToolAdapter_OllamaUsesOpenAI(t *testing.T) {
	a, err := NewToolAdapter("ollama")
	if err != nil {
		t.Fatalf("NewToolAdapter(ollama): %v", err)
	}
	if _, ok := a.(*OpenAIAdapter); !ok {
		t.Error("expected OpenAIAdapter for ollama")
	}
}

func TestNewToolAdapter_AzureUsesOpenAI(t *testing.T) {
	a, err := NewToolAdapter("azure")
	if err != nil {
		t.Fatalf("NewToolAdapter(azure): %v", err)
	}
	if _, ok := a.(*OpenAIAdapter); !ok {
		t.Error("expected OpenAIAdapter for azure")
	}
}

// ── Anthropic adapter ───────────────────────────────────────────────────────

func TestAnthropicAdapter_FormatTools(t *testing.T) {
	a := &AnthropicAdapter{}
	raw, err := a.FormatTools(sampleTools())
	if err != nil {
		t.Fatalf("FormatTools: %v", err)
	}

	var tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "list_tasks" {
		t.Errorf("tool[0].name = %s", tools[0].Name)
	}
	if tools[1].Description != "Execute a pipeline by name" {
		t.Errorf("tool[1].description = %s", tools[1].Description)
	}
}

func TestAnthropicAdapter_ParseToolCalls(t *testing.T) {
	a := &AnthropicAdapter{}
	response := json.RawMessage(`{
		"content": [
			{"type": "text", "text": "Let me check..."},
			{"type": "tool_use", "id": "toolu_01", "name": "list_tasks", "input": {"status": "pending"}},
			{"type": "tool_use", "id": "toolu_02", "name": "run_pipeline", "input": {"name": "build"}}
		]
	}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].ID != "toolu_01" || calls[0].Name != "list_tasks" {
		t.Errorf("call[0] = {%s, %s}", calls[0].ID, calls[0].Name)
	}
	if calls[1].ID != "toolu_02" || calls[1].Name != "run_pipeline" {
		t.Errorf("call[1] = {%s, %s}", calls[1].ID, calls[1].Name)
	}

	// Verify arguments are preserved.
	var args map[string]string
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if args["status"] != "pending" {
		t.Errorf("args[status] = %s", args["status"])
	}
}

func TestAnthropicAdapter_ParseToolCalls_NoToolUse(t *testing.T) {
	a := &AnthropicAdapter{}
	response := json.RawMessage(`{"content": [{"type": "text", "text": "No tools needed."}]}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestAnthropicAdapter_FormatToolResults(t *testing.T) {
	a := &AnthropicAdapter{}
	raw, err := a.FormatToolResults(sampleResults())
	if err != nil {
		t.Fatalf("FormatToolResults: %v", err)
	}

	var results []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Type != "tool_result" || results[0].ToolUseID != "call_123" {
		t.Errorf("result[0] = {%s, %s}", results[0].Type, results[0].ToolUseID)
	}
	if results[1].IsError != true {
		t.Error("result[1].is_error should be true")
	}
}

// ── OpenAI adapter ──────────────────────────────────────────────────────────

func TestOpenAIAdapter_FormatTools(t *testing.T) {
	a := &OpenAIAdapter{}
	raw, err := a.FormatTools(sampleTools())
	if err != nil {
		t.Fatalf("FormatTools: %v", err)
	}

	var tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Type != "function" {
		t.Errorf("tool[0].type = %s", tools[0].Type)
	}
	if tools[0].Function.Name != "list_tasks" {
		t.Errorf("tool[0].function.name = %s", tools[0].Function.Name)
	}
}

func TestOpenAIAdapter_ParseToolCalls(t *testing.T) {
	a := &OpenAIAdapter{}
	response := json.RawMessage(`{
		"choices": [{
			"message": {
				"tool_calls": [
					{
						"id": "call_abc",
						"type": "function",
						"function": {"name": "list_tasks", "arguments": "{\"status\":\"done\"}"}
					}
				]
			}
		}]
	}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ID != "call_abc" || calls[0].Name != "list_tasks" {
		t.Errorf("call = {%s, %s}", calls[0].ID, calls[0].Name)
	}

	// OpenAI arguments is a JSON string, verify it deserialises correctly.
	var args map[string]string
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if args["status"] != "done" {
		t.Errorf("args[status] = %s", args["status"])
	}
}

func TestOpenAIAdapter_ParseToolCalls_NoChoices(t *testing.T) {
	a := &OpenAIAdapter{}
	response := json.RawMessage(`{"choices": []}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil calls, got %d", len(calls))
	}
}

func TestOpenAIAdapter_ParseToolCalls_NoToolCalls(t *testing.T) {
	a := &OpenAIAdapter{}
	response := json.RawMessage(`{"choices": [{"message": {"content": "Hello!"}}]}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil calls, got %d", len(calls))
	}
}

func TestOpenAIAdapter_FormatToolResults(t *testing.T) {
	a := &OpenAIAdapter{}
	raw, err := a.FormatToolResults(sampleResults())
	if err != nil {
		t.Fatalf("FormatToolResults: %v", err)
	}

	var results []struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Role != "tool" {
		t.Errorf("result[0].role = %s", results[0].Role)
	}
	if results[0].ToolCallID != "call_123" {
		t.Errorf("result[0].tool_call_id = %s", results[0].ToolCallID)
	}
}

// ── Google adapter ──────────────────────────────────────────────────────────

func TestGoogleAdapter_FormatTools(t *testing.T) {
	a := &GoogleAdapter{}
	raw, err := a.FormatTools(sampleTools())
	if err != nil {
		t.Fatalf("FormatTools: %v", err)
	}

	var wrapper struct {
		FunctionDeclarations []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"functionDeclarations"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(wrapper.FunctionDeclarations) != 2 {
		t.Fatalf("expected 2 declarations, got %d", len(wrapper.FunctionDeclarations))
	}
	if wrapper.FunctionDeclarations[0].Name != "list_tasks" {
		t.Errorf("decl[0].name = %s", wrapper.FunctionDeclarations[0].Name)
	}
}

func TestGoogleAdapter_ParseToolCalls(t *testing.T) {
	a := &GoogleAdapter{}
	response := json.RawMessage(`{
		"candidates": [{
			"content": {
				"parts": [
					{"functionCall": {"name": "list_tasks", "args": {"status": "pending"}}},
					{"functionCall": {"name": "run_pipeline", "args": {"name": "test"}}}
				]
			}
		}]
	}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "list_tasks" {
		t.Errorf("call[0].name = %s", calls[0].Name)
	}
	// Synthetic ID format check.
	if calls[0].ID != "gemini_call_0_list_tasks" {
		t.Errorf("call[0].id = %s", calls[0].ID)
	}
	if calls[1].ID != "gemini_call_1_run_pipeline" {
		t.Errorf("call[1].id = %s", calls[1].ID)
	}
}

func TestGoogleAdapter_ParseToolCalls_NoCandidates(t *testing.T) {
	a := &GoogleAdapter{}
	calls, err := a.ParseToolCalls(json.RawMessage(`{"candidates": []}`))
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil, got %d calls", len(calls))
	}
}

func TestGoogleAdapter_FormatToolResults(t *testing.T) {
	a := &GoogleAdapter{}
	results := []types.ToolCallResult{
		{ToolCallID: "gemini_call_0_list_tasks", Content: `{"tasks":[]}`, IsError: false},
	}
	raw, err := a.FormatToolResults(results)
	if err != nil {
		t.Fatalf("FormatToolResults: %v", err)
	}

	var parts []struct {
		FunctionResponse struct {
			Name     string `json:"name"`
			Response struct {
				Content string `json:"content"`
				IsError bool   `json:"is_error"`
			} `json:"response"`
		} `json:"functionResponse"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].FunctionResponse.Name != "list_tasks" {
		t.Errorf("name = %s", parts[0].FunctionResponse.Name)
	}
}

func TestExtractGeminiName(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"gemini_call_0_list_tasks", "list_tasks"},
		{"gemini_call_12_run_pipeline", "run_pipeline"},
		{"gemini_call_0_a_b_c", "a_b_c"},
		{"other_id", "other_id"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractGeminiName(tt.id)
		if got != tt.want {
			t.Errorf("extractGeminiName(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

// ── Bedrock adapter ─────────────────────────────────────────────────────────

func TestBedrockAdapter_FormatTools(t *testing.T) {
	a := &BedrockAdapter{}
	raw, err := a.FormatTools(sampleTools())
	if err != nil {
		t.Fatalf("FormatTools: %v", err)
	}

	var config struct {
		Tools []struct {
			ToolSpec struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				InputSchema struct {
					JSON json.RawMessage `json:"json"`
				} `json:"inputSchema"`
			} `json:"toolSpec"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(config.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(config.Tools))
	}
	if config.Tools[0].ToolSpec.Name != "list_tasks" {
		t.Errorf("tool[0].name = %s", config.Tools[0].ToolSpec.Name)
	}
	// Verify nested inputSchema.json is valid.
	var schema map[string]any
	if err := json.Unmarshal(config.Tools[0].ToolSpec.InputSchema.JSON, &schema); err != nil {
		t.Fatalf("inputSchema.json invalid: %v", err)
	}
}

func TestBedrockAdapter_ParseToolCalls(t *testing.T) {
	a := &BedrockAdapter{}
	response := json.RawMessage(`{
		"output": {
			"message": {
				"content": [
					{"toolUse": {"toolUseId": "br_001", "name": "list_tasks", "input": {"status": "active"}}},
					{"text": "I found the following tasks..."}
				]
			}
		}
	}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ID != "br_001" || calls[0].Name != "list_tasks" {
		t.Errorf("call = {%s, %s}", calls[0].ID, calls[0].Name)
	}
}

func TestBedrockAdapter_ParseToolCalls_NoMessage(t *testing.T) {
	a := &BedrockAdapter{}
	calls, err := a.ParseToolCalls(json.RawMessage(`{"output": {}}`))
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil, got %d calls", len(calls))
	}
}

func TestBedrockAdapter_FormatToolResults(t *testing.T) {
	a := &BedrockAdapter{}
	raw, err := a.FormatToolResults(sampleResults())
	if err != nil {
		t.Fatalf("FormatToolResults: %v", err)
	}

	var blocks []struct {
		ToolResult struct {
			ToolUseID string `json:"toolUseId"`
			Status    string `json:"status"`
			Content   []struct {
				Text *string `json:"text"`
			} `json:"content"`
		} `json:"toolResult"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].ToolResult.Status != "success" {
		t.Errorf("block[0].status = %s", blocks[0].ToolResult.Status)
	}
	if blocks[1].ToolResult.Status != "error" {
		t.Errorf("block[1].status = %s", blocks[1].ToolResult.Status)
	}
	if blocks[0].ToolResult.ToolUseID != "call_123" {
		t.Errorf("block[0].toolUseId = %s", blocks[0].ToolResult.ToolUseID)
	}
}

// ── Empty tools edge case ───────────────────────────────────────────────────

func TestAllAdapters_EmptyTools(t *testing.T) {
	adapters := []struct {
		name    string
		adapter ProviderToolAdapter
	}{
		{"anthropic", &AnthropicAdapter{}},
		{"openai", &OpenAIAdapter{}},
		{"google", &GoogleAdapter{}},
		{"bedrock", &BedrockAdapter{}},
	}

	for _, tc := range adapters {
		raw, err := tc.adapter.FormatTools(nil)
		if err != nil {
			t.Errorf("%s: FormatTools(nil): %v", tc.name, err)
			continue
		}
		// Should produce valid JSON (empty array or wrapper with empty array).
		if !json.Valid(raw) {
			t.Errorf("%s: FormatTools(nil) produced invalid JSON", tc.name)
		}
	}
}

func TestAllAdapters_EmptyResults(t *testing.T) {
	adapters := []struct {
		name    string
		adapter ProviderToolAdapter
	}{
		{"anthropic", &AnthropicAdapter{}},
		{"openai", &OpenAIAdapter{}},
		{"google", &GoogleAdapter{}},
		{"bedrock", &BedrockAdapter{}},
	}

	for _, tc := range adapters {
		raw, err := tc.adapter.FormatToolResults(nil)
		if err != nil {
			t.Errorf("%s: FormatToolResults(nil): %v", tc.name, err)
			continue
		}
		if !json.Valid(raw) {
			t.Errorf("%s: FormatToolResults(nil) produced invalid JSON", tc.name)
		}
	}
}

// ── Text-based tool call extraction tests ──────────────────────────────────

func TestExtractToolCallsFromText_SingleCall(t *testing.T) {
	text := `{"name": "send_message", "arguments": {"to": "reid", "content": "hello"}}`
	calls := extractToolCallsFromText(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "send_message" {
		t.Errorf("name = %q, want %q", calls[0].Name, "send_message")
	}
	if calls[0].ID != "text-extract-0" {
		t.Errorf("id = %q, want %q", calls[0].ID, "text-extract-0")
	}
}

func TestExtractToolCallsFromText_WithSurroundingText(t *testing.T) {
	text := `I'll call the tool now: {"name": "list_tasks", "arguments": {"status": "pending"}} Let me know if you need more.`
	calls := extractToolCallsFromText(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "list_tasks" {
		t.Errorf("name = %q, want %q", calls[0].Name, "list_tasks")
	}
}

func TestExtractToolCallsFromText_FunctionWrapper(t *testing.T) {
	text := `{"type": "function", "function": {"name": "run_pipeline", "arguments": {"name": "build"}}}`
	calls := extractToolCallsFromText(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "run_pipeline" {
		t.Errorf("name = %q, want %q", calls[0].Name, "run_pipeline")
	}
}

func TestExtractToolCallsFromText_Array(t *testing.T) {
	text := `[{"name": "list_tasks", "arguments": {}}, {"name": "send_message", "arguments": {"to": "bob"}}]`
	calls := extractToolCallsFromText(text)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "list_tasks" {
		t.Errorf("calls[0].name = %q", calls[0].Name)
	}
	if calls[1].Name != "send_message" {
		t.Errorf("calls[1].name = %q", calls[1].Name)
	}
}

func TestExtractToolCallsFromText_NoToolCall(t *testing.T) {
	cases := []string{
		"Hello, how can I help you?",
		"",
		"   ",
		`{"key": "value"}`, // Has JSON but no name+arguments
		`{"name": "foo"}`,  // Has name but no arguments
	}
	for _, text := range cases {
		calls := extractToolCallsFromText(text)
		if len(calls) != 0 {
			t.Errorf("extractToolCallsFromText(%q): expected 0 calls, got %d", text, len(calls))
		}
	}
}

func TestExtractOutermostJSON_Simple(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"a":1}`, `{"a":1}`},
		{`text before {"a":1} text after`, `{"a":1}`},
		{`[{"a":1}]`, `[{"a":1}]`},
		{`no json here`, ""},
		{`{"nested": {"deep": true}}`, `{"nested": {"deep": true}}`},
	}
	for _, tt := range tests {
		got := extractOutermostJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractOutermostJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOpenAIAdapter_ParseToolCalls_TextFallback(t *testing.T) {
	a := &OpenAIAdapter{}
	// Simulate a model that outputs tool calls as text (no structured tool_calls).
	response := json.RawMessage(`{"choices": [{"message": {"content": "{\"name\": \"send_message\", \"arguments\": {\"to\": \"reid\", \"content\": \"hello\"}}"}}]}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from text fallback, got %d", len(calls))
	}
	if calls[0].Name != "send_message" {
		t.Errorf("name = %q, want %q", calls[0].Name, "send_message")
	}
}

func TestOpenAIAdapter_ParseToolCalls_PlainTextNoExtraction(t *testing.T) {
	a := &OpenAIAdapter{}
	// Normal conversational text — should NOT extract anything.
	response := json.RawMessage(`{"choices": [{"message": {"content": "Sure, I can help you with that task."}}]}`)

	calls, err := a.ParseToolCalls(response)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for plain text, got %d", len(calls))
	}
}
