package tooluse

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// OpenAIAdapter formats tools for the OpenAI Chat Completions API.
// Also used for Ollama (OpenAI-compatible), Azure, and Custom providers.
//
// Wire formats:
//
//	Tools:   [{type:"function", function:{name, description, parameters}}]
//	Calls:   choices[0].message.tool_calls → [{id, type:"function", function:{name, arguments}}]
//	Results: [{role:"tool", tool_call_id, content}]
type OpenAIAdapter struct{}

// -- FormatTools --------------------------------------------------------------

// openAITool is the OpenAI wire format for a tool definition.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

// openAIFunction describes the function within an OpenAI tool definition.
type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// FormatTools converts tool definitions to the OpenAI tools array format.
func (o *OpenAIAdapter) FormatTools(tools []types.ToolDefinition) (json.RawMessage, error) {
	out := make([]openAITool, len(tools))
	for i, t := range tools {
		out[i] = openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return json.Marshal(out)
}

// -- ParseToolCalls -----------------------------------------------------------

// openAIResponse is a minimal OpenAI Chat Completion response for tool call extraction.
type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

// openAIChoice is a single choice in an OpenAI response.
type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIMessage struct {
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}


// openAIToolCall is a tool invocation in the OpenAI response format.
type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

// openAIFunctionCall contains the function name and arguments from the LLM.
// Arguments is RawMessage to accept both a JSON string (OpenAI standard) and
// a JSON object (Ollama and some other OpenAI-compatible providers).
type openAIFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ParseToolCalls extracts tool_calls from the first choice in an OpenAI response.
// If no structured tool_calls are found, it falls back to extracting JSON tool
// calls from the message text content. This handles models (e.g. some Ollama models)
// that output tool calls as plain text JSON instead of structured tool_calls.
func (o *OpenAIAdapter) ParseToolCalls(response json.RawMessage) ([]types.ToolCall, error) {
	var resp openAIResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("openai: parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, nil
	}

	// Primary path: structured tool_calls array.
	toolCalls := resp.Choices[0].Message.ToolCalls
	if len(toolCalls) > 0 {
		calls := make([]types.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			id := tc.ID
			// Ollama tool_calls may lack an ID. Generate a synthetic one so
			// FormatToolResults and FormatTurnMessages can match results to calls.
			if id == "" {
				id = fmt.Sprintf("ollama-call-%d", i)
			}
			calls[i] = types.ToolCall{
				ID:   id,
				Name: tc.Function.Name,
				// Arguments can be a JSON string (OpenAI: "\"{ ... }\"") or a
				// JSON object (Ollama: "{ ... }"). Normalise to raw object.
				Arguments: normalizeArguments(tc.Function.Arguments),
			}
		}
		return calls, nil
	}

	// Fallback: extract tool calls from text content for models that output
	// JSON tool invocations as plain text instead of structured tool_calls.
	if resp.Choices[0].Message.Content != nil {
		return extractToolCallsFromText(*resp.Choices[0].Message.Content), nil
	}
	return nil, nil
}

// normalizeArguments handles the difference between OpenAI (arguments is a JSON
// string like `"{\"key\":\"val\"}"`) and Ollama (arguments is a JSON object like
// `{"key":"val"}`). It returns raw JSON object bytes in both cases.
func normalizeArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	// If the raw value is a JSON string (starts with '"'), unmarshal to get
	// the inner string, which itself is a JSON object.
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return json.RawMessage(s)
		}
	}
	// Already an object/array/other — return as-is.
	return raw
}

