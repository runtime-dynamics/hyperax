package context

import (
	"strings"
	"testing"
)

func TestCompact_WithinBudget_NoChange(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 100000 // very high budget
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	result := c.Compact(messages)

	if result.CompactedCount != 3 {
		t.Errorf("expected 3 messages (no compaction), got %d", result.CompactedCount)
	}
	if result.SummaryInserted {
		t.Error("should not insert summary when within budget")
	}
	if result.TokensEstimatedAfter != result.TokensEstimatedBefore {
		t.Errorf("tokens should be unchanged: before=%d, after=%d",
			result.TokensEstimatedBefore, result.TokensEstimatedAfter)
	}
}

func TestCompact_ExceedsBudget_Summarises(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 50 // very tight budget
	cfg.PreserveRecentTurns = 2
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "system", Content: "System prompt."},
		{Role: "user", Content: "What is the weather today in New York City?"},
		{Role: "assistant", Content: "The weather in NYC is sunny with 75°F."},
		{Role: "user", Content: "How about tomorrow?"},
		{Role: "assistant", Content: "Tomorrow will be partly cloudy, 68°F."},
		{Role: "user", Content: "And the day after?"},
		{Role: "assistant", Content: "Rainy, 55°F expected."},
		{Role: "user", Content: "Thanks, what about this weekend?"},
		{Role: "assistant", Content: "Saturday sunny 70°F, Sunday cloudy 65°F."},
	}

	result := c.Compact(messages)

	if result.OriginalCount != 9 {
		t.Errorf("original count = %d, want 9", result.OriginalCount)
	}
	if result.CompactedCount >= result.OriginalCount {
		t.Errorf("compacted count (%d) should be less than original (%d)",
			result.CompactedCount, result.OriginalCount)
	}
	if !result.SummaryInserted {
		t.Error("expected summary to be inserted")
	}

	// System message should still be first.
	if result.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want system", result.Messages[0].Role)
	}

	// Summary should be second.
	if result.Messages[1].Role != "assistant" {
		t.Errorf("second message role = %q, want assistant (summary)", result.Messages[1].Role)
	}
	if !strings.Contains(result.Messages[1].Content, "[Earlier conversation summary]") {
		t.Error("expected summary header in compacted message")
	}
}

func TestCompact_PreservesSystemMessage(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 30
	cfg.PreserveRecentTurns = 1
	cfg.SystemMessageProtected = true
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "system", Content: "Important system instructions."},
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Second answer"},
	}

	result := c.Compact(messages)

	if result.Messages[0].Role != "system" {
		t.Errorf("first message should be system, got %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "Important system instructions." {
		t.Error("system message content should be preserved exactly")
	}
}

func TestCompact_NoSystemMessage(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 30
	cfg.PreserveRecentTurns = 1
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "user", Content: "First question about code"},
		{Role: "assistant", Content: "First answer about code"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Second answer"},
	}

	result := c.Compact(messages)

	// No system message — compaction should still work.
	if result.OriginalCount != 4 {
		t.Errorf("original count = %d, want 4", result.OriginalCount)
	}
}

func TestCompact_ToolResults(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 40
	cfg.PreserveRecentTurns = 1
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "system", Content: "You are an agent."},
		{Role: "user", Content: "Find the main function"},
		{Role: "assistant", Content: "I'll search for it."},
		{Role: "tool", Content: "Found 3 symbols matching 'main': function main() at line 10"},
		{Role: "assistant", Content: "Found the main function at line 10."},
		{Role: "user", Content: "What does it do?"},
		{Role: "assistant", Content: "It initializes the application."},
	}

	result := c.Compact(messages)

	// Summary should mention tool calls.
	hasSummary := false
	for _, msg := range result.Messages {
		if strings.Contains(msg.Content, "[Earlier conversation summary]") {
			hasSummary = true
			if !strings.Contains(msg.Content, "Tool calls:") {
				t.Error("summary should mention tool calls")
			}
		}
	}
	if !hasSummary {
		t.Error("expected a summary message")
	}
}

