package tooluse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
	"google.golang.org/genai"
)

// gTrace appends to the shared Google trace log.
func gTrace(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [adapter] %s\n", time.Now().Format("15:04:05.000"), msg)
	f, err := os.OpenFile(filepath.Join("data", "google_trace.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// GoogleAdapter formats tools for the Google Gemini generateContent API
// using types from the official go-genai SDK.
//
// Wire formats:
//
//	Tools:   {functionDeclarations: [{name, description, parameters}]}
//	Calls:   candidates[0].content.parts → {functionCall: {name, args}}
//	Results: [{functionResponse: {name, response: {content}}}]
type GoogleAdapter struct{}

// -- FormatTools --------------------------------------------------------------

// FormatTools converts tool definitions to the Gemini functionDeclarations
// format using SDK types. The parameters field is passed through as
// ParametersJsonSchema (raw JSON Schema) since our internal ToolDefinition
// stores input schemas as json.RawMessage.
func (g *GoogleAdapter) FormatTools(tools []types.ToolDefinition) (json.RawMessage, error) {
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, t := range tools {
		// Unmarshal InputSchema into any so ParametersJsonSchema receives
		// a proper Go value (map/slice) rather than a json.RawMessage string.
		var params any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("google: unmarshal parameters for %s: %w", t.Name, err)
			}
		}
		decls[i] = &genai.FunctionDeclaration{
			Name:                 t.Name,
			Description:          t.Description,
			ParametersJsonSchema: params,
		}
	}
	// Wrap in a genai.Tool and marshal. The result is a single object:
	// {functionDeclarations: [...]}
	tool := &genai.Tool{FunctionDeclarations: decls}
	return json.Marshal(tool)
}

// -- ParseToolCalls -----------------------------------------------------------

// ParseToolCalls extracts functionCall parts from a Gemini response using
// the SDK's GenerateContentResponse type. If the SDK's FunctionCall.ID is
// populated (newer models), it is used directly; otherwise a synthetic ID
// is generated from the part index and function name.
func (g *GoogleAdapter) ParseToolCalls(response json.RawMessage) ([]types.ToolCall, error) {
	var resp genai.GenerateContentResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("google: parse response: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		gTrace("ParseToolCalls: no candidates or content")
		return nil, nil
	}

	var calls []types.ToolCall
	for i, part := range resp.Candidates[0].Content.Parts {
		if part.FunctionCall == nil {
			continue
		}

		// Use the SDK-provided ID if available, otherwise generate a synthetic one.
		callID := part.FunctionCall.ID
		if callID == "" {
			callID = fmt.Sprintf("gemini_call_%d_%s", i, part.FunctionCall.Name)
		}

		gTrace("ParseToolCalls: part[%d] name=%s id=%s hasSig=%v sigLen=%d",
			i, part.FunctionCall.Name, callID, len(part.ThoughtSignature) > 0, len(part.ThoughtSignature))

		// Marshal Args (map[string]any) back to json.RawMessage for the
		// internal ToolCall type.
		args, err := json.Marshal(part.FunctionCall.Args)
		if err != nil {
			return nil, fmt.Errorf("google: marshal args for %s: %w", part.FunctionCall.Name, err)
		}

		calls = append(calls, types.ToolCall{
			ID:        callID,
			Name:      part.FunctionCall.Name,
			Arguments: args,
		})
	}
	gTrace("ParseToolCalls: found %d calls", len(calls))
	return calls, nil
}

// -- FormatToolResults --------------------------------------------------------

// FormatToolResults converts tool results to Gemini functionResponse parts
// using SDK types. The output is a JSON array of Part objects with
// FunctionResponse set, which completeGoogle() will unmarshal into
// []*genai.Part for the next Content turn.
func (g *GoogleAdapter) FormatToolResults(results []types.ToolCallResult) (json.RawMessage, error) {
	gTrace("FormatToolResults: %d results", len(results))
	parts := make([]*genai.Part, len(results))
	for i, r := range results {
		// Use the function name from ToolCallResult. Fall back to extracting
		// from synthetic IDs for backward compatibility.
		name := r.Name
		if name == "" {
			name = extractGeminiName(r.ToolCallID)
		}
		gTrace("  result[%d] callID=%s name=%s resolvedName=%s contentLen=%d",
			i, r.ToolCallID, r.Name, name, len(r.Content))

		// Build the response map. The SDK expects map[string]any for the
		// Response field. We use "content" for the output and "error" for
		// error details, matching the Gemini API convention.
		respMap := map[string]any{
			"content": r.Content,
		}
		if r.IsError {
			respMap["error"] = r.Content
		}

		parts[i] = &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				Name:     name,
				Response: respMap,
				ID:       extractGeminiID(r.ToolCallID),
			},
		}
	}
	return json.Marshal(parts)
}

