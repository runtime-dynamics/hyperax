package context

import (
	"strings"
	"testing"
)

func TestExtractXMLBlock(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		tag      string
		expected string
	}{
		{
			name:     "basic extraction",
			text:     "<summary>This is a summary.</summary>",
			tag:      "summary",
			expected: "This is a summary.",
		},
		{
			name:     "with whitespace",
			text:     "<summary>\n  Trimmed summary.\n</summary>",
			tag:      "summary",
			expected: "Trimmed summary.",
		},
		{
			name:     "missing tag",
			text:     "No tags here.",
			tag:      "summary",
			expected: "",
		},
		{
			name:     "unclosed tag",
			text:     "<summary>Open but no close",
			tag:      "summary",
			expected: "",
		},
		{
			name:     "nested content",
			text:     "<memory><description>desc</description><details>det</details></memory>",
			tag:      "description",
			expected: "desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractXMLBlock(tt.text, tt.tag)
			if got != tt.expected {
				t.Errorf("extractXMLBlock(%q, %q) = %q, want %q", tt.text, tt.tag, got, tt.expected)
			}
		})
	}
}

func TestParseMemoryBlock(t *testing.T) {
	t.Run("full memory block", func(t *testing.T) {
		text := `<summary>A summary</summary>
<memory>
    <description>Key decisions about architecture</description>
    <details>The team decided to use SQLite as the default storage backend.</details>
    <keywords>architecture, sqlite, storage</keywords>
</memory>`

		mem := parseMemoryBlock(text)
		if mem == nil {
			t.Fatal("expected memory to be parsed")
		}
		if mem.Description != "Key decisions about architecture" {
			t.Errorf("description = %q", mem.Description)
		}
		if !strings.Contains(mem.Details, "SQLite") {
			t.Error("details should mention SQLite")
		}
		if mem.Keywords != "architecture, sqlite, storage" {
			t.Errorf("keywords = %q", mem.Keywords)
		}
	})

	t.Run("no memory block", func(t *testing.T) {
		text := "<summary>Just a summary, no memory.</summary>"
		mem := parseMemoryBlock(text)
		if mem != nil {
			t.Error("expected nil memory when no memory block present")
		}
	})

	t.Run("empty description and details", func(t *testing.T) {
		text := "<memory><description></description><details></details></memory>"
		mem := parseMemoryBlock(text)
		if mem != nil {
			t.Error("expected nil memory when description and details are both empty")
		}
	})
}

func TestFindRecentTurnBoundary(t *testing.T) {
	conversation := []CompactMessage{
		{Role: "user", Content: "old1"},
		{Role: "assistant", Content: "old1-resp"},
		{Role: "user", Content: "old2"},
		{Role: "assistant", Content: "old2-resp"},
		{Role: "user", Content: "recent1"},
		{Role: "assistant", Content: "recent1-resp"},
		{Role: "user", Content: "recent2"},
		{Role: "assistant", Content: "recent2-resp"},
	}

	t.Run("preserve 2 turns", func(t *testing.T) {
		idx := findRecentTurnBoundary(conversation, 2)
		if idx != 4 {
			t.Errorf("expected boundary at 4, got %d", idx)
		}
		if conversation[idx].Content != "recent1" {
			t.Errorf("expected 'recent1' at boundary, got %q", conversation[idx].Content)
		}
	})

	t.Run("preserve 4 turns (all)", func(t *testing.T) {
		idx := findRecentTurnBoundary(conversation, 4)
		if idx != 0 {
			t.Errorf("expected boundary at 0 (preserve all), got %d", idx)
		}
	})

	t.Run("empty conversation", func(t *testing.T) {
		idx := findRecentTurnBoundary(nil, 2)
		if idx != 0 {
			t.Errorf("expected 0 for empty conversation, got %d", idx)
		}
	})
}

func TestBuildCompactionPrompt(t *testing.T) {
	olderTurns := []CompactMessage{
		{Role: "user", Content: "What is Go?"},
		{Role: "assistant", Content: "Go is a programming language."},
	}

	prompt := buildCompactionPrompt("You are an expert.", olderTurns)

	if !strings.Contains(prompt, "You are an expert.") {
		t.Error("should contain system prompt")
	}
	if !strings.Contains(prompt, "<summary>") {
		t.Error("should contain summary instruction tag")
	}
	if !strings.Contains(prompt, "<memory>") {
		t.Error("should contain memory instruction tag")
	}
	if !strings.Contains(prompt, "[user]: What is Go?") {
		t.Error("should contain user message")
	}
	if !strings.Contains(prompt, "[assistant]: Go is a programming language.") {
		t.Error("should contain assistant message")
	}
}

func TestLLMCompactor_NeedsCompaction(t *testing.T) {
	cfg := DefaultLLMCompactorConfig()
	c := NewLLMCompactor(cfg)

	// Short conversation should not need compaction.
	short := []CompactMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}
	if c.NeedsCompaction(short) {
		t.Error("short conversation should not need compaction")
	}

	// Massive conversation should need compaction.
	massive := make([]CompactMessage, 1000)
	for i := range massive {
		massive[i] = CompactMessage{Role: "user", Content: strings.Repeat("word ", 300)}
	}
	if !c.NeedsCompaction(massive) {
		t.Error("massive conversation should need compaction")
	}
}

func TestDefaultLLMCompactorConfig(t *testing.T) {
	cfg := DefaultLLMCompactorConfig()
	if cfg.TokenThreshold != 150000 {
		t.Errorf("default TokenThreshold = %d, want 150000", cfg.TokenThreshold)
	}
	if cfg.PreserveRecentTurns != 4 {
		t.Errorf("default PreserveRecentTurns = %d, want 4", cfg.PreserveRecentTurns)
	}
}
