package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"google.golang.org/genai"
)

// completionTimeout is the HTTP client timeout for LLM chat completion requests.
// This applies per-call (each tool-use iteration gets a fresh client). Set high
// enough to handle large payloads (50-90 ABAC-filtered tools + conversation
// history) without exceeding reasonable wait times for a single API round-trip.
const completionTimeout = 180 * time.Second

// ChatMessage represents a single message in the conversation history.
type ChatMessage struct {
	Role       string          `json:"role"`                  // "system", "user", "assistant"
	Content    string          `json:"content,omitempty"`     // Simple text content
	RawContent json.RawMessage `json:"raw_content,omitempty"` // Provider-specific content field (overrides Content when non-nil)
	RawMessage json.RawMessage `json:"raw_message,omitempty"` // Pre-formatted full message JSON (overrides everything when non-nil)
}

// MarshalJSON implements custom JSON serialization for ChatMessage.
// Priority: RawMessage (full pre-formatted JSON) > RawContent (content field override) > Content (plain string).
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	if m.RawMessage != nil {
		return m.RawMessage, nil
	}
	if m.RawContent != nil {
		type msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		return json.Marshal(msg{Role: m.Role, Content: m.RawContent})
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	return json.Marshal(msg{Role: m.Role, Content: m.Content})
}

// CompletionRequest contains all parameters needed to execute a chat completion
// against any supported LLM provider.
type CompletionRequest struct {
	Kind      string          // Provider kind: ollama, openai, anthropic, azure, custom
	BaseURL   string          // Provider base URL (e.g., "http://localhost:11434", "https://api.openai.com/v1")
	APIKey    string          // API key (empty for keyless providers like local Ollama)
	Model     string          // Model identifier (e.g., "llama3", "gpt-4o", "claude-sonnet-4-20250514")
	Messages  []ChatMessage   // Conversation messages in chronological order
	Tools     json.RawMessage // Provider-formatted tool definitions (nil = no tool use)
	AgentName string          // Agent name for diagnostics (not sent to provider)
}

// CompletionResponse holds the result of a chat completion call.
type CompletionResponse struct {
	Content     string          // The assistant's reply text
	Model       string          // The model that generated the response
	Usage       *UsageInfo      // Token usage statistics (nil if provider doesn't report them)
	StopReason  string          // Why generation stopped: "end_turn"/"stop", "tool_use"/"tool_calls", etc.
	RawResponse json.RawMessage // Full provider response body for adapter parsing of tool calls
}

// UsageInfo contains token usage statistics returned by the provider.
type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CacheCreationTokens is the number of tokens written to the prompt cache
	// on this request (Anthropic). Zero when the prefix was already cached.
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
	// CacheReadTokens is the number of tokens read from the prompt cache
	// (Anthropic). Non-zero means a cache hit — these tokens cost ~90% less.
	CacheReadTokens int `json:"cache_read_input_tokens,omitempty"`
}

// ChatCompletion sends a chat completion request to the appropriate LLM provider
// and returns the assistant's response. It dispatches to provider-specific
// implementations based on the Kind field in the request.
//
// Supported kinds: ollama, openai, anthropic, azure, google, bedrock, custom.
// Custom providers use the OpenAI-compatible format.
//
// Returns an error if the provider kind is unsupported, the HTTP request fails,
// the provider returns a non-2xx status, or the response cannot be parsed.
func ChatCompletion(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("provider.ChatCompletion: request is nil")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("provider.ChatCompletion: model is required")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("provider.ChatCompletion: at least one message is required")
	}

	// Dump the full prompt to data/<agent_name>.md for inspection.
	// Overwrites on every send so only the latest request is captured.
	dumpPromptToFile(req)

	switch strings.ToLower(req.Kind) {
	case "ollama":
		return completeOllama(ctx, req)
	case "openai":
		return completeOpenAI(ctx, req)
	case "anthropic":
		return completeAnthropic(ctx, req)
	case "azure":
		return completeAzure(ctx, req)
	case "google":
		return completeGoogle(ctx, req)
	case "bedrock":
		return completeBedrock(ctx, req)
	case "custom":
		return completeOpenAI(ctx, req) // Custom providers use OpenAI-compatible format
	default:
		return nil, fmt.Errorf("provider.ChatCompletion: unsupported provider kind %q", req.Kind)
	}
}

// -- Prompt dump (diagnostic) -----------------------------------------------