// extractToolCallsFromText attempts to find JSON tool call objects in plain text.
// It looks for objects containing "name" and "arguments" keys, which is the
// pattern models use when they can't emit structured tool_calls.
//
// Supported formats:
//
//	{"name": "tool_name", "arguments": {...}}
//	{"type": "function", "function": {"name": "tool_name", "arguments": {...}}}
//	[{"name": "tool_name", "arguments": {...}}, ...]
func extractToolCallsFromText(text string) []types.ToolCall {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Strip <think>...</think> blocks (Qwen/reasoning models emit internal
	// chain-of-thought that shouldn't be parsed as tool calls).
	if idx := strings.Index(text, "</think>"); idx >= 0 {
		text = strings.TrimSpace(text[idx+len("</think>"):])
		if text == "" {
			return nil
		}
	}

	// Try to find the outermost JSON object or array in the text.
	jsonStr := extractOutermostJSON(text)
	if jsonStr == "" {
		return nil
	}

	// Try as a single tool call: {"name": "...", "arguments": {...}}
	var single struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal([]byte(jsonStr), &single) == nil && single.Name != "" && len(single.Arguments) > 0 {
		return []types.ToolCall{{
			ID:        "text-extract-0",
			Name:      single.Name,
			Arguments: single.Arguments,
		}}
	}

	// Try as OpenAI function wrapper: {"type": "function", "function": {"name": ..., "arguments": ...}}
	var wrapper struct {
		Type     string `json:"type"`
		Function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal([]byte(jsonStr), &wrapper) == nil && wrapper.Function.Name != "" {
		return []types.ToolCall{{
			ID:        "text-extract-0",
			Name:      wrapper.Function.Name,
			Arguments: wrapper.Function.Arguments,
		}}
	}

	// Try as an array of tool calls.
	var arr []json.RawMessage
	if json.Unmarshal([]byte(jsonStr), &arr) == nil && len(arr) > 0 {
		var calls []types.ToolCall
		for i, raw := range arr {
			var item struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(raw, &item) == nil && item.Name != "" && len(item.Arguments) > 0 {
				calls = append(calls, types.ToolCall{
					ID:        fmt.Sprintf("text-extract-%d", i),
					Name:      item.Name,
					Arguments: item.Arguments,
				})
			}
		}
		if len(calls) > 0 {
			return calls
		}
	}

	return nil
}

// extractOutermostJSON finds the first balanced JSON object or array in text.
// Returns the extracted JSON string, or empty if none found.
func extractOutermostJSON(text string) string {
	// Find the first '{' or '[' that starts a JSON structure.
	for i, ch := range text {
		if ch != '{' && ch != '[' {
			continue
		}
		close := byte('}')
		if ch == '[' {
			close = ']'
		}
		depth := 0
		inString := false
		escaped := false
		for j := i; j < len(text); j++ {
			b := text[j]
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' && inString {
				escaped = true
				continue
			}
			if b == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if b == byte(ch) {
				depth++
			} else if b == close {
				depth--
				if depth == 0 {
					return text[i : j+1]
				}
			}
		}
	}
	return ""
}

// -- FormatToolResults --------------------------------------------------------

// openAIToolResultMsg is the OpenAI wire format for a tool result message.
type openAIToolResultMsg struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// FormatToolResults converts tool results to OpenAI tool message format.
func (o *OpenAIAdapter) FormatToolResults(results []types.ToolCallResult) (json.RawMessage, error) {
	out := make([]openAIToolResultMsg, len(results))
	for i, r := range results {
		out[i] = openAIToolResultMsg{
			Role:       "tool",
			ToolCallID: r.ToolCallID,
			Content:    r.Content,
		}
	}
	return json.Marshal(out)
}

// -- FormatTurnMessages -------------------------------------------------------

// FormatTurnMessages constructs the OpenAI conversation turn for tool use.
// OpenAI requires:
//  1. An assistant message with the tool_calls array
//  2. One "tool" role message per tool result
//
// For text-extracted tool calls (models that output JSON as text), the assistant
// message won't have structured tool_calls. In that case we format the turn as
// natural language so the model can understand the result.
func (o *OpenAIAdapter) FormatTurnMessages(rawResponse json.RawMessage, formattedResults json.RawMessage) ([]provider.ChatMessage, error) {
	// Extract the assistant message (with tool_calls) from the response.
	var resp openAIResponse
	if err := json.Unmarshal(rawResponse, &resp); err != nil {
		return nil, fmt.Errorf("openai: parse response for turn messages: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}

	// Parse the tool results up front — needed for both paths.
	var toolResults []openAIToolResultMsg
	if err := json.Unmarshal(formattedResults, &toolResults); err != nil {
		return nil, fmt.Errorf("openai: parse formatted results: %w", err)
	}

	// Check if these are text-extracted tool calls (no structured tool_calls
	// in the response). If so, we can't use the standard OpenAI turn format
	// because the model doesn't understand "tool" role messages.
	hasStructuredCalls := len(resp.Choices[0].Message.ToolCalls) > 0
	if !hasStructuredCalls && len(toolResults) > 0 && isTextExtractedResult(toolResults) {
		return o.formatTextExtractedTurn(resp.Choices[0].Message, toolResults)
	}

	// Standard path: structured tool_calls.
	// The full message object (including role, content, and tool_calls) is
	// sent back as-is.
	assistantRaw, err := json.Marshal(resp.Choices[0].Message)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal assistant message: %w", err)
	}

	messages := []provider.ChatMessage{
		{RawMessage: assistantRaw},
	}

	// Each tool result is a separate "tool" role message with tool_call_id.
	for _, tr := range toolResults {
		raw, err := json.Marshal(tr)
		if err != nil {
			return nil, fmt.Errorf("openai: marshal tool result %q: %w", tr.ToolCallID, err)
		}
		messages = append(messages, provider.ChatMessage{RawMessage: raw})
	}

	return messages, nil
}

// isTextExtractedResult checks if tool results came from text extraction
// by looking for the "text-extract-" prefix in tool call IDs.
func isTextExtractedResult(results []openAIToolResultMsg) bool {
	for _, r := range results {
		if strings.HasPrefix(r.ToolCallID, "text-extract-") {
			return true
		}
	}
	return false
}

// formatTextExtractedTurn builds conversation messages for models that output
// tool calls as text. Instead of using the "tool" role (which the model won't
// understand), we present results as a user message so the model can naturally
// continue the conversation.
func (o *OpenAIAdapter) formatTextExtractedTurn(msg openAIMessage, results []openAIToolResultMsg) ([]provider.ChatMessage, error) {
	// Keep the original assistant message as-is (contains the JSON text).
	assistantRaw, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal text-extracted assistant message: %w", err)
	}
	messages := []provider.ChatMessage{
		{RawMessage: assistantRaw},
	}

	// Build a user message with the tool results in natural language.
	var sb strings.Builder
	for _, r := range results {
		toolName := strings.TrimPrefix(r.ToolCallID, "text-extract-")
		fmt.Fprintf(&sb, "Tool result:\n%s\n\n", r.Content)
		_ = toolName // ID is numeric index, tool name is in the content
	}

	userMsg := struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		Role:    "user",
		Content: strings.TrimSpace(sb.String()),
	}
	raw, err := json.Marshal(userMsg)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal text-extracted user message: %w", err)
	}
	messages = append(messages, provider.ChatMessage{RawMessage: raw})

	return messages, nil
}

