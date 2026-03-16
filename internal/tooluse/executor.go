package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/guard"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// DefaultMaxIterations is the maximum number of tool-use loop iterations
// before the executor returns an error to prevent runaway loops.
const DefaultMaxIterations = 100

// DispatchFunc is the signature for the function that executes a single MCP
// tool call. It mirrors ToolRegistry.Dispatch.
type DispatchFunc func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)

// EventEmitter is an optional callback for publishing tool-use loop events
// to the Nervous System. If nil, no events are emitted.
type EventEmitter func(eventType types.EventType, payload any)

// ToolCallRecorder is an optional callback for recording tool call metrics
// to the telemetry session tracker. If nil, no metrics are recorded.
// Parameters: toolName, duration, success, errorMsg, inputSize, outputSize.
type ToolCallRecorder func(toolName string, duration time.Duration, success bool, errorMsg string, inputSize, outputSize int64)

// ExecutorConfig configures the tool-use execution loop.
type ExecutorConfig struct {
	// MaxIterations is the maximum number of tool-use round-trips before
	// returning an error. Defaults to DefaultMaxIterations if <= 0.
	MaxIterations int

	// AutoContinue, when true, causes the executor to reset the iteration
	// counter upon hitting MaxIterations instead of stopping. The loop
	// continues until the LLM produces a final response or the context
	// is cancelled. A checkpoint event is emitted at each reset.
	AutoContinue bool

	// PersonaID is the agent's UUID, injected into the dispatch context
	// so ABAC can identify the caller.
	PersonaID string

	// ClearanceLevel is the agent's ABAC clearance, injected into the
	// dispatch context so tools see the agent's actual permissions.
	ClearanceLevel int

	// AgentName is the display name of the agent running this executor.
	// Included in event payloads so the frontend can show which agent
	// is performing each tool-use action without parsing the event envelope.
	AgentName string

	// Dispatch executes a single tool call via the MCP registry.
	Dispatch DispatchFunc

	// Emitter publishes tool-use lifecycle events. Optional.
	Emitter EventEmitter

	// Recorder records tool call metrics to the telemetry system. Optional.
	Recorder ToolCallRecorder
}

// Executor runs the tool-use loop: resolve tools → send completion → parse
// tool calls → dispatch → format results → repeat until the LLM stops
// requesting tools or the iteration limit is reached.
type Executor struct {
	config   ExecutorConfig
	adapter  ProviderToolAdapter
	resolver *Resolver
	logger   *slog.Logger
}

// NewExecutor creates a tool-use loop executor.
func NewExecutor(config ExecutorConfig, adapter ProviderToolAdapter, resolver *Resolver, logger *slog.Logger) *Executor {
	if config.MaxIterations <= 0 {
		config.MaxIterations = DefaultMaxIterations
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		config:   config,
		adapter:  adapter,
		resolver: resolver,
		logger:   logger,
	}
}

// CompletionFunc is the function signature for sending a chat completion
// request. Matches provider.ChatCompletion.
type CompletionFunc func(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error)

// ExecuteResult holds the final response from the tool-use loop along with
// cumulative usage and iteration count.
type ExecuteResult struct {
	// Response is the final completion response (with Content, no tool calls).
	Response *provider.CompletionResponse

	// Iterations is how many completion round-trips were made.
	Iterations int

	// TotalUsage is the cumulative token usage across all iterations.
	TotalUsage provider.UsageInfo
}

