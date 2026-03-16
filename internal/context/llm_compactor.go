package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/provider"
)

// LLMCompactorConfig configures the LLM-based conversation compactor.
type LLMCompactorConfig struct {
	// TokenThreshold is the token estimate above which compaction triggers.
	// Default: 150000 (~150k tokens).
	TokenThreshold int `yaml:"token_threshold"`
	// PreserveRecentTurns is the number of most recent user+assistant
	// pairs to keep verbatim after compaction. Default: 4.
	PreserveRecentTurns int `yaml:"preserve_recent_turns"`
}

// DefaultLLMCompactorConfig returns production defaults.
func DefaultLLMCompactorConfig() LLMCompactorConfig {
	return LLMCompactorConfig{
		TokenThreshold:      150000,
		PreserveRecentTurns: 4,
	}
}

// LLMCompactionRequest contains all inputs for an LLM-based compaction.
type LLMCompactionRequest struct {
	// Messages is the full conversation including the system prompt.
	Messages []CompactMessage
	// SystemPrompt is the structured system prompt (identity, role, etc.).
	// It is prepended to the compaction prompt so the LLM understands
	// the agent's context when summarising.
	SystemPrompt string
	// ProviderKind is the LLM provider type (e.g., "anthropic", "openai").
	ProviderKind string
	// BaseURL is the provider's API base URL.
	BaseURL string
	// APIKey is the provider's API key.
	APIKey string
	// Model is the model identifier.
	Model string
}

// LLMCompactionResult holds the output of LLM-based compaction.
type LLMCompactionResult struct {
	// Summary is the LLM-generated conversation summary for session context.
	Summary string
	// ExtractedMemory contains the memory block extracted by the LLM (if any).
	ExtractedMemory *ExtractedMemory
	// TokensBefore is the estimated token count before compaction.
	TokensBefore int
	// TokensAfter is the estimated token count after compaction.
	TokensAfter int
	// RecentMessages are the preserved recent turns to use going forward.
	RecentMessages []CompactMessage
}

// ExtractedMemory holds the structured memory extracted from compaction.
type ExtractedMemory struct {
	Description string
	Details     string
	Keywords    string
}

// LLMCompactor performs LLM-based conversation compaction at high token counts.
// Unlike the heuristic Compactor (8k budget), this calls the LLM to produce
// a semantically meaningful summary and extracts long-term memory.
type LLMCompactor struct {
	config LLMCompactorConfig
}

// NewLLMCompactor creates an LLM compactor with the given configuration.
func NewLLMCompactor(cfg LLMCompactorConfig) *LLMCompactor {
	if cfg.TokenThreshold <= 0 {
		cfg.TokenThreshold = 150000
	}
	if cfg.PreserveRecentTurns <= 0 {
		cfg.PreserveRecentTurns = 4
	}
	return &LLMCompactor{config: cfg}
}

// NeedsCompaction returns true if the conversation exceeds the token threshold.
func (c *LLMCompactor) NeedsCompaction(messages []CompactMessage) bool {
	return EstimateTokens(messages) > c.config.TokenThreshold
}

