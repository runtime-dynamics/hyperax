package tooluse

import (
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// BedrockAdapter formats tools for the AWS Bedrock Converse API.
//
// Wire formats:
//
//	Tools:   {tools: [{toolSpec: {name, description, inputSchema: {json: ...}}}]}
//	Calls:   output.message.content → {toolUse: {toolUseId, name, input}}
//	Results: [{toolResult: {toolUseId, content: [{json: ...}]}}]
type BedrockAdapter struct{}

// -- FormatTools --------------------------------------------------------------

// bedrockToolConfig is the top-level tools configuration for Bedrock Converse.
type bedrockToolConfig struct {
	Tools []bedrockTool `json:"tools"`
}

// bedrockTool wraps a single tool spec in the Bedrock format.
type bedrockTool struct {
	ToolSpec bedrockToolSpec `json:"toolSpec"`
}

// bedrockToolSpec describes a tool for the Bedrock Converse API.
type bedrockToolSpec struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema bedrockInputSchema `json:"inputSchema"`
}

// bedrockInputSchema wraps the JSON Schema in Bedrock's nested format.
type bedrockInputSchema struct {
	JSON json.RawMessage `json:"json"`
}

// FormatTools converts tool definitions to the Bedrock toolConfiguration format.
func (b *BedrockAdapter) FormatTools(tools []types.ToolDefinition) (json.RawMessage, error) {
	bt := make([]bedrockTool, len(tools))
	for i, t := range tools {
		bt[i] = bedrockTool{
			ToolSpec: bedrockToolSpec{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: bedrockInputSchema{JSON: t.InputSchema},
			},
		}
	}
	config := bedrockToolConfig{Tools: bt}
	return json.Marshal(config)
}

// -- ParseToolCalls -----------------------------------------------------------

// bedrockConverseResp is a minimal Bedrock Converse response for tool call extraction.
type bedrockConverseResp struct {
	Output bedrockOutput `json:"output"`
}

// bedrockOutput wraps the output message in a Bedrock response.
type bedrockOutput struct {
	Message *bedrockRespMessage `json:"message,omitempty"`
}

// bedrockRespMessage is the assistant message in a Bedrock response.
type bedrockRespMessage struct {
	Content []bedrockRespContentBlock `json:"content"`
}

// bedrockRespContentBlock is a content block that may contain a tool use request.
type bedrockRespContentBlock struct {
	ToolUse *bedrockToolUse `json:"toolUse,omitempty"`
}

// bedrockToolUse is a tool invocation in the Bedrock response format.
type bedrockToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

// ParseToolCalls extracts toolUse content blocks from a Bedrock Converse response.
func (b *BedrockAdapter) ParseToolCalls(response json.RawMessage) ([]types.ToolCall, error) {
	var resp bedrockConverseResp
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("bedrock: parse response: %w", err)
	}

	if resp.Output.Message == nil {
		return nil, nil
	}

	var calls []types.ToolCall
	for _, block := range resp.Output.Message.Content {
		if block.ToolUse == nil {
			continue
		}
		calls = append(calls, types.ToolCall{
			ID:        block.ToolUse.ToolUseID,
			Name:      block.ToolUse.Name,
			Arguments: block.ToolUse.Input,
		})
	}
	return calls, nil
}

// -- FormatToolResults --------------------------------------------------------

// bedrockToolResultBlock is the Bedrock wire format for a tool result.
type bedrockToolResultBlock struct {
	ToolResult bedrockToolResultBody `json:"toolResult"`
}

// bedrockToolResultBody contains the tool result data.
type bedrockToolResultBody struct {
	ToolUseID string                    `json:"toolUseId"`
	Status    string                    `json:"status,omitempty"`
	Content   []bedrockResultContentEl `json:"content"`
}

// bedrockResultContentEl is a content element in a Bedrock tool result.
type bedrockResultContentEl struct {
	JSON json.RawMessage `json:"json,omitempty"`
	Text *string         `json:"text,omitempty"`
}

// FormatToolResults converts tool results to Bedrock toolResult content blocks.
func (b *BedrockAdapter) FormatToolResults(results []types.ToolCallResult) (json.RawMessage, error) {
	out := make([]bedrockToolResultBlock, len(results))
	for i, r := range results {
		status := "success"
		if r.IsError {
			status = "error"
		}
		content := r.Content
		out[i] = bedrockToolResultBlock{
			ToolResult: bedrockToolResultBody{
				ToolUseID: r.ToolCallID,
				Status:    status,
				Content:   []bedrockResultContentEl{{Text: &content}},
			},
		}
	}
	return json.Marshal(out)
}

// -- FormatTurnMessages -------------------------------------------------------

// FormatTurnMessages constructs the Bedrock Converse conversation turn for tool use.
// Bedrock uses "assistant" for the model's tool-use response and "user" for tool results.
func (b *BedrockAdapter) FormatTurnMessages(rawResponse json.RawMessage, formattedResults json.RawMessage) ([]provider.ChatMessage, error) {
	// Extract the assistant's content blocks from the response.
	var resp bedrockConverseResp
	if err := json.Unmarshal(rawResponse, &resp); err != nil {
		return nil, fmt.Errorf("bedrock: parse response for turn messages: %w", err)
	}

	var assistantRaw json.RawMessage
	if resp.Output.Message != nil {
		var err error
		assistantRaw, err = json.Marshal(resp.Output.Message.Content)
		if err != nil {
			return nil, fmt.Errorf("bedrock: marshal assistant content: %w", err)
		}
	}

	return []provider.ChatMessage{
		{Role: "assistant", RawContent: assistantRaw},
		{Role: "user", RawContent: formattedResults},
	}, nil
}