// Execute runs the tool-use loop. It resolves the available tools for the
// given clearance level, formats them for the provider, and enters the
// completion loop until the LLM produces a final text response or the
// iteration limit is reached.
//
// If the resolver produces no tools, the request is passed through directly
// as a regular completion (no tool-use overhead).
func (e *Executor) Execute(
	ctx context.Context,
	completeFn CompletionFunc,
	req *provider.CompletionRequest,
	clearance int,
	delegScopes []string,
	allowedActions ...string,
) (*ExecuteResult, error) {
	// Resolve which tools this persona can see (filtered by ABAC + role actions).
	toolDefs := e.resolver.ResolveTools(clearance, delegScopes, allowedActions...)

	// If no tools are available, pass through as a regular completion.
	if len(toolDefs) == 0 {
		resp, err := completeFn(ctx, req)
		if err != nil {
			return nil, err
		}
		result := &ExecuteResult{Response: resp, Iterations: 1}
		if resp.Usage != nil {
			result.TotalUsage = *resp.Usage
		}
		return result, nil
	}

	// Format tools for the provider.
	formattedTools, err := e.adapter.FormatTools(toolDefs)
	if err != nil {
		return nil, fmt.Errorf("tooluse.Executor.Execute: format tools: %w", err)
	}
	req.Tools = formattedTools

	e.emit(types.EventToolUseLoopStart, map[string]any{
		"tool_count":     len(toolDefs),
		"max_iterations": e.config.MaxIterations,
		"provider":       req.Kind,
		"agent_name":     e.config.AgentName,
	})

	var totalUsage provider.UsageInfo
	var lastCallKey string              // For cycle detection: "name:args"
	var lastTextResp *provider.CompletionResponse // Track last response with text content

	for iteration := 1; ; iteration++ {
		// Check iteration limit (auto-continue resets the window).
		if iteration > e.config.MaxIterations {
			if e.config.AutoContinue {
				// Emit checkpoint event at each multiple of MaxIterations.
				if (iteration-1)%e.config.MaxIterations == 0 {
					e.logger.Info("auto-continue: extending tool-use loop",
						"iteration", iteration,
						"checkpoint", e.config.MaxIterations,
					)
					e.emit(types.EventToolUseAutoExtend, map[string]any{
						"iteration":      iteration,
						"checkpoint":     e.config.MaxIterations,
						"total_usage":    totalUsage,
						"agent_name":     e.config.AgentName,
					})
				}
			} else {
				break // Exit loop — handled below.
			}
		}
		resp, err := completeFn(ctx, req)
		if err != nil {
			e.emit(types.EventToolUseLoopError, map[string]any{
				"iteration":  iteration,
				"error":      err.Error(),
				"agent_name": e.config.AgentName,
			})
			// If we have a previous text response, return it instead of erroring.
			if lastTextResp != nil {
				e.logger.Warn("completion failed, returning last text response",
					"iteration", iteration, "error", err)
				return &ExecuteResult{
					Response:   lastTextResp,
					Iterations: iteration,
					TotalUsage: totalUsage,
				}, nil
			}
			return nil, fmt.Errorf("tooluse.Executor.Execute: completion (iteration %d): %w", iteration, err)
		}

		// Accumulate usage (including cache stats for providers that support it).
		if resp.Usage != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens += resp.Usage.TotalTokens
			totalUsage.CacheCreationTokens += resp.Usage.CacheCreationTokens
			totalUsage.CacheReadTokens += resp.Usage.CacheReadTokens
		}

		// Track responses that contain text content (tool_use responses
		// often include text like "Let me check..." alongside tool calls).
		if resp.Content != "" {
			lastTextResp = resp
		}

		// Parse tool calls from the response. We always attempt parsing
		// regardless of stop reason because some models (e.g. Ollama models
		// that don't support native function calling) output tool calls as
		// plain text JSON with a "stop" reason instead of "tool_calls".
		// The adapter's ParseToolCalls handles both structured tool_calls
		// and text-based extraction as a fallback.
		calls, err := e.adapter.ParseToolCalls(resp.RawResponse)
		if err != nil {
			return nil, fmt.Errorf("tooluse.Executor.Execute: parse tool calls (iteration %d): %w", iteration, err)
		}

		if len(calls) == 0 {
			// No tool calls found (structured or text-extracted) — final response.
			e.emit(types.EventToolUseLoopComplete, map[string]any{
				"iterations":  iteration,
				"total_usage": totalUsage,
				"agent_name":  e.config.AgentName,
			})
			return &ExecuteResult{
				Response:   resp,
				Iterations: iteration,
				TotalUsage: totalUsage,
			}, nil
		}

		e.emit(types.EventToolUseLoopIteration, map[string]any{
			"iteration":  iteration,
			"call_count": len(calls),
			"tools":      toolCallNames(calls),
			"agent_name": e.config.AgentName,
		})

		// Cycle detection: if a single tool call with identical args repeats.
		if len(calls) == 1 {
			callKey := calls[0].Name + ":" + string(calls[0].Arguments)
			if callKey == lastCallKey {
				e.emit(types.EventToolUseCycleDetected, map[string]any{
					"iteration":  iteration,
					"tool":       calls[0].Name,
					"agent_name": e.config.AgentName,
				})
				e.logger.Warn("tool-use cycle detected, breaking loop",
					"tool", calls[0].Name,
					"iteration", iteration,
				)
				return &ExecuteResult{
					Response:   resp,
					Iterations: iteration,
					TotalUsage: totalUsage,
				}, nil
			}
			lastCallKey = callKey
		} else {
			lastCallKey = "" // Reset cycle detection for multi-call turns.
		}

		// Dispatch each tool call.
		results := make([]types.ToolCallResult, len(calls))
		for i, call := range calls {
			e.emit(types.EventToolUseToolDispatch, map[string]any{
				"iteration":  iteration,
				"tool":       call.Name,
				"call_id":    call.ID,
				"agent_name": e.config.AgentName,
			})
			result := e.dispatchCall(ctx, call)
			results[i] = result
		}

		// Format results for the provider and append to messages.
		formattedResults, err := e.adapter.FormatToolResults(results)
		if err != nil {
			return nil, fmt.Errorf("tooluse.Executor.Execute: format tool results (iteration %d): %w", iteration, err)
		}

		// Build provider-specific conversation turn messages (assistant + tool results).
		turnMsgs, fmtErr := e.adapter.FormatTurnMessages(resp.RawResponse, formattedResults)
		if fmtErr != nil {
			return nil, fmt.Errorf("tooluse.Executor.Execute: format turn messages (iteration %d): %w", iteration, fmtErr)
		}
		req.Messages = append(req.Messages, turnMsgs...)
	}

	// Max iterations reached (auto_continue=false). Return the last text
	// response if available, otherwise error.
	e.logger.Warn("tool-use loop hit max iterations",
		"limit", e.config.MaxIterations,
		"has_text_response", lastTextResp != nil,
		"auto_continue", e.config.AutoContinue)

	e.emit(types.EventToolUseMaxIterReached, map[string]any{
		"limit":             e.config.MaxIterations,
		"has_text_response": lastTextResp != nil,
		"total_usage":       totalUsage,
		"agent_name":        e.config.AgentName,
	})

	if lastTextResp != nil {
		e.emit(types.EventToolUseLoopComplete, map[string]any{
			"iterations":       e.config.MaxIterations,
			"total_usage":      totalUsage,
			"max_iter_reached": true,
			"agent_name":       e.config.AgentName,
		})
		return &ExecuteResult{
			Response:   lastTextResp,
			Iterations: e.config.MaxIterations,
			TotalUsage: totalUsage,
		}, nil
	}

	e.emit(types.EventToolUseLoopError, map[string]any{
		"error":      "max iterations exceeded with no text response",
		"limit":      e.config.MaxIterations,
		"agent_name": e.config.AgentName,
	})

	return nil, fmt.Errorf("tooluse.Executor.Execute: max iterations exceeded (%d) with no text response", e.config.MaxIterations)
}

