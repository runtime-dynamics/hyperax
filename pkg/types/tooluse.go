package types

import "encoding/json"

// ToolDefinition describes a tool available to a persona, after ABAC filtering.
// This is the wire format sent to LLM providers for tool-use resolution.
type ToolDefinition struct {
	// Name is the canonical MCP tool name (e.g., "list_tasks").
	Name string `json:"name"`

	// Description is a human-readable summary of what the tool does.
	Description string `json:"description"`

	// InputSchema is the JSON Schema describing the tool's parameters.
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCall represents an LLM-generated tool invocation request.
type ToolCall struct {
	// ID is the provider-assigned identifier for this call (used to correlate results).
	ID string `json:"id"`

	// Name is the MCP tool name to invoke.
	Name string `json:"name"`

	// Arguments is the JSON-encoded parameter object from the LLM.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolCallResult is the result of executing a ToolCall, formatted for
// returning to the LLM in the next conversation turn.
type ToolCallResult struct {
	// ToolCallID correlates this result back to the originating ToolCall.ID.
	ToolCallID string `json:"tool_call_id"`

	// Name is the function name from the originating ToolCall. Required by
	// providers (e.g. Google Gemini) that need the function name in the
	// response, not just the opaque call ID.
	Name string `json:"name,omitempty"`

	// Content is the textual result content.
	Content string `json:"content"`

	// IsError indicates the tool call failed.
	IsError bool `json:"is_error,omitempty"`
}