// CompactWithLLM performs LLM-based compaction on the conversation.
// It sends older turns to the LLM for summarisation and memory extraction,
// preserving the most recent turns verbatim.
func (c *LLMCompactor) CompactWithLLM(ctx context.Context, req LLMCompactionRequest) (*LLMCompactionResult, error) {
	tokensBefore := EstimateTokens(req.Messages)

	// If under threshold, return early with no changes.
	if tokensBefore <= c.config.TokenThreshold {
		return &LLMCompactionResult{
			TokensBefore:   tokensBefore,
			TokensAfter:    tokensBefore,
			RecentMessages: req.Messages,
		}, nil
	}

	// Separate system messages from conversation.
	var systemMsgs []CompactMessage
	var conversation []CompactMessage
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		} else {
			conversation = append(conversation, msg)
		}
	}

	// Find the split point to preserve recent turns.
	recentStart := findRecentTurnBoundary(conversation, c.config.PreserveRecentTurns)

	// If there are no older turns to summarise, return early.
	if recentStart == 0 {
		return &LLMCompactionResult{
			TokensBefore:   tokensBefore,
			TokensAfter:    tokensBefore,
			RecentMessages: req.Messages,
		}, nil
	}

	olderTurns := conversation[:recentStart]
	recentTurns := conversation[recentStart:]

	// Build the compaction prompt.
	compactionPrompt := buildCompactionPrompt(req.SystemPrompt, olderTurns)

	// Call the LLM for summarisation.
	llmMessages := []provider.ChatMessage{
		{Role: "system", Content: compactionPrompt},
		{Role: "user", Content: "Please summarise the conversation above and extract the most pertinent memory."},
	}

	resp, err := provider.ChatCompletion(ctx, &provider.CompletionRequest{
		Kind:     req.ProviderKind,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
		Model:    req.Model,
		Messages: llmMessages,
	})
	if err != nil {
		return nil, fmt.Errorf("context.LLMCompactor.CompactWithLLM: %w", err)
	}

	// Parse the LLM response for summary and memory blocks.
	summary := extractXMLBlock(resp.Content, "summary")
	memory := parseMemoryBlock(resp.Content)

	// If the LLM didn't produce a parseable summary, use the raw response.
	if summary == "" {
		summary = resp.Content
	}

	// Rebuild the message list: system + recent turns.
	var resultMessages []CompactMessage
	resultMessages = append(resultMessages, systemMsgs...)
	resultMessages = append(resultMessages, recentTurns...)

	tokensAfter := EstimateTokens(resultMessages)

	return &LLMCompactionResult{
		Summary:         summary,
		ExtractedMemory: memory,
		TokensBefore:    tokensBefore,
		TokensAfter:     tokensAfter,
		RecentMessages:  resultMessages,
	}, nil
}

// buildCompactionPrompt constructs the prompt sent to the LLM for summarisation.
func buildCompactionPrompt(systemPrompt string, olderTurns []CompactMessage) string {
	var sb strings.Builder

	if systemPrompt != "" {
		sb.WriteString(systemPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("You must now summarize the following chat and provide the most pertinent memory by responding with:\n")
	sb.WriteString("<summary>[your summary]</summary>\n")
	sb.WriteString("<memory>\n")
	sb.WriteString("    <description>A brief description of the key memory</description>\n")
	sb.WriteString("    <details>Detailed memory content, no more than 2000 words</details>\n")
	sb.WriteString("    <keywords>comma-separated keywords for retrieval</keywords>\n")
	sb.WriteString("</memory>\n\n")
	sb.WriteString("Here is the conversation to summarize:\n\n")

	for _, msg := range olderTurns {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}

	return sb.String()
}

// findRecentTurnBoundary walks backward through conversation messages to find
// where the preserved recent turns begin.
func findRecentTurnBoundary(conversation []CompactMessage, preserveCount int) int {
	if len(conversation) == 0 {
		return 0
	}

	turnsFound := 0
	for i := len(conversation) - 1; i >= 0; i-- {
		if conversation[i].Role == "user" {
			turnsFound++
			if turnsFound >= preserveCount {
				return i
			}
		}
	}

	return 0
}

// extractXMLBlock extracts the content between <tag>...</tag> from text.
func extractXMLBlock(text, tag string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"

	startIdx := strings.Index(text, openTag)
	if startIdx == -1 {
		return ""
	}
	startIdx += len(openTag)

	endIdx := strings.Index(text[startIdx:], closeTag)
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(text[startIdx : startIdx+endIdx])
}

// parseMemoryBlock extracts the structured memory from the LLM response.
func parseMemoryBlock(text string) *ExtractedMemory {
	memBlock := extractXMLBlock(text, "memory")
	if memBlock == "" {
		return nil
	}

	desc := extractXMLBlock(text, "description")
	details := extractXMLBlock(text, "details")
	keywords := extractXMLBlock(text, "keywords")

	if desc == "" && details == "" {
		return nil
	}

	return &ExtractedMemory{
		Description: desc,
		Details:     details,
		Keywords:    keywords,
	}
}
