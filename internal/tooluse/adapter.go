package tooluse

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// ProviderToolAdapter translates between the internal ToolDefinition / ToolCall
// types and the provider-specific wire formats used by each LLM API.
//
// Implementations exist for Anthropic, OpenAI (also used by Ollama, Azure, Custom),
// Google Gemini, and AWS Bedrock.
type ProviderToolAdapter interface {
	// FormatTools converts internal tool definitions into the provider-specific
	// JSON structure that gets included in the LLM request body.
	FormatTools(tools []types.ToolDefinition) (json.RawMessage, error)

	// ParseToolCalls extracts tool invocation requests from a provider-specific
	// LLM response body. Returns nil (no error) if the response contains no
	// tool calls.
	ParseToolCalls(response json.RawMessage) ([]types.ToolCall, error)

	// FormatToolResults converts internal tool call results into the
	// provider-specific JSON structure for the next conversation turn.
	FormatToolResults(results []types.ToolCallResult) (json.RawMessage, error)

	// FormatTurnMessages constructs the messages to append to the conversation
	// after a tool-use round. It takes the raw LLM response (containing tool
	// calls) and the formatted tool results, and returns the provider-specific
	// messages (e.g., Anthropic needs an assistant message with content blocks
	// and a user message with tool_result blocks; OpenAI needs an assistant
	// message with tool_calls and separate tool role messages).
	FormatTurnMessages(rawResponse json.RawMessage, formattedResults json.RawMessage) ([]provider.ChatMessage, error)
}

// NewToolAdapter returns the appropriate ProviderToolAdapter for the given
// provider kind string. Supported kinds: anthropic, openai, ollama, azure,
// custom, google, bedrock. Returns an error for unsupported kinds.
func NewToolAdapter(providerKind string) (ProviderToolAdapter, error) {
	switch strings.ToLower(providerKind) {
	case "anthropic":
		return &AnthropicAdapter{}, nil
	case "openai", "ollama", "azure", "custom":
		return &OpenAIAdapter{}, nil
	case "google":
		return &GoogleAdapter{}, nil
	case "bedrock":
		return &BedrockAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider kind %q for tool adapter", providerKind)
	}
}

