package memory

import (
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestSummariseMemories_Single(t *testing.T) {
	mems := []*mockMem{{content: "single fact"}}
	memories := toTypesMemories(mems)
	summary := summariseMemories(memories)
	if summary != "single fact" {
		t.Errorf("summary = %q, want %q", summary, "single fact")
	}
}

func TestSummariseMemories_Multiple(t *testing.T) {
	mems := []*mockMem{
		{content: "fact one"},
		{content: "fact two"},
		{content: "fact three"},
	}
	memories := toTypesMemories(mems)
	summary := summariseMemories(memories)

	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !contains(summary, "3 episodic memories") {
		t.Errorf("summary should mention count: %q", summary)
	}
	if !contains(summary, "fact one") || !contains(summary, "fact two") || !contains(summary, "fact three") {
		t.Errorf("summary should contain all facts: %q", summary)
	}
}

func TestSummariseMemories_Deduplicates(t *testing.T) {
	mems := []*mockMem{
		{content: "duplicate"},
		{content: "duplicate"},
		{content: "unique"},
	}
	memories := toTypesMemories(mems)
	summary := summariseMemories(memories)

	// Should only contain "duplicate" once.
	count := 0
	for _, line := range splitLines(summary) {
		if contains(line, "duplicate") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 'duplicate' once, found %d times in: %q", count, summary)
	}
}

func TestGroupByWorkspace(t *testing.T) {
	mems := toTypesMemories([]*mockMem{
		{content: "a", workspaceID: "ws-1"},
		{content: "b", workspaceID: "ws-1"},
		{content: "c", workspaceID: "ws-2"},
		{content: "d", workspaceID: ""},
	})

	groups := groupByWorkspace(mems)

	if len(groups["ws-1"]) != 2 {
		t.Errorf("ws-1 group = %d, want 2", len(groups["ws-1"]))
	}
	if len(groups["ws-2"]) != 1 {
		t.Errorf("ws-2 group = %d, want 1", len(groups["ws-2"]))
	}
	if len(groups["_global"]) != 1 {
		t.Errorf("_global group = %d, want 1", len(groups["_global"]))
	}
}

func TestAllSamePersona(t *testing.T) {
	tests := []struct {
		name      string
		personas  []string
		want      bool
	}{
		{"all same", []string{"p1", "p1", "p1"}, true},
		{"different", []string{"p1", "p2"}, false},
		{"empty", []string{"", ""}, false},
		{"single", []string{"p1"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := make([]*mockMem, len(tt.personas))
			for i, p := range tt.personas {
				mocks[i] = &mockMem{content: "x", personaID: p}
			}
			mems := toTypesMemories(mocks)
			if got := allSamePersona(mems); got != tt.want {
				t.Errorf("allSamePersona = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- helpers ---

type mockMem struct {
	content     string
	workspaceID string
	personaID   string
}

func toTypesMemories(mocks []*mockMem) []*types.Memory {
	var result []*types.Memory
	for _, m := range mocks {
		result = append(result, &types.Memory{
			Content:     m.content,
			WorkspaceID: m.workspaceID,
			PersonaID:   m.personaID,
		})
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
