package tooluse

import (
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// AnthropicAdapter formats tools for the Anthropic Messages API.
//
// Wire formats:
//
//	Tools:   [{name, description, input_schema}]
//	Calls:   content blocks with type="tool_use" → {id, name, input}
//	Results: [{type:"tool_result", tool_use_id, content}]
type AnthropicAdapter struct{}

// -- FormatTools --------------------------------------------------------------

// anthropicTool is the Anthropic wire format for a tool definition.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// FormatTools converts tool definitions to the Anthropic tools array format.
func (a *AnthropicAdapter) FormatTools(tools []types.ToolDefinition) (json.RawMessage, error) {
	out := make([]anthropicTool, len(tools))
	for i, t := range tools {
		out[i] = anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return json.Marshal(out)
}

// -- ParseToolCalls -----------------------------------------------------------

// anthropicContentBlock represents a content block in an Anthropic response.
type anthropicContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// anthropicResponse is a minimal Anthropic Messages API response for tool call extraction.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
}

// ParseToolCalls extracts tool_use content blocks from an Anthropic response.
func (a *AnthropicAdapter) ParseToolCalls(response json.RawMessage) ([]types.ToolCall, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: parse response: %w", err)
	}

	var calls []types.ToolCall
	for _, block := range resp.Content {
		if block.Type != "tool_use" {
			continue
		}
		calls = append(calls, types.ToolCall{
			ID:        block.ID,
			Name:      block.Name,
			Arguments: block.Input,
		})
	}
	return calls, nil
}

// -- FormatToolResults --------------------------------------------------------

// anthropicToolResult is the Anthropic wire format for a tool result message.
type anthropicToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// FormatToolResults converts tool results to Anthropic tool_result content blocks.
func (a *AnthropicAdapter) FormatToolResults(results []types.ToolCallResult) (json.RawMessage, error) {
	out := make([]anthropicToolResult, len(results))
	for i, r := range results {
		out[i] = anthropicToolResult{
			Type:      "tool_result",
			ToolUseID: r.ToolCallID,
			Content:   r.Content,
			IsError:   r.IsError,
		}
	}
	return json.Marshal(out)
}

// -- FormatTurnMessages -------------------------------------------------------

// FormatTurnMessages constructs the Anthropic conversation turn for tool use.
// Anthropic requires:
//  1. An assistant message with the raw content blocks (text + tool_use) from the response
//  2. A user message with tool_result content blocks
func (a *AnthropicAdapter) FormatTurnMessages(rawResponse json.RawMessage, formattedResults json.RawMessage) ([]provider.ChatMessage, error) {
	// Extract the content array from the Anthropic response to use as
	// the assistant message's content (preserves tool_use blocks).
	var resp struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawResponse, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: parse response for turn messages: %w", err)
	}

	return []provider.ChatMessage{
		{Role: "assistant", RawContent: resp.Content},
		{Role: "user", RawContent: formattedResults},
	}, nil
}