// dumpPromptToFile writes the full prompt payload to data/<agent_name>.md for
// inspection. Overwrites on every send so only the latest request is captured.
// Errors are logged but never propagated — this is purely diagnostic.
func dumpPromptToFile(req *CompletionRequest) {
	name := req.AgentName
	if name == "" {
		name = "unknown"
	}
	// Sanitise the agent name for use as a filename.
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")

	dir := filepath.Join("data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("dumpPromptToFile: mkdir", "error", err)
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Prompt Dump: %s\n\n", req.AgentName)
	fmt.Fprintf(&b, "**Provider:** %s  \n", req.Kind)
	fmt.Fprintf(&b, "**Model:** %s  \n", req.Model)
	fmt.Fprintf(&b, "**Messages:** %d  \n", len(req.Messages))

	// Tool count and names.
	if len(req.Tools) > 0 {
		var toolArray []json.RawMessage
		if json.Unmarshal(req.Tools, &toolArray) == nil {
			fmt.Fprintf(&b, "**Tools:** %d  \n", len(toolArray))
			// Extract tool names for a quick summary.
			var toolNames []string
			for _, raw := range toolArray {
				var t struct {
					Name     string `json:"name"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				}
				if json.Unmarshal(raw, &t) == nil {
					n := t.Name
					if n == "" {
						n = t.Function.Name
					}
					if n != "" {
						toolNames = append(toolNames, n)
					}
				}
			}
			if len(toolNames) > 0 {
				fmt.Fprintf(&b, "**Tool names:** %s  \n", strings.Join(toolNames, ", "))
			}
		} else {
			fmt.Fprintf(&b, "**Tools:** (present, non-array)  \n")
		}
		fmt.Fprintf(&b, "**Tools JSON size:** %d bytes (~%d KB)  \n", len(req.Tools), len(req.Tools)/1024)
	}

	// Per-message byte breakdown.
	totalMsgBytes := 0
	fmt.Fprintf(&b, "\n### Message sizes\n\n")
	fmt.Fprintf(&b, "| # | Role | Bytes | Source |\n")
	fmt.Fprintf(&b, "|---|------|-------|--------|\n")
	for i, m := range req.Messages {
		sz := len(m.Content) + len(m.RawContent) + len(m.RawMessage)
		totalMsgBytes += sz
		src := "Content"
		if m.RawMessage != nil {
			src = "RawMessage"
		} else if m.RawContent != nil {
			src = "RawContent"
		}
		fmt.Fprintf(&b, "| %d | %s | %d | %s |\n", i, m.Role, sz, src)
	}

	totalBytes := len(req.Tools) + totalMsgBytes
	fmt.Fprintf(&b, "\n**Total message bytes:** %d (~%d KB)  \n", totalMsgBytes, totalMsgBytes/1024)
	fmt.Fprintf(&b, "**Total payload (msgs+tools):** %d bytes (~%d KB)  \n\n", totalBytes, totalBytes/1024)

	fmt.Fprintf(&b, "---\n\n")

	// Messages.
	for i, m := range req.Messages {
		sz := len(m.Content) + len(m.RawContent) + len(m.RawMessage)
		fmt.Fprintf(&b, "## Message %d — role: `%s` (%d bytes)\n\n", i, m.Role, sz)

		if m.RawMessage != nil {
			raw := string(m.RawMessage)
			if len(raw) > 2000 {
				fmt.Fprintf(&b, "```json\n%s\n... (%d bytes truncated)\n```\n\n", raw[:2000], len(raw)-2000)
			} else {
				fmt.Fprintf(&b, "```json\n%s\n```\n\n", raw)
			}
		} else if m.RawContent != nil {
			raw := string(m.RawContent)
			if len(raw) > 2000 {
				fmt.Fprintf(&b, "```json\n%s\n... (%d bytes truncated)\n```\n\n", raw[:2000], len(raw)-2000)
			} else {
				fmt.Fprintf(&b, "```json\n%s\n```\n\n", raw)
			}
		} else {
			content := m.Content
			if len(content) > 4000 {
				fmt.Fprintf(&b, "%s\n\n... (%d chars truncated)\n\n", content[:4000], len(content)-4000)
			} else {
				fmt.Fprintf(&b, "%s\n\n", content)
			}
		}
	}

	outPath := filepath.Join(dir, name+".md")
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		slog.Warn("dumpPromptToFile: write", "path", outPath, "error", err)
	}
}

// -- Ollama completion ------------------------------------------------------

// ollamaTrace appends a timestamped line to data/ollama_trace.log for
// diagnosing the tool-use round-trip. Safe to call concurrently (append-only).
func ollamaTrace(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05.000"), msg)
	f, err := os.OpenFile(filepath.Join("data", "ollama_trace.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}

// ollamaChatRequest is the request body for Ollama's /api/chat endpoint.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ChatMessage   `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    json.RawMessage `json:"tools,omitempty"`
}

// ollamaNativeResponse is the native /api/chat response format.
type ollamaNativeResponse struct {
	Message struct {
		Role      string          `json:"role"`
		Content   string          `json:"content"`
		ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
	} `json:"message"`
	Model           string `json:"model"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	TotalDuration   int64  `json:"total_duration"`
}

// ollamaOpenAIResponse is the OpenAI-compatible response that some Ollama
// servers (vLLM, llama.cpp, Ollama with compat mode) return even on /api/chat.
type ollamaOpenAIResponse struct {
	Choices []struct {
		Message struct {
			Role             string          `json:"role"`
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// completeOllama sends a chat completion to a local Ollama instance.
// Ollama has no official Go SDK, so this uses direct HTTP.
// Endpoint: POST {baseURL}/api/chat with stream=false.
//
// Handles two response formats:
//   - OpenAI-compatible: {choices: [{message: {...}}], usage: {...}} — returned
//     by Ollama servers with compat mode, vLLM, llama.cpp, etc.
//   - Native Ollama: {message: {...}, model: "...", prompt_eval_count: N}
func completeOllama(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	ollamaTrace("=== completeOllama called, model=%s, messages=%d, hasTools=%v, agent=%s",
		req.Model, len(req.Messages), len(req.Tools) > 0, req.AgentName)

	// Log each message role + content type.
	for i, msg := range req.Messages {
		contentLen := len(msg.Content)
		hasRawContent := msg.RawContent != nil
		hasRawMessage := msg.RawMessage != nil
		ollamaTrace("  msg[%d] role=%s contentLen=%d hasRawContent=%v hasRawMessage=%v",
			i, msg.Role, contentLen, hasRawContent, hasRawMessage)
	}

	reqURL := strings.TrimRight(req.BaseURL, "/") + "/api/chat"

	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   false,
		Tools:    req.Tools,
	}

	// DEBUG: dump the full request being sent to Ollama for diagnostics.
	if debugData, debugErr := json.MarshalIndent(body, "", "  "); debugErr == nil {
		_ = os.WriteFile(filepath.Join("data", "ollama_debug.json"), debugData, 0o644)
	}

	respBody, err := ollamaPost(ctx, reqURL, body)
	if err != nil {
		ollamaTrace("  POST ERROR: %v", err)
		return nil, fmt.Errorf("provider.completeOllama: %w", err)
	}

	ollamaTrace("  response received, %d bytes", len(respBody))

	// Write the response dump for debugging (before any processing).
	if respDump, dumpErr := json.MarshalIndent(json.RawMessage(respBody), "", "  "); dumpErr == nil {
		_ = os.WriteFile(filepath.Join("data", "ollama_response_debug.json"), respDump, 0o644)
	}

	// Detect response format: OpenAI-compatible (has "choices") vs native Ollama (has "message").
	var formatProbe struct {
		Choices json.RawMessage `json:"choices"`
	}
	if json.Unmarshal(respBody, &formatProbe) == nil && len(formatProbe.Choices) > 2 {
		ollamaTrace("  detected OpenAI-compatible response format")
		return completeOllamaOpenAI(respBody, req.AgentName)
	}

	ollamaTrace("  detected native Ollama response format")
	return completeOllamaNative(respBody, req.AgentName)
}

// completeOllamaOpenAI handles Ollama responses in OpenAI-compatible format.
// Many Ollama deployments (vLLM, llama.cpp, compat mode) return this format
// even on /api/chat. The response is already in the format the OpenAI adapter
// expects, so we just extract display fields and pass through the raw JSON.
func completeOllamaOpenAI(respBody []byte, agentName string) (*CompletionResponse, error) {
	var resp ollamaOpenAIResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		ollamaTrace("  PARSE ERROR (OpenAI format): %v", err)
		return nil, fmt.Errorf("provider.completeOllama: parse OpenAI-format response: %w", err)
	}

	if len(resp.Choices) == 0 {
		ollamaTrace("  EMPTY RESPONSE: no choices in OpenAI-format response")
		return &CompletionResponse{
			Content:     "I received an empty response from the model. Let me try a different approach.",
			Model:       resp.Model,
			StopReason:  "stop",
			RawResponse: respBody,
		}, nil
	}

	choice := resp.Choices[0]
	displayContent := choice.Message.Content
	ollamaTrace("  model=%s, role=%s, contentLen=%d, reasoningContentLen=%d, finishReason=%s, hasToolCalls=%v",
		resp.Model, choice.Message.Role, len(choice.Message.Content),
		len(choice.Message.ReasoningContent), choice.FinishReason,
		len(choice.Message.ToolCalls) > 2)

	// Reasoning models (Qwen3.5, DeepSeek R1) put chain-of-thought in
	// reasoning_content and leave content empty. Fall back to a summary
	// if content is empty but reasoning exists.
	if displayContent == "" && choice.Message.ReasoningContent != "" {
		ollamaTrace("  content empty, reasoning_content present (%d chars)", len(choice.Message.ReasoningContent))
	}

	// Strip <think>...</think> tags if present in content (some models
	// put thinking in the content field instead of reasoning_content).
	if idx := strings.Index(displayContent, "</think>"); idx >= 0 {
		displayContent = strings.TrimSpace(displayContent[idx+len("</think>"):])
		ollamaTrace("  stripped <think> tags, displayContent=%d chars", len(displayContent))
	}

	// Map finish_reason to our internal stop reason.
	stopReason := "stop"
	if choice.FinishReason == "tool_calls" || len(choice.Message.ToolCalls) > 2 {
		stopReason = "tool_calls"
	}

	// Log tool calls if present.
	if len(choice.Message.ToolCalls) > 2 {
		var toolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(choice.Message.ToolCalls, &toolCalls) == nil {
			for i, tc := range toolCalls {
				ollamaTrace("    tool_call[%d] id=%s name=%s", i, tc.ID, tc.Function.Name)
			}
		}
	}

	// Extract usage info.
	var usage *UsageInfo
	if resp.Usage.TotalTokens > 0 {
		usage = &UsageInfo{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
		ollamaTrace("  usage: prompt=%d, completion=%d, total=%d",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}

	// The response is already in OpenAI choices format — pass through directly.
	// The OpenAI adapter's ParseToolCalls and FormatTurnMessages will handle it.
	ollamaTrace("  passing through OpenAI-format response, stopReason=%s", stopReason)

	// Self-healing: if no content and no tool calls, return synthetic message.
	if displayContent == "" && stopReason == "stop" {
		ollamaTrace("  EMPTY RESPONSE: no content and no tool calls — returning synthetic recovery message")
		return &CompletionResponse{
			Content:     "I received an empty response from the model. Let me try a different approach.",
			Model:       resp.Model,
			StopReason:  "stop",
			RawResponse: respBody,
			Usage:       usage,
		}, nil
	}

	return &CompletionResponse{
		Content:     displayContent,
		Model:       resp.Model,
		StopReason:  stopReason,
		RawResponse: respBody,
		Usage:       usage,
	}, nil
}

// completeOllamaNative handles Ollama responses in native /api/chat format.
// Native format: {message: {role, content, tool_calls}, model, prompt_eval_count, eval_count}
func completeOllamaNative(respBody []byte, agentName string) (*CompletionResponse, error) {
	var resp ollamaNativeResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		ollamaTrace("  PARSE ERROR (native format): %v", err)
		return nil, fmt.Errorf("provider.completeOllama: parse native response: %w", err)
	}

	ollamaTrace("  model=%s, role=%s, contentLen=%d, hasToolCalls=%v, promptTokens=%d, completionTokens=%d",
		resp.Model, resp.Message.Role, len(resp.Message.Content),
		len(resp.Message.ToolCalls) > 2, resp.PromptEvalCount, resp.EvalCount)

	// Strip <think>...</think> tags from content.
	displayContent := resp.Message.Content
	if idx := strings.Index(displayContent, "</think>"); idx >= 0 {
		displayContent = strings.TrimSpace(displayContent[idx+len("</think>"):])
		ollamaTrace("  stripped <think> tags, displayContent=%d chars", len(displayContent))
	}

	// Detect tool calls.
	stopReason := "stop"
	var rawMsg struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(respBody, &rawMsg); err != nil {
		ollamaTrace("  ERROR: unmarshal raw message: %v", err)
		slog.Error("failed to unmarshal Ollama response for normalization", "error", err)
	}

	// Native Ollama tool_calls may lack "id" and "type" fields. Inject them
	// so the OpenAI adapter can process them correctly.
	if len(resp.Message.ToolCalls) > 2 {
		stopReason = "tool_calls"

		var toolCalls []json.RawMessage
		if json.Unmarshal(resp.Message.ToolCalls, &toolCalls) == nil {
			ollamaTrace("  found %d native tool calls, injecting synthetic IDs", len(toolCalls))

			patched := make([]json.RawMessage, len(toolCalls))
			for i, tc := range toolCalls {
				var parsed map[string]any
				if json.Unmarshal(tc, &parsed) != nil {
					patched[i] = tc
					continue
				}
				if _, ok := parsed["id"]; !ok {
					parsed["id"] = fmt.Sprintf("ollama-call-%d", i)
				}
				if _, ok := parsed["type"]; !ok {
					parsed["type"] = "function"
				}
				if b, err := json.Marshal(parsed); err == nil {
					patched[i] = b
				} else {
					patched[i] = tc
				}
				if fn, ok := parsed["function"].(map[string]any); ok {
					ollamaTrace("    tool_call[%d] id=%v name=%v", i, parsed["id"], fn["name"])
				}
			}

			// Rebuild the raw message with patched tool_calls.
			var msgMap map[string]any
			if json.Unmarshal(rawMsg.Message, &msgMap) == nil {
				patchedJSON, _ := json.Marshal(patched)
				msgMap["tool_calls"] = json.RawMessage(patchedJSON)
				if rebuilt, err := json.Marshal(msgMap); err == nil {
					rawMsg.Message = rebuilt
				}
			}
		}
	}

	// Normalize native format to OpenAI choices format for the adapter layer.
	normalized, err := json.Marshal(struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}{
		Choices: []struct {
			Message json.RawMessage `json:"message"`
		}{
			{Message: rawMsg.Message},
		},
	})
	if err != nil {
		ollamaTrace("  ERROR: normalize to OpenAI format: %v", err)
		slog.Error("failed to normalize Ollama response to OpenAI format", "error", err)
		normalized = respBody
	}

	ollamaTrace("  normalized response: %d bytes, stopReason=%s", len(normalized), stopReason)

	// Extract usage info from native fields.
	var usage *UsageInfo
	if resp.PromptEvalCount > 0 || resp.EvalCount > 0 {
		usage = &UsageInfo{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		}
	}

	// Self-healing: if no content and no tool calls, return synthetic message.
	if displayContent == "" && stopReason == "stop" {
		ollamaTrace("  EMPTY RESPONSE: no content and no tool calls — returning synthetic recovery message")
		return &CompletionResponse{
			Content:     "I received an empty response from the model. Let me try a different approach.",
			Model:       resp.Model,
			StopReason:  "stop",
			RawResponse: normalized,
			Usage:       usage,
		}, nil
	}

	return &CompletionResponse{
		Content:     displayContent,
		Model:       resp.Model,
		StopReason:  stopReason,
		RawResponse: normalized,
		Usage:       usage,
	}, nil
}

// -- OpenAI completion (via openai-go SDK) ------------------------------------

// completeOpenAI sends a chat completion using the official openai-go SDK.
// Also used for "custom" providers which follow the OpenAI format.
// The SDK handles auth, request serialization, and retries.
// The raw JSON response is preserved for tool-use adapter compatibility.
func completeOpenAI(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	opts := []openaiopt.RequestOption{
		openaiopt.WithAPIKey(req.APIKey),
		openaiopt.WithHTTPClient(&http.Client{Timeout: completionTimeout}),
	}
	if req.BaseURL != "" {
		opts = append(opts, openaiopt.WithBaseURL(strings.TrimRight(req.BaseURL, "/")+"/"))
	}

	client := openai.NewClient(opts...)

	// Convert messages: pre-formatted messages (RawMessage/RawContent from
	// tool-use bridge) are unmarshalled into SDK types; plain messages use helpers.
	sdkMsgs, err := convertOpenAIMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("provider.completeOpenAI: %w", err)
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: sdkMsgs,
	}

	// Convert tools from the adapter's JSON format.
	if len(req.Tools) > 0 {
		var tools []openai.ChatCompletionToolParam
		if err := json.Unmarshal(req.Tools, &tools); err != nil {
			return nil, fmt.Errorf("provider.completeOpenAI: parse tools: %w", err)
		}
		params.Tools = tools
	}

	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("provider.completeOpenAI: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("provider.completeOpenAI: no choices in response")
	}

	// Use the SDK's RawJSON() to get the exact API response for adapter compatibility.
	rawResp := json.RawMessage(resp.RawJSON())

	stopReason := resp.Choices[0].FinishReason
	if stopReason == "" {
		stopReason = "stop"
	}

	result := &CompletionResponse{
		Content:     resp.Choices[0].Message.Content,
		Model:       resp.Model,
		StopReason:  stopReason,
		RawResponse: rawResp,
		Usage: &UsageInfo{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
			CacheReadTokens:  int(resp.Usage.PromptTokensDetails.CachedTokens),
		},
	}

	return result, nil
}

// convertOpenAIMessages transforms ChatMessage slice into SDK message types.
// Messages with RawMessage are pre-formatted JSON from the tool-use bridge
// and are unmarshalled directly into the SDK union type.
func convertOpenAIMessages(msgs []ChatMessage) ([]openai.ChatCompletionMessageParamUnion, error) {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, msg := range msgs {
		if msg.RawMessage != nil {
			// Pre-formatted full message from tool-use bridge.
			var sdkMsg openai.ChatCompletionMessageParamUnion
			if err := json.Unmarshal(msg.RawMessage, &sdkMsg); err != nil {
				return nil, fmt.Errorf("unmarshal raw message: %w", err)
			}
			result = append(result, sdkMsg)
			continue
		}
		if msg.RawContent != nil {
			// Content-override message — build with role + raw content.
			var sdkMsg openai.ChatCompletionMessageParamUnion
			raw, err := json.Marshal(struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}{Role: msg.Role, Content: msg.RawContent})
			if err != nil {
				return nil, fmt.Errorf("marshal raw content message: %w", err)
			}
			if err := json.Unmarshal(raw, &sdkMsg); err != nil {
				return nil, fmt.Errorf("unmarshal raw content message: %w", err)
			}
			result = append(result, sdkMsg)
			continue
		}
		// Plain text messages use the SDK helper functions.
		switch msg.Role {
		case "system":
			result = append(result, openai.SystemMessage(msg.Content))
		case "user":
			result = append(result, openai.UserMessage(msg.Content))
		case "assistant":
			result = append(result, openai.AssistantMessage(msg.Content))
		default:
			// For unknown roles (e.g. "tool"), unmarshal from JSON.
			var sdkMsg openai.ChatCompletionMessageParamUnion
			raw, _ := json.Marshal(msg)
			if err := json.Unmarshal(raw, &sdkMsg); err != nil {
				return nil, fmt.Errorf("unmarshal %s message: %w", msg.Role, err)
			}
			result = append(result, sdkMsg)
		}
	}
	return result, nil
}

// -- Anthropic completion (via anthropic-sdk-go) -----------------------------

// anthropicCacheControl marks a content block for Anthropic prompt caching.
// Ephemeral caching keeps the prefix cached for ~5 minutes after last use.
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// completeAnthropic sends a chat completion using the official anthropic-sdk-go SDK.
// System messages are extracted and placed in the top-level system field.
//
// Prompt caching: the SDK's top-level CacheControl field auto-applies
// cache_control=ephemeral to the last cacheable block. Manual breakpoints
// are added on tool definitions and the conversation history prefix.
func completeAnthropic(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	opts := []anthropicopt.RequestOption{
		anthropicopt.WithAPIKey(req.APIKey),
		anthropicopt.WithHTTPClient(&http.Client{Timeout: completionTimeout}),
	}
	if req.BaseURL != "" {
		opts = append(opts, anthropicopt.WithBaseURL(strings.TrimRight(req.BaseURL, "/")))
	}

	client := anthropic.NewClient(opts...)

	// Extract system messages — Anthropic requires them as a top-level field.
	var systemBlocks []anthropic.TextBlockParam
	var nonSystemMessages []ChatMessage
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: msg.Content,
			})
		} else {
			nonSystemMessages = append(nonSystemMessages, msg)
		}
	}

	// Mark the last system block for caching.
	if len(systemBlocks) > 0 {
		systemBlocks[len(systemBlocks)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}

	// Mark the last tool definition for caching.
	cachedTools := injectToolCacheControl(req.Tools)

	// Build SDK messages with cache breakpoints on the conversation prefix.
	sdkMsgs := marshalAnthropicSDKMessages(nonSystemMessages)

	// Convert tools from adapter JSON to SDK types.
	var sdkTools []anthropic.ToolUnionParam
	if len(cachedTools) > 0 {
		if err := json.Unmarshal(cachedTools, &sdkTools); err != nil {
			return nil, fmt.Errorf("provider.completeAnthropic: parse tools: %w", err)
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		Messages:  sdkMsgs,
		MaxTokens: 4096,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if len(sdkTools) > 0 {
		params.Tools = sdkTools
	}

	resp, err := client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("provider.completeAnthropic: %w", err)
	}

	if len(resp.Content) == 0 {
		return nil, fmt.Errorf("provider.completeAnthropic: no content blocks in response")
	}

	// Use the SDK's RawJSON() to get the exact API response for adapter compatibility.
	rawResp := json.RawMessage(resp.RawJSON())

	// Extract text from the first text block.
	var textContent string
	for _, block := range resp.Content {
		if block.Type == "text" {
			textContent = block.Text
			break
		}
	}

	stopReason := string(resp.StopReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}

	result := &CompletionResponse{
		Content:     textContent,
		Model:       string(resp.Model),
		StopReason:  stopReason,
		RawResponse: rawResp,
		Usage: &UsageInfo{
			PromptTokens:        int(resp.Usage.InputTokens),
			CompletionTokens:    int(resp.Usage.OutputTokens),
			TotalTokens:         int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
			CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
			CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
		},
	}

	return result, nil
}

// injectToolCacheControl adds cache_control=ephemeral to the last tool
// definition in the JSON array so Anthropic caches the tool surface.
// Returns the original tools if injection fails or tools are empty.
func injectToolCacheControl(tools json.RawMessage) json.RawMessage {
	if len(tools) == 0 {
		return tools
	}
	var toolArray []json.RawMessage
	if err := json.Unmarshal(tools, &toolArray); err != nil || len(toolArray) == 0 {
		return tools
	}

	// Parse the last tool as a generic map and inject cache_control.
	var lastTool map[string]json.RawMessage
	if err := json.Unmarshal(toolArray[len(toolArray)-1], &lastTool); err != nil {
		return tools
	}
	cc, _ := json.Marshal(anthropicCacheControl{Type: "ephemeral"})
	lastTool["cache_control"] = cc
	modified, err := json.Marshal(lastTool)
	if err != nil {
		return tools
	}
	toolArray[len(toolArray)-1] = modified

	result, err := json.Marshal(toolArray)
	if err != nil {
		return tools
	}
	return result
}

// marshalAnthropicSDKMessages converts ChatMessages to Anthropic SDK MessageParam
// types, adding cache_control=ephemeral as a breakpoint on the conversation
// history prefix (all messages except the final user message).
func marshalAnthropicSDKMessages(msgs []ChatMessage) []anthropic.MessageParam {
	if len(msgs) == 0 {
		return nil
	}

	// Find the cache breakpoint: last message before the final user message.
	breakpoint := -1
	if len(msgs) >= 2 {
		breakpoint = len(msgs) - 2
	}

	result := make([]anthropic.MessageParam, 0, len(msgs))
	for i, msg := range msgs {
		if msg.RawMessage != nil {
			// Pre-formatted full message from tool-use bridge — unmarshal directly.
			var sdkMsg anthropic.MessageParam
			if err := json.Unmarshal(msg.RawMessage, &sdkMsg); err != nil {
				slog.Error("failed to unmarshal Anthropic raw message", "error", err)
				continue
			}
			result = append(result, sdkMsg)
			continue
		}

		if msg.RawContent != nil {
			// RawContent from the tool-use bridge — unmarshal content blocks.
			var blocks []anthropic.ContentBlockParamUnion
			if err := json.Unmarshal(msg.RawContent, &blocks); err != nil {
				slog.Error("failed to unmarshal Anthropic raw content", "error", err)
				continue
			}

			// At cache breakpoint, inject cache_control on the last block.
			if i == breakpoint && len(blocks) > 0 {
				injectBlockCacheControl(blocks)
			}

			role := anthropic.MessageParamRole(msg.Role)
			result = append(result, anthropic.MessageParam{
				Role:    role,
				Content: blocks,
			})
			continue
		}

		// Plain text content.
		role := anthropic.MessageParamRole(msg.Role)
		if i == breakpoint {
			// Add cache_control to this breakpoint message.
			result = append(result, anthropic.MessageParam{
				Role: role,
				Content: []anthropic.ContentBlockParamUnion{
					{OfText: &anthropic.TextBlockParam{
						Text:         msg.Content,
						CacheControl: anthropic.NewCacheControlEphemeralParam(),
					}},
				},
			})
		} else {
			result = append(result, anthropic.MessageParam{
				Role:    role,
				Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)},
			})
		}
	}
	return result
}

// injectBlockCacheControl sets cache_control=ephemeral on the last block in
// a content block slice by re-marshalling it with the cache_control field.
func injectBlockCacheControl(blocks []anthropic.ContentBlockParamUnion) {
	if len(blocks) == 0 {
		return
	}
	last := &blocks[len(blocks)-1]
	// Try each variant to inject cache_control.
	cc := anthropic.NewCacheControlEphemeralParam()
	if last.OfText != nil {
		last.OfText.CacheControl = cc
	} else if last.OfToolUse != nil {
		last.OfToolUse.CacheControl = cc
	} else if last.OfToolResult != nil {
		last.OfToolResult.CacheControl = cc
	}
}

// -- Azure completion (via openai-go SDK) ------------------------------------

// completeAzure sends a chat completion to the Azure OpenAI Service using
// the openai-go SDK. Azure uses a deployment-based URL pattern and api-key
// header authentication instead of Bearer token.
func completeAzure(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Azure deployment URL: {baseURL}/openai/deployments/{model}/chat/completions?api-version=...
	// The SDK appends "chat/completions" to the base URL, so we set the base to
	// include the deployment prefix.
	azureBase := fmt.Sprintf("%s/openai/deployments/%s/",
		strings.TrimRight(req.BaseURL, "/"), req.Model)

	opts := []openaiopt.RequestOption{
		openaiopt.WithBaseURL(azureBase),
		// Azure uses api-key header, not Bearer auth. Set a dummy API key to
		// suppress the SDK's default env lookup, then override the header.
		openaiopt.WithAPIKey("azure-placeholder"),
		openaiopt.WithHeader("api-key", req.APIKey),
		// Remove the default Bearer Authorization header the SDK would set.
		openaiopt.WithHeader("Authorization", ""),
		openaiopt.WithQueryAdd("api-version", "2024-02-01"),
		openaiopt.WithHTTPClient(&http.Client{Timeout: completionTimeout}),
	}

	client := openai.NewClient(opts...)

	// Convert messages.
	sdkMsgs, err := convertOpenAIMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("provider.completeAzure: %w", err)
	}

	// Azure doesn't use the model field in the request body (it's in the URL).
	// The SDK sends it anyway, which Azure ignores.
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: sdkMsgs,
	}

	if len(req.Tools) > 0 {
		var tools []openai.ChatCompletionToolParam
		if err := json.Unmarshal(req.Tools, &tools); err != nil {
			return nil, fmt.Errorf("provider.completeAzure: parse tools: %w", err)
		}
		params.Tools = tools
	}

	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("provider.completeAzure: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("provider.completeAzure: no choices in response")
	}

	rawResp := json.RawMessage(resp.RawJSON())

	stopReason := resp.Choices[0].FinishReason
	if stopReason == "" {
		stopReason = "stop"
	}

	result := &CompletionResponse{
		Content:     resp.Choices[0].Message.Content,
		Model:       resp.Model,
		StopReason:  stopReason,
		RawResponse: rawResp,
		Usage: &UsageInfo{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
			CacheReadTokens:  int(resp.Usage.PromptTokensDetails.CachedTokens),
		},
	}

	return result, nil
}

// -- Google Gemini completion (via go-genai SDK) -----------------------------

// completeGoogle sends a chat completion to the Google Gemini API using the
// official go-genai SDK. System messages are extracted and placed in the
// GenerateContentConfig.SystemInstruction field. Gemini uses "model" instead
// of "assistant" for the assistant role.
//
// The SDK handles HTTP transport, authentication, and request/response
// serialization. The response is marshalled back to JSON for RawResponse so
// the GoogleAdapter (ParseToolCalls, FormatTurnMessages) can consume it
// through the same json.RawMessage interface used by all other providers.
// googleTrace appends a timestamped line to data/google_trace.log for
// diagnosing the tool-use round-trip. Safe to call concurrently (append-only).
func googleTrace(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05.000"), msg)
	f, err := os.OpenFile(filepath.Join("data", "google_trace.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("googleTrace: failed to open trace log", "error", err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("googleTrace: failed to close trace log", "error", cerr)
		}
	}()
	if _, werr := f.WriteString(line); werr != nil {
		slog.Warn("googleTrace: failed to write trace log", "error", werr)
	}
}


func completeGoogle(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	googleTrace("=== completeGoogle called, model=%s, messages=%d, hasTools=%v, agent=%s",
		req.Model, len(req.Messages), len(req.Tools) > 0, req.AgentName)

	// Log each message role + content type.
	for i, msg := range req.Messages {
		contentLen := len(msg.Content)
		hasRawContent := msg.RawContent != nil
		hasRawMessage := msg.RawMessage != nil
		googleTrace("  msg[%d] role=%s contentLen=%d hasRawContent=%v hasRawMessage=%v",
			i, msg.Role, contentLen, hasRawContent, hasRawMessage)
	}

	// Build the SDK client. The client is lightweight — it's safe to create
	// per-request since it only holds config, no persistent connections.
	clientCfg := &genai.ClientConfig{
		APIKey:  req.APIKey,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			Timeout: genai.Ptr(completionTimeout),
		},
	}
	// Support custom base URL for proxies or self-hosted endpoints.
	if req.BaseURL != "" && req.BaseURL != "https://generativelanguage.googleapis.com" {
		clientCfg.HTTPOptions.BaseURL = strings.TrimRight(req.BaseURL, "/")
	}
	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("provider.completeGoogle: create SDK client: %w", err)
	}
	// Note: genai.Client has no Close() method — it is lightweight/stateless
	// with no persistent connections requiring cleanup.

	// Build GenerateContentConfig with system instruction, tools, and thinking config.
	config := &genai.GenerateContentConfig{}

	// Extract system messages — Gemini uses a SystemInstruction field.
	var systemTextParts []*genai.Part
	var contents []*genai.Content
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			if msg.Content != "" {
				systemTextParts = append(systemTextParts, genai.NewPartFromText(msg.Content))
			}
		default:
			role := msg.Role
			if role == "assistant" {
				role = "model"
			}
			// Gemini API only accepts "user" or "model" — function responses
			// use "user" role with functionResponse parts.
			if role == "function" {
				role = "user"
			}

			if msg.RawContent != nil {
				// RawContent from the tool-use adapter — Gemini-formatted parts
				// (functionCall or functionResponse blocks). Unmarshal into SDK Part types.
				var parts []*genai.Part
				if err := json.Unmarshal(msg.RawContent, &parts); err != nil {
					return nil, fmt.Errorf("provider.completeGoogle: unmarshal raw content parts: %w", err)
				}
				contents = append(contents, &genai.Content{
					Role:  role,
					Parts: parts,
				})
			} else if msg.Content != "" {
				// Plain text content. Skip empty messages — Gemini requires
				// every Part to have a populated data field (text, functionCall,
				// etc). Empty text produces a Part with no data oneof set,
				// which the API rejects with INVALID_ARGUMENT.
				contents = append(contents, genai.NewContentFromText(msg.Content, genai.Role(role)))
			}
		}
	}

	// Log the contents we built.
	googleTrace("  built %d content blocks, %d system parts", len(contents), len(systemTextParts))
	for i, c := range contents {
		partSummary := ""
		for j, p := range c.Parts {
			kind := "unknown"
			if p.Text != "" {
				kind = fmt.Sprintf("text(%d chars)", len(p.Text))
			}
			if p.FunctionCall != nil {
				kind = fmt.Sprintf("functionCall(name=%s, id=%s)", p.FunctionCall.Name, p.FunctionCall.ID)
			}
			if p.FunctionResponse != nil {
				kind = fmt.Sprintf("functionResponse(name=%s, id=%s)", p.FunctionResponse.Name, p.FunctionResponse.ID)
			}
			hasSig := len(p.ThoughtSignature) > 0
			if hasSig {
				kind += fmt.Sprintf("+thoughtSig(%d bytes)", len(p.ThoughtSignature))
			}
			if p.Thought {
				kind += "+thought"
			}
			if j > 0 {
				partSummary += ", "
			}
			partSummary += kind
		}
		googleTrace("  content[%d] role=%s parts=%d: [%s]", i, c.Role, len(c.Parts), partSummary)
	}

	// Merge consecutive same-role contents. Skipping empty messages above can
	// break the strict user/model role alternation that Gemini requires. When
	// two adjacent Content entries have the same role, merge the second's Parts
	// into the first to restore valid alternation.
	if len(contents) > 1 {
		merged := make([]*genai.Content, 0, len(contents))
		merged = append(merged, contents[0])
		for i := 1; i < len(contents); i++ {
			last := merged[len(merged)-1]
			if last.Role == contents[i].Role {
				last.Parts = append(last.Parts, contents[i].Parts...)
			} else {
				merged = append(merged, contents[i])
			}
		}
		contents = merged
	}

	if len(systemTextParts) > 0 {
		config.SystemInstruction = &genai.Content{
			Parts: systemTextParts,
		}
	}

	// Convert tools from the adapter's JSON format to SDK types.
	// req.Tools is a Gemini-formatted JSON: {functionDeclarations: [{name, description, parameters}]}
	if len(req.Tools) > 0 {
		sdkTools, toolErr := parseToolsToSDK(req.Tools)
		if toolErr != nil {
			return nil, fmt.Errorf("provider.completeGoogle: parse tools: %w", toolErr)
		}
		config.Tools = sdkTools

		// Allow thinking for tool-use requests. Gemini 2.5+ models emit
		// thoughtSignature on functionCall parts, and the API REQUIRES these
		// signatures for correct round-trip function calling. Our
		// FormatTurnMessages in adapter_google.go preserves thoughtSignature
		// by extracting parts as json.RawMessage (not typed structs), ensuring
		// the opaque signature bytes survive the round-trip.
	}

	// DEBUG: dump the full contents being sent to Gemini so we can diagnose
	// iteration 2 INVALID_ARGUMENT errors. Written to data/google_debug.json.
	if debugData, debugErr := json.MarshalIndent(struct {
		Model    string                       `json:"model"`
		Contents []*genai.Content             `json:"contents"`
		Config   *genai.GenerateContentConfig `json:"config"`
	}{
		Model:    req.Model,
		Contents: contents,
		Config:   config,
	}, "", "  "); debugErr == nil {
		if writeErr := os.WriteFile(filepath.Join("data", "google_debug.json"), debugData, 0o644); writeErr != nil {
			slog.Warn("completeGoogle: failed to write debug file", "error", writeErr)
		}
	}


	// Log post-merge state.
	googleTrace("  after merge: %d content blocks", len(contents))
	for i, c := range contents {
		googleTrace("    merged[%d] role=%s parts=%d", i, c.Role, len(c.Parts))
	}

	googleTrace("  calling SDK GenerateContent model=%s", req.Model)
	// Call the SDK.
	resp, err := client.Models.GenerateContent(ctx, req.Model, contents, config)
	if err != nil {
		googleTrace("  SDK ERROR: %v", err)
		return nil, fmt.Errorf("provider.completeGoogle: %w", err)
	}
	googleTrace("  SDK response received, candidates=%d", len(resp.Candidates))

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil ||
		len(resp.Candidates[0].Content.Parts) == 0 {
		if len(resp.Candidates) > 0 {
			c := resp.Candidates[0]
			googleTrace("  EMPTY RESPONSE: finishReason=%s, finishMessage=%q, hasContent=%v",
				c.FinishReason, c.FinishMessage, c.Content != nil)

			// MALFORMED_FUNCTION_CALL is a Gemini model-side issue — the model
			// generated an invalid function call. Instead of returning a hard error
			// that breaks the tool-use loop, return a synthetic text response so
			// the executor treats it as a final (non-tool-use) response. The agent
			// session handler can then retry or surface the message.
			if c.FinishReason == "MALFORMED_FUNCTION_CALL" {
				googleTrace("  recovering from MALFORMED_FUNCTION_CALL with synthetic response")
				rawResp, _ := json.Marshal(resp)
				return &CompletionResponse{
					Content:     "I attempted to use a tool but the request was malformed. Let me try a different approach.",
					Model:       resp.ModelVersion,
					StopReason:  "stop",
					RawResponse: rawResp,
				}, nil
			}

			return nil, fmt.Errorf("provider.completeGoogle: no content in response (finishReason=%s, finishMessage=%q)", c.FinishReason, c.FinishMessage)
		}
		googleTrace("  EMPTY RESPONSE: no candidates at all")
		return nil, fmt.Errorf("provider.completeGoogle: no candidates in response")
	}

	// Log response parts.
	for i, part := range resp.Candidates[0].Content.Parts {
		kind := "unknown"
		if part.Text != "" {
			kind = fmt.Sprintf("text(%d chars, thought=%v)", len(part.Text), part.Thought)
		}
		if part.FunctionCall != nil {
			kind = fmt.Sprintf("functionCall(name=%s, id=%s)", part.FunctionCall.Name, part.FunctionCall.ID)
		}
		hasSig := len(part.ThoughtSignature) > 0
		googleTrace("  response part[%d]: %s hasSig=%v", i, kind, hasSig)
	}

	// Marshal the SDK response to JSON for RawResponse. The adapter layer
	// (ParseToolCalls, FormatTurnMessages) consumes this as json.RawMessage
	// to extract functionCall parts and preserve all fields (thoughtSignature, etc.)
	// for round-trip fidelity.
	rawResp, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("provider.completeGoogle: marshal response: %w", err)
	}

	// Extract text and detect function calls from SDK types.
	var textParts []string
	hasFunctionCall := len(resp.FunctionCalls()) > 0
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" && !part.Thought {
			textParts = append(textParts, part.Text)
		}
	}

	stopReason := "stop"
	if hasFunctionCall {
		stopReason = "tool_use"
	}

	result := &CompletionResponse{
		Content:     strings.Join(textParts, ""),
		Model:       resp.ModelVersion,
		StopReason:  stopReason,
		RawResponse: rawResp,
	}

	if resp.UsageMetadata != nil {
		result.Usage = &UsageInfo{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
			CacheReadTokens:  int(resp.UsageMetadata.CachedContentTokenCount),
		}
	}

	return result, nil
}

// parseToolsToSDK converts the adapter's Gemini-formatted tool JSON into SDK
// []*genai.Tool. The input is a JSON object: {functionDeclarations: [{name, description, parameters}]}.
// Each function's parameters field is a JSON Schema object, passed through as
// ParametersJsonSchema (the SDK's raw JSON Schema field).
func parseToolsToSDK(toolsJSON json.RawMessage) ([]*genai.Tool, error) {
	var wrapper struct {
		FunctionDeclarations []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		} `json:"functionDeclarations"`
	}
	if err := json.Unmarshal(toolsJSON, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal tool declarations: %w", err)
	}

	decls := make([]*genai.FunctionDeclaration, len(wrapper.FunctionDeclarations))
	for i, fd := range wrapper.FunctionDeclarations {
		decls[i] = &genai.FunctionDeclaration{
			Name:                 fd.Name,
			Description:          fd.Description,
			ParametersJsonSchema: fd.Parameters,
		}
	}

	return []*genai.Tool{{FunctionDeclarations: decls}}, nil
}

// -- AWS Bedrock completion (via aws-sdk-go-v2) ------------------------------

// bedrockContentBlock is a content element in a Bedrock Converse message.
// Used for building requests and parsing responses in the wire format that
// the tool-use adapter expects.
type bedrockContentBlock struct {
	Text       *string            `json:"text,omitempty"`
	CachePoint *bedrockCachePoint `json:"cachePoint,omitempty"`
	ToolUse    json.RawMessage    `json:"toolUse,omitempty"`
	ToolResult json.RawMessage    `json:"toolResult,omitempty"`
}

// bedrockCachePoint marks a cache checkpoint in Bedrock Converse requests.
type bedrockCachePoint struct {
	Type string `json:"type"` // "default" (Bedrock uses "default", NOT "ephemeral")
}

// bedrockWireMessage is a single message in the Bedrock Converse wire format.
type bedrockWireMessage struct {
	Role    string                `json:"role"`
	Content []bedrockContentBlock `json:"content"`
}

// bedrockWireResponse is the response wire format from Bedrock's Converse API.
// This structure matches what the tool-use adapter (adapter_bedrock.go) expects.
type bedrockWireResponse struct {
	Output struct {
		Message *bedrockWireMessage `json:"message"`
	} `json:"output"`
	StopReason string `json:"stopReason"`
	Usage      *struct {
		InputTokens           int `json:"inputTokens"`
		OutputTokens          int `json:"outputTokens"`
		CacheReadInputTokens  int `json:"cacheReadInputTokens"`
		CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
	} `json:"usage,omitempty"`
}

// completeBedrock sends a chat completion to AWS Bedrock using the official
// aws-sdk-go-v2 Converse API. Auth is handled by the SDK with static credentials
// parsed from ACCESS_KEY_ID:SECRET_ACCESS_KEY format.
func completeBedrock(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	accessKey, secretKey, err := parseBedrockCredentials(req.APIKey)
	if err != nil {
		return nil, fmt.Errorf("provider.completeBedrock: %w", err)
	}

	region := extractBedrockRegion(req.BaseURL)

	// Build the SDK client with static credentials and custom endpoint.
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey, secretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("provider.completeBedrock: load AWS config: %w", err)
	}

	brClient := bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		if req.BaseURL != "" {
			o.BaseEndpoint = aws.String(strings.TrimRight(req.BaseURL, "/"))
		}
	})

	// Build Bedrock SDK messages and system blocks.
	var systemBlocks []brtypes.SystemContentBlock
	var messages []brtypes.Message
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{
				Value: msg.Content,
			})
			continue
		}
		if msg.RawContent != nil {
			// RawContent from the tool-use adapter — Bedrock-formatted content blocks.
			sdkBlocks := convertBedrockRawContent(msg.RawContent)
			if len(sdkBlocks) > 0 {
				messages = append(messages, brtypes.Message{
					Role:    brtypes.ConversationRole(msg.Role),
					Content: sdkBlocks,
				})
				continue
			}
		}
		messages = append(messages, brtypes.Message{
			Role: brtypes.ConversationRole(msg.Role),
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: msg.Content},
			},
		})
	}

	// Add cache breakpoints: last system block and last message before final user message.
	if len(systemBlocks) > 0 {
		systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberCachePoint{
			Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
		})
	}
	if len(messages) >= 2 {
		bp := len(messages) - 2
		messages[bp].Content = append(messages[bp].Content, &brtypes.ContentBlockMemberCachePoint{
			Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
		})
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(req.Model),
		Messages: messages,
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(4096),
		},
	}
	if len(systemBlocks) > 0 {
		input.System = systemBlocks
	}

	// Convert tools from adapter JSON to SDK types.
	if len(req.Tools) > 0 {
		toolConfig, toolErr := convertBedrockToolConfig(req.Tools)
		if toolErr != nil {
			return nil, fmt.Errorf("provider.completeBedrock: %w", toolErr)
		}
		input.ToolConfig = toolConfig
	}

	sdkResp, err := brClient.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("provider.completeBedrock: %w", err)
	}

	// Convert SDK response to wire-format JSON for adapter compatibility.
	wireResp := convertBedrockToWireResponse(sdkResp)
	respBody, err := json.Marshal(wireResp)
	if err != nil {
		return nil, fmt.Errorf("provider.completeBedrock: marshal response: %w", err)
	}

	if wireResp.Output.Message == nil || len(wireResp.Output.Message.Content) == 0 {
		return nil, fmt.Errorf("provider.completeBedrock: no content in response")
	}

	var textParts []string
	for _, block := range wireResp.Output.Message.Content {
		if block.Text != nil {
			textParts = append(textParts, *block.Text)
		}
	}

	stopReason := wireResp.StopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}

	result := &CompletionResponse{
		Content:     strings.Join(textParts, ""),
		Model:       req.Model,
		StopReason:  stopReason,
		RawResponse: respBody,
	}

	if sdkResp.Usage != nil {
		result.Usage = &UsageInfo{
			PromptTokens:        int(aws.ToInt32(sdkResp.Usage.InputTokens)),
			CompletionTokens:    int(aws.ToInt32(sdkResp.Usage.OutputTokens)),
			TotalTokens:         int(aws.ToInt32(sdkResp.Usage.TotalTokens)),
			CacheReadTokens:     int(aws.ToInt32(sdkResp.Usage.CacheReadInputTokens)),
			CacheCreationTokens: int(aws.ToInt32(sdkResp.Usage.CacheWriteInputTokens)),
		}
	}

	return result, nil
}

// parseBedrockCredentials parses "ACCESS_KEY_ID:SECRET_ACCESS_KEY" into components.
func parseBedrockCredentials(secret string) (accessKey, secretKey string, err error) {
	parts := strings.SplitN(secret, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid AWS credentials format: expected ACCESS_KEY_ID:SECRET_ACCESS_KEY")
	}
	return parts[0], parts[1], nil
}

// extractBedrockRegion extracts the region from a Bedrock URL like
// https://bedrock-runtime.us-east-1.amazonaws.com
func extractBedrockRegion(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "us-east-1"
	}
	parts := strings.Split(u.Hostname(), ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "us-east-1"
}

// convertBedrockRawContent converts adapter-formatted JSON content blocks
// into Bedrock SDK ContentBlock types.
func convertBedrockRawContent(raw json.RawMessage) []brtypes.ContentBlock {
	var blocks []bedrockContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var result []brtypes.ContentBlock
	for _, b := range blocks {
		if b.Text != nil {
			result = append(result, &brtypes.ContentBlockMemberText{Value: *b.Text})
		}
		if len(b.ToolUse) > 0 {
			var tu struct {
				ToolUseID string          `json:"toolUseId"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
			}
			if json.Unmarshal(b.ToolUse, &tu) == nil {
				// Convert Input JSON to a document.Interface via NewLazyDocument.
				var inputMap map[string]any
				_ = json.Unmarshal(tu.Input, &inputMap)
				result = append(result, &brtypes.ContentBlockMemberToolUse{
					Value: brtypes.ToolUseBlock{
						ToolUseId: aws.String(tu.ToolUseID),
						Name:      aws.String(tu.Name),
						Input:     brdoc.NewLazyDocument(inputMap),
					},
				})
			}
		}
		if len(b.ToolResult) > 0 {
			var tr struct {
				ToolUseID string `json:"toolUseId"`
				Status    string `json:"status,omitempty"`
				Content   []struct {
					Text *string         `json:"text,omitempty"`
					JSON json.RawMessage `json:"json,omitempty"`
				} `json:"content"`
			}
			if json.Unmarshal(b.ToolResult, &tr) == nil {
				var trContent []brtypes.ToolResultContentBlock
				for _, c := range tr.Content {
					if c.Text != nil {
						trContent = append(trContent, &brtypes.ToolResultContentBlockMemberText{Value: *c.Text})
					}
				}
				status := brtypes.ToolResultStatusSuccess
				if tr.Status == "error" {
					status = brtypes.ToolResultStatusError
				}
				result = append(result, &brtypes.ContentBlockMemberToolResult{
					Value: brtypes.ToolResultBlock{
						ToolUseId: aws.String(tr.ToolUseID),
						Content:   trContent,
						Status:    status,
					},
				})
			}
		}
	}
	return result
}

// convertBedrockToolConfig converts adapter-formatted tool JSON to the SDK's
// ToolConfiguration type. Input format: {tools: [{toolSpec: {name, description, inputSchema: {json: ...}}}]}
func convertBedrockToolConfig(toolsJSON json.RawMessage) (*brtypes.ToolConfiguration, error) {
	var wireConfig struct {
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
	if err := json.Unmarshal(toolsJSON, &wireConfig); err != nil {
		return nil, fmt.Errorf("parse tool config: %w", err)
	}

	var sdkTools []brtypes.Tool
	for _, t := range wireConfig.Tools {
		var schemaMap map[string]any
		_ = json.Unmarshal(t.ToolSpec.InputSchema.JSON, &schemaMap)
		sdkTools = append(sdkTools, &brtypes.ToolMemberToolSpec{
			Value: brtypes.ToolSpecification{
				Name:        aws.String(t.ToolSpec.Name),
				Description: aws.String(t.ToolSpec.Description),
				InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: brdoc.NewLazyDocument(schemaMap)},
			},
		})
	}

	return &brtypes.ToolConfiguration{Tools: sdkTools}, nil
}

// convertBedrockToWireResponse converts the SDK response to the wire format
// JSON structure that the adapter (adapter_bedrock.go) expects.
func convertBedrockToWireResponse(resp *bedrockruntime.ConverseOutput) bedrockWireResponse {
	wire := bedrockWireResponse{
		StopReason: string(resp.StopReason),
	}

	// Convert output message.
	if outputMsg, ok := resp.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		msg := &bedrockWireMessage{
			Role: string(outputMsg.Value.Role),
		}
		for _, block := range outputMsg.Value.Content {
			switch b := block.(type) {
			case *brtypes.ContentBlockMemberText:
				text := b.Value
				msg.Content = append(msg.Content, bedrockContentBlock{Text: &text})
			case *brtypes.ContentBlockMemberToolUse:
				// Marshal tool use to JSON for the adapter.
				// Use MarshalSmithyDocument to extract JSON from the document.Interface.
				inputJSON, _ := b.Value.Input.MarshalSmithyDocument()
				tuJSON, _ := json.Marshal(struct {
					ToolUseID string          `json:"toolUseId"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
				}{
					ToolUseID: aws.ToString(b.Value.ToolUseId),
					Name:      aws.ToString(b.Value.Name),
					Input:     inputJSON,
				})
				msg.Content = append(msg.Content, bedrockContentBlock{ToolUse: tuJSON})
			}
		}
		wire.Output.Message = msg
	}

	// Convert usage.
	if resp.Usage != nil {
		wire.Usage = &struct {
			InputTokens           int `json:"inputTokens"`
			OutputTokens          int `json:"outputTokens"`
			CacheReadInputTokens  int `json:"cacheReadInputTokens"`
			CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
		}{
			InputTokens:           int(aws.ToInt32(resp.Usage.InputTokens)),
			OutputTokens:          int(aws.ToInt32(resp.Usage.OutputTokens)),
			CacheReadInputTokens:  int(aws.ToInt32(resp.Usage.CacheReadInputTokens)),
			CacheWriteInputTokens: int(aws.ToInt32(resp.Usage.CacheWriteInputTokens)),
		}
	}

	return wire
}

// -- HTTP helper (Ollama only) -----------------------------------------------

// ollamaPost performs an HTTP POST request with JSON body and returns the
// response body. Used only for Ollama since it has no official Go SDK.
func ollamaPost(ctx context.Context, url string, body any) ([]byte, error) {
	client := &http.Client{Timeout: completionTimeout}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("provider.ollamaPost: marshal request body: %w", err)
	}

	ollamaTrace("  POST %s (%d bytes)", url, len(jsonBody))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("provider.ollamaPost: build request for %s: %w", url, err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		ollamaTrace("  POST ERROR: connect to %s: %v", url, err)
		return nil, fmt.Errorf("provider.ollamaPost: connect to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		ollamaTrace("  POST ERROR: read response: %v", err)
		return nil, fmt.Errorf("provider.ollamaPost: read response from %s: %w", url, err)
	}

	ollamaTrace("  POST %s → HTTP %d (%d bytes)", url, resp.StatusCode, len(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		ollamaTrace("  POST ERROR: HTTP %d: %s", resp.StatusCode, snippet)
		return nil, fmt.Errorf("provider.ollamaPost: HTTP %d from %s: %s", resp.StatusCode, url, snippet)
	}

	return respBody, nil
}
