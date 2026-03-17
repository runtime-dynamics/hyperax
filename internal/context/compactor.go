package context

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// CompactMessage represents a conversation message for compaction purposes.
// This mirrors provider.ChatMessage but avoids an import dependency on the
// provider package.
type CompactMessage struct {
	Role    string `json:"role"`    // "system", "user", "assistant", "tool"
	Content string `json:"content"`
}

// CompactionResult holds the output of a conversation compaction operation.
type CompactionResult struct {
	// Messages is the compacted conversation history.
	Messages []CompactMessage `json:"messages"`
	// OriginalCount is the number of messages before compaction.
	OriginalCount int `json:"original_count"`
	// CompactedCount is the number of messages after compaction.
	CompactedCount int `json:"compacted_count"`
	// TokensEstimatedBefore is the estimated token count before compaction.
	TokensEstimatedBefore int `json:"tokens_estimated_before"`
	// TokensEstimatedAfter is the estimated token count after compaction.
	TokensEstimatedAfter int `json:"tokens_estimated_after"`
	// SummaryInserted indicates whether an older-context summary was created.
	SummaryInserted bool `json:"summary_inserted"`
}

// CompactorConfig configures the conversation compactor.
type CompactorConfig struct {
	// MaxTokenBudget is the target maximum token count for the compacted
	// conversation. Default: 8000.
	MaxTokenBudget int `yaml:"max_token_budget"`
	// PreserveRecentTurns is the number of most recent turns (user+assistant
	// pairs) to always preserve verbatim. Default: 4.
	PreserveRecentTurns int `yaml:"preserve_recent_turns"`
	// SummaryMaxTokens is the maximum token budget for the compacted summary
	// of older turns. Default: 1000.
	SummaryMaxTokens int `yaml:"summary_max_tokens"`
	// SystemMessageProtected prevents the system message from being compacted.
	// Default: true.
	SystemMessageProtected bool `yaml:"system_message_protected"`
}

// DefaultCompactorConfig returns production defaults for the compactor.
func DefaultCompactorConfig() CompactorConfig {
	return CompactorConfig{
		MaxTokenBudget:         8000,
		PreserveRecentTurns:    4,
		SummaryMaxTokens:       1000,
		SystemMessageProtected: true,
	}
}

// Compactor implements automated conversation compaction. It preserves
// recent turns verbatim and summarises older turns into a condensed
// context block. The system message is optionally protected from
// compaction.
//
// The compaction strategy is:
//  1. Always preserve the system message (if protected).
//  2. Always preserve the N most recent turns.
//  3. Summarise older turns into a single "assistant" message with key
//     points extracted.
//  4. Tool call results are condensed to their tool names and success/error status.
type Compactor struct {
	config CompactorConfig
}

// NewCompactor creates a Compactor with the given configuration.
func NewCompactor(cfg CompactorConfig) *Compactor {
	if cfg.MaxTokenBudget <= 0 {
		cfg.MaxTokenBudget = 8000
	}
	if cfg.PreserveRecentTurns <= 0 {
		cfg.PreserveRecentTurns = 4
	}
	if cfg.SummaryMaxTokens <= 0 {
		cfg.SummaryMaxTokens = 1000
	}
	return &Compactor{config: cfg}
}

// Compact performs conversation compaction on the given message history.
// If the conversation is already within the token budget, it is returned
// unchanged. Otherwise, older turns are summarised and recent turns are
// preserved verbatim.
func (c *Compactor) Compact(messages []CompactMessage) CompactionResult {
	totalTokens := EstimateTokens(messages)

	result := CompactionResult{
		OriginalCount:         len(messages),
		TokensEstimatedBefore: totalTokens,
	}

	// If within budget, return unchanged.
	if totalTokens <= c.config.MaxTokenBudget {
		result.Messages = make([]CompactMessage, len(messages))
		copy(result.Messages, messages)
		result.CompactedCount = len(messages)
		result.TokensEstimatedAfter = totalTokens
		return result
	}

	// Separate system message if protected.
	var systemMsg *CompactMessage
	conversation := messages
	if c.config.SystemMessageProtected && len(messages) > 0 && messages[0].Role == "system" {
		sys := messages[0]
		systemMsg = &sys
		conversation = messages[1:]
	}

	// Determine the split point: preserve recent turns.
	recentStart := c.findRecentTurnStart(conversation)

	// Build the compacted message list.
	var compacted []CompactMessage

	if systemMsg != nil {
		compacted = append(compacted, *systemMsg)
	}

	// Summarise older turns if there are any.
	if recentStart > 0 {
		olderTurns := conversation[:recentStart]
		summary := c.summariseOlderTurns(olderTurns)
		compacted = append(compacted, CompactMessage{
			Role:    "assistant",
			Content: summary,
		})
		result.SummaryInserted = true
	}

	// Append recent turns verbatim.
	compacted = append(compacted, conversation[recentStart:]...)

	// If still over budget after basic compaction, truncate tool results
	// in the preserved section.
	afterTokens := EstimateTokens(compacted)
	if afterTokens > c.config.MaxTokenBudget {
		compacted = c.truncateToolResults(compacted)
		afterTokens = EstimateTokens(compacted)
	}

	result.Messages = compacted
	result.CompactedCount = len(compacted)
	result.TokensEstimatedAfter = afterTokens

	return result
}

