package types

import "encoding/json"

// ToolResult is the standard response envelope for MCP tool invocations.
type ToolResult struct {
	Content   []ToolContent `json:"content"`
	IsError   bool          `json:"isError,omitempty"`
	ElapsedMS int64         `json:"_meta_elapsed_ms,omitempty"`
}

// ToolContent represents a single content item in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NewToolResult creates a ToolResult from any JSON-serializable value.
func NewToolResult(v any) *ToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return &ToolResult{
			Content: []ToolContent{{Type: "text", Text: err.Error()}},
			IsError: true,
		}
	}
	return &ToolResult{
		Content: []ToolContent{{Type: "text", Text: string(data)}},
	}
}

// NewErrorResult creates an error ToolResult.
func NewErrorResult(msg string) *ToolResult {
	return &ToolResult{
		Content: []ToolContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