// extractGeminiName parses the function name from a Gemini tool call ID.
// For synthetic IDs (format: "gemini_call_{idx}_{name}"), extracts the name.
// For SDK-provided IDs, the name may not be embedded — falls back to the
// full ID which the caller should have set from the original FunctionCall.Name.
func extractGeminiName(callID string) string {
	// "gemini_call_0_list_tasks" → "list_tasks"
	const prefix = "gemini_call_"
	if len(callID) <= len(prefix) {
		return callID
	}
	rest := callID[len(prefix):]
	// Skip the index and underscore: "0_list_tasks" → "list_tasks"
	for i, ch := range rest {
		if ch == '_' {
			return rest[i+1:]
		}
	}
	return callID
}

// extractGeminiID returns the FunctionCall ID for correlation. For synthetic
// IDs (gemini_call_*), returns empty string since those don't have real IDs.
// For SDK-provided IDs, returns the ID as-is.
func extractGeminiID(callID string) string {
	const prefix = "gemini_call_"
	if len(callID) > len(prefix) && callID[:len(prefix)] == prefix {
		return "" // Synthetic ID, no real correlation ID.
	}
	return callID
}

// -- FormatTurnMessages -------------------------------------------------------

// FormatTurnMessages constructs the Google Gemini conversation turn for tool use.
// Gemini uses a "model" role for assistant responses and a "user" role for
// function response results.
//
// IMPORTANT: We extract parts as json.RawMessage to preserve all fields verbatim,
// including thoughtSignature which Gemini requires on round-trip function calls.
// Parsing through fully typed structs would strip unknown fields and cause 400 errors.
func (g *GoogleAdapter) FormatTurnMessages(rawResponse json.RawMessage, formattedResults json.RawMessage) ([]provider.ChatMessage, error) {
	gTrace("FormatTurnMessages: rawResponse=%d bytes, formattedResults=%d bytes",
		len(rawResponse), len(formattedResults))

	// Extract the raw content parts, preserving all fields (thoughtSignature, etc.).
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts json.RawMessage `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(rawResponse, &resp); err != nil {
		return nil, fmt.Errorf("google: parse response for turn messages: %w", err)
	}

	var assistantRaw json.RawMessage
	if len(resp.Candidates) > 0 {
		assistantRaw = resp.Candidates[0].Content.Parts
	}

	// Verify thoughtSignature survives in the raw parts.
	var rawParts []json.RawMessage
	if assistantRaw != nil {
		_ = json.Unmarshal(assistantRaw, &rawParts)
		for i, rp := range rawParts {
			var partCheck struct {
				FunctionCall     *json.RawMessage `json:"functionCall,omitempty"`
				ThoughtSignature json.RawMessage  `json:"thoughtSignature,omitempty"`
				Thought          bool             `json:"thought,omitempty"`
			}
			_ = json.Unmarshal(rp, &partCheck)
			hasFC := partCheck.FunctionCall != nil
			hasSig := len(partCheck.ThoughtSignature) > 0
			if hasFC || hasSig {
				gTrace("  FormatTurnMessages assistant part[%d]: hasFC=%v hasSig=%v thought=%v",
					i, hasFC, hasSig, partCheck.Thought)
			}
		}
	}

	gTrace("  FormatTurnMessages: assistantRaw=%d bytes", len(assistantRaw))

	return []provider.ChatMessage{
		{Role: "model", RawContent: assistantRaw},
		// Gemini API only accepts "user" or "model" roles — function responses
		// are sent as "user" with functionResponse parts (not a "function" role).
		{Role: "user", RawContent: formattedResults},
	}, nil
}