// dispatchCall executes a single tool call via the configured dispatch function
// and converts the result. Dispatch errors are captured as error results rather
// than propagated, so the LLM can reason about the failure.
func (e *Executor) dispatchCall(ctx context.Context, call types.ToolCall) types.ToolCallResult {
	ctx = guard.WithAutonomousContext(ctx) // Mark as autonomous executor loop call.

	// Inject the agent's identity so ABAC grants the correct clearance level.
	ctx = context.WithValue(ctx, mcp.AuthContextKey(), types.AuthContext{
		PersonaID:      e.config.PersonaID,
		ClearanceLevel: e.config.ClearanceLevel,
		Authenticated:  true,
	})

	// Strip provider-injected prefixes (e.g. Gemini's "default_api:" prefix)
	// so dispatch matches the canonical MCP tool name.
	toolName := call.Name
	if idx := strings.Index(toolName, ":"); idx >= 0 {
		toolName = toolName[idx+1:]
	}

	inputSize := int64(len(call.Arguments))
	start := time.Now()

	result, err := e.config.Dispatch(ctx, toolName, call.Arguments)

	duration := time.Since(start)
	success := err == nil && (result == nil || !result.IsError)
	errMsg := ""

	if err != nil {
		errMsg = err.Error()

		// Unknown/hallucinated tool calls are not real errors — they're LLM
		// hallucinations. Return a helpful message so the LLM can self-correct
		// and try a valid tool name. Don't log as a warning since this is normal
		// model behavior, not a system failure.
		if strings.Contains(errMsg, "unknown tool") {
			e.logger.Debug("tool not found (LLM hallucination)",
				"tool", call.Name, "call_id", call.ID)

			if e.config.Recorder != nil {
				e.config.Recorder(toolName, duration, false, "hallucinated tool", inputSize, 0)
			}

			return types.ToolCallResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    fmt.Sprintf("The tool %q does not exist on this platform. Please review the available tools and try again with a valid tool name.", call.Name),
				IsError:    false,
			}
		}

		e.logger.Warn("tool dispatch error",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err,
		)

		// Record the failed tool call metric.
		if e.config.Recorder != nil {
			e.config.Recorder(toolName, duration, false, errMsg, inputSize, 0)
		}

		return types.ToolCallResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    fmt.Sprintf("error: %s", err.Error()),
			IsError:    true,
		}
	}

	// Convert *types.ToolResult to ToolCallResult.
	content := ""
	if result != nil && len(result.Content) > 0 {
		content = result.Content[0].Text
	}
	if result != nil && result.IsError {
		errMsg = content
		success = false
	}

	outputSize := int64(len(content))

	// Record the tool call metric to the telemetry session.
	if e.config.Recorder != nil {
		e.config.Recorder(call.Name, duration, success, errMsg, inputSize, outputSize)
	}

	return types.ToolCallResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		IsError:    result != nil && result.IsError,
	}
}

// isToolUseStop reports whether the stop reason indicates tool use.
func isToolUseStop(reason string) bool {
	switch reason {
	case "tool_use",   // Anthropic, Bedrock, Google (our normalized value)
		"tool_calls": // OpenAI, Ollama
		return true
	}
	return false
}

// toolCallNames extracts tool names from a slice of ToolCalls for logging.
func toolCallNames(calls []types.ToolCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

// emit publishes an event if an emitter is configured.
func (e *Executor) emit(eventType types.EventType, payload any) {
	if e.config.Emitter == nil {
		return
	}
	e.config.Emitter(eventType, payload)
}