func TestCompact_PreservesRecentTurns(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 30
	cfg.PreserveRecentTurns = 2
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "user", Content: "Old question 1"},
		{Role: "assistant", Content: "Old answer 1"},
		{Role: "user", Content: "Old question 2"},
		{Role: "assistant", Content: "Old answer 2"},
		{Role: "user", Content: "Recent question 1"},
		{Role: "assistant", Content: "Recent answer 1"},
		{Role: "user", Content: "Recent question 2"},
		{Role: "assistant", Content: "Recent answer 2"},
	}

	result := c.Compact(messages)

	// Last two user messages should be preserved.
	hasRecent1 := false
	hasRecent2 := false
	for _, msg := range result.Messages {
		if msg.Content == "Recent question 1" {
			hasRecent1 = true
		}
		if msg.Content == "Recent question 2" {
			hasRecent2 = true
		}
	}
	if !hasRecent1 {
		t.Error("recent turn 1 should be preserved")
	}
	if !hasRecent2 {
		t.Error("recent turn 2 should be preserved")
	}
}

func TestNeedsCompaction(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 50
	c := NewCompactor(cfg)

	short := []CompactMessage{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello"},
	}
	if c.NeedsCompaction(short) {
		t.Error("short conversation should not need compaction")
	}

	long := make([]CompactMessage, 100)
	for i := range long {
		long[i] = CompactMessage{Role: "user", Content: strings.Repeat("word ", 50)}
	}
	if !c.NeedsCompaction(long) {
		t.Error("long conversation should need compaction")
	}
}

func TestEstimateTokens(t *testing.T) {
	messages := []CompactMessage{
		{Role: "user", Content: strings.Repeat("a", 400)},
	}

	tokens := EstimateTokens(messages)
	// 400 chars / 4 = 100 content tokens + 4 overhead = 104
	if tokens != 104 {
		t.Errorf("expected 104 estimated tokens, got %d", tokens)
	}
}

func TestEstimateTokens_EmptyMessages(t *testing.T) {
	tokens := EstimateTokens(nil)
	if tokens != 0 {
		t.Errorf("expected 0 tokens for nil messages, got %d", tokens)
	}
}

func TestDefaultCompactorConfig(t *testing.T) {
	cfg := DefaultCompactorConfig()
	if cfg.MaxTokenBudget != 8000 {
		t.Errorf("default MaxTokenBudget = %d, want 8000", cfg.MaxTokenBudget)
	}
	if cfg.PreserveRecentTurns != 4 {
		t.Errorf("default PreserveRecentTurns = %d, want 4", cfg.PreserveRecentTurns)
	}
	if cfg.SummaryMaxTokens != 1000 {
		t.Errorf("default SummaryMaxTokens = %d, want 1000", cfg.SummaryMaxTokens)
	}
	if !cfg.SystemMessageProtected {
		t.Error("default SystemMessageProtected should be true")
	}
}

func TestCompact_TruncatesVerboseToolResults(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 50
	cfg.PreserveRecentTurns = 2
	c := NewCompactor(cfg)

	// Create a very verbose tool result in the preserved section.
	messages := []CompactMessage{
		{Role: "user", Content: "Show me the file"},
		{Role: "tool", Content: strings.Repeat("line of content\n", 200)}, // ~3000 chars
		{Role: "assistant", Content: "Here's the file content."},
		{Role: "user", Content: "What's in it?"},
		{Role: "assistant", Content: "It contains various functions."},
	}

	result := c.Compact(messages)

	// Check that tool results are truncated.
	for _, msg := range result.Messages {
		if msg.Role == "tool" && len(msg.Content) > 600 {
			t.Errorf("tool result should be truncated, got %d chars", len(msg.Content))
		}
	}
}

func TestFirstContentLine(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello world", "Hello world"},
		{"\n\nSecond line", "Second line"},
		{"", ""},
		{strings.Repeat("a", 150), strings.Repeat("a", 100) + "..."},
	}

	for _, tt := range tests {
		got := firstContentLine(tt.input)
		if got != tt.expected {
			t.Errorf("firstContentLine(%q) = %q, want %q", tt.input[:min(len(tt.input), 20)], got, tt.expected)
		}
	}
}

func TestCompact_SingleMessage(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.MaxTokenBudget = 100000
	c := NewCompactor(cfg)

	messages := []CompactMessage{
		{Role: "user", Content: "Hello"},
	}

	result := c.Compact(messages)
	if result.CompactedCount != 1 {
		t.Errorf("expected 1 message, got %d", result.CompactedCount)
	}
}

func TestCompact_EmptyConversation(t *testing.T) {
	c := NewCompactor(DefaultCompactorConfig())
	result := c.Compact(nil)

	if result.CompactedCount != 0 {
		t.Errorf("expected 0 messages, got %d", result.CompactedCount)
	}
	if result.SummaryInserted {
		t.Error("should not insert summary for empty conversation")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