// NeedsCompaction returns true if the conversation exceeds the token budget.
func (c *Compactor) NeedsCompaction(messages []CompactMessage) bool {
	return EstimateTokens(messages) > c.config.MaxTokenBudget
}

// findRecentTurnStart finds the index in the conversation where the
// recent preserved turns begin. A "turn" is a user+assistant pair.
func (c *Compactor) findRecentTurnStart(conversation []CompactMessage) int {
	if len(conversation) == 0 {
		return 0
	}

	turnsFound := 0
	// Walk backwards counting user messages as turn boundaries.
	for i := len(conversation) - 1; i >= 0; i-- {
		if conversation[i].Role == "user" {
			turnsFound++
			if turnsFound >= c.config.PreserveRecentTurns {
				return i
			}
		}
	}

	// Not enough turns to reach the preservation threshold — preserve all.
	return 0
}

// summariseOlderTurns creates a condensed summary of the older conversation
// turns. It extracts key points, decisions, and tool call outcomes.
func (c *Compactor) summariseOlderTurns(turns []CompactMessage) string {
	var sb strings.Builder
	sb.WriteString("[Earlier conversation summary]\n")

	var userQueries []string
	var assistantActions []string
	var toolResults []string

	for _, msg := range turns {
		switch msg.Role {
		case "user":
			// Extract the first line as a key point.
			line := firstContentLine(msg.Content)
			if line != "" {
				userQueries = append(userQueries, line)
			}

		case "assistant":
			line := firstContentLine(msg.Content)
			if line != "" {
				assistantActions = append(assistantActions, line)
			}

		case "tool":
			// Condense tool results to a brief status.
			condensed := condenseToolResult(msg.Content)
			if condensed != "" {
				toolResults = append(toolResults, condensed)
			}
		}
	}

	if len(userQueries) > 0 {
		sb.WriteString("User discussed: ")
		// Cap at 5 topics for brevity.
		limit := len(userQueries)
		if limit > 5 {
			limit = 5
		}
		sb.WriteString(strings.Join(userQueries[:limit], "; "))
		if len(userQueries) > 5 {
			fmt.Fprintf(&sb, " (and %d more topics)", len(userQueries)-5)
		}
		sb.WriteString("\n")
	}

	if len(assistantActions) > 0 {
		sb.WriteString("Actions taken: ")
		limit := len(assistantActions)
		if limit > 5 {
			limit = 5
		}
		sb.WriteString(strings.Join(assistantActions[:limit], "; "))
		if len(assistantActions) > 5 {
			fmt.Fprintf(&sb, " (and %d more)", len(assistantActions)-5)
		}
		sb.WriteString("\n")
	}

	if len(toolResults) > 0 {
		sb.WriteString("Tool calls: ")
		limit := len(toolResults)
		if limit > 10 {
			limit = 10
		}
		sb.WriteString(strings.Join(toolResults[:limit], ", "))
		if len(toolResults) > 10 {
			fmt.Fprintf(&sb, " (+%d more)", len(toolResults)-10)
		}
		sb.WriteString("\n")
	}

	summary := sb.String()

	// Enforce summary token budget.
	maxChars := c.config.SummaryMaxTokens * 4 // ~4 chars per token
	if utf8.RuneCountInString(summary) > maxChars {
		runes := []rune(summary)
		summary = string(runes[:maxChars]) + "..."
	}

	return summary
}

// truncateToolResults condenses verbose tool results in the preserved
// section to save tokens.
func (c *Compactor) truncateToolResults(messages []CompactMessage) []CompactMessage {
	const maxToolResultChars = 500

	result := make([]CompactMessage, len(messages))
	for i, msg := range messages {
		if msg.Role == "tool" && utf8.RuneCountInString(msg.Content) > maxToolResultChars {
			runes := []rune(msg.Content)
			result[i] = CompactMessage{
				Role:    msg.Role,
				Content: string(runes[:maxToolResultChars]) + "\n... [truncated]",
			}
		} else {
			result[i] = msg
		}
	}
	return result
}

// EstimateTokens provides a rough token count estimate using the ~4 chars
// per token heuristic. This is intentionally approximate — exact tokenisation
// depends on the specific model and tokeniser.
func EstimateTokens(messages []CompactMessage) int {
	total := 0
	for _, msg := range messages {
		// Each message has ~4 tokens overhead for role + delimiters.
		total += 4
		total += utf8.RuneCountInString(msg.Content) / 4
	}
	return total
}

// firstContentLine returns the first non-empty line of content, trimmed
// and capped at 100 characters.
func firstContentLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if utf8.RuneCountInString(trimmed) > 100 {
			runes := []rune(trimmed)
			return string(runes[:100]) + "..."
		}
		return trimmed
	}
	return ""
}

// condenseToolResult extracts a brief status from a tool result string.
// Returns something like "search_code: 5 results" or "get_file_content: ok".
func condenseToolResult(content string) string {
	if content == "" {
		return ""
	}
	line := firstContentLine(content)
	if utf8.RuneCountInString(line) > 60 {
		runes := []rune(line)
		return string(runes[:60]) + "..."
	}
	return line
}
