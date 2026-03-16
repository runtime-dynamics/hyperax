package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// setupCodeTestWorkspace creates a temp workspace and returns the CodeHandler.
func setupCodeTestWorkspace(t *testing.T) (*CodeHandler, string, context.Context) {
	t.Helper()
	root := t.TempDir()

	store := &storage.Store{
		Workspaces: &mockWorkspaceRepo{
			workspaces: map[string]*types.WorkspaceInfo{
				"test": {ID: "ws-test", Name: "test", RootPath: root},
			},
		},
	}

	handler := NewCodeHandler(store, nil)
	return handler, root, context.Background()
}

// writeTestFile creates a file with the given number of lines.
func writeTestFile(t *testing.T, dir, name string, lineCount int) string {
	t.Helper()
	var sb strings.Builder
	for i := 1; i <= lineCount; i++ {
		fmt.Fprintf(&sb, "line %d content\n", i)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

func TestGetFileContent_DefaultLimit500(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "big.go", 800)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "big.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	// Should show 500 lines (default limit).
	if !strings.Contains(text, "lines 1-500 of 800") {
		t.Errorf("expected header showing lines 1-500 of 800, got:\n%s", firstLine(text))
	}

	// Should contain truncation guidance.
	if !strings.Contains(text, "Showing 500 of 800 lines") {
		t.Error("expected truncation guidance note")
	}
	if !strings.Contains(text, "offset=501") {
		t.Error("expected next-page hint with offset=501")
	}
	if !strings.Contains(text, "limit=-1") {
		t.Error("expected full-file hint with limit=-1")
	}
	if !strings.Contains(text, "outline") {
		t.Error("expected code outline hint")
	}
}

func TestGetFileContent_SmallFileNoTruncation(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "small.go", 50)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "small.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	// Should show all 50 lines without truncation.
	if !strings.Contains(text, "lines 1-50 of 50") {
		t.Errorf("expected header showing all 50 lines, got:\n%s", firstLine(text))
	}

	// Should NOT contain truncation guidance.
	if strings.Contains(text, "Showing") {
		t.Error("small file should not show truncation guidance")
	}
}

func TestGetFileContent_UnlimitedWithMinusOne(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "full.go", 800)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "full.go",
		"limit":          -1,
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	// Should show all 800 lines.
	if !strings.Contains(text, "lines 1-800 of 800") {
		t.Errorf("expected header showing all 800 lines, got:\n%s", firstLine(text))
	}

	// Should NOT contain truncation guidance.
	if strings.Contains(text, "Showing") {
		t.Error("unlimited read should not show truncation guidance")
	}
}

func TestGetFileContent_OffsetPaging(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "paged.go", 600)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "paged.go",
		"offset":         501,
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	// Should show lines 501-600 (100 remaining, under 500 limit).
	if !strings.Contains(text, "lines 501-600 of 600") {
		t.Errorf("expected header showing lines 501-600, got:\n%s", firstLine(text))
	}

	// No truncation since remaining is under limit.
	if strings.Contains(text, "Showing") {
		t.Error("should not truncate when remaining lines < limit")
	}
}

func TestGetFileContent_CustomLimit(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "custom.go", 100)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "custom.go",
		"limit":          20,
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	if !strings.Contains(text, "lines 1-20 of 100") {
		t.Errorf("expected header showing lines 1-20 of 100, got:\n%s", firstLine(text))
	}

	if !strings.Contains(text, "Showing 20 of 100 lines (80 remaining)") {
		t.Error("expected truncation guidance with 80 remaining")
	}
}

func TestGetFileContent_ExactlyAtLimit(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "exact.go", 500)

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "exact.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	// Exactly 500 lines — should not truncate.
	if strings.Contains(text, "Showing") {
		t.Error("file exactly at limit should not show truncation")
	}
}

func TestGetFileContent_MemoryFirstResolution(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "main.go", 10)

	handler.SetMemoryResolver(func(_ context.Context, wsID, filePath string) ([]string, error) {
		return []string{
			"This file contains the application entry point",
			"Known issue: error handling in line 5 is incomplete",
		}, nil
	})

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "main.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)

	if !strings.Contains(text, "--- Memory Context ---") {
		t.Error("expected memory context header")
	}
	if !strings.Contains(text, "application entry point") {
		t.Error("expected first memory note")
	}
	if !strings.Contains(text, "error handling in line 5") {
		t.Error("expected second memory note")
	}
	if !strings.Contains(text, "----------------------") {
		t.Error("expected memory context footer")
	}
}

func TestGetFileContent_MemoryContextOnlyOnFirstPage(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "paged.go", 100)

	called := false
	handler.SetMemoryResolver(func(_ context.Context, wsID, filePath string) ([]string, error) {
		called = true
		return []string{"Some note"}, nil
	})

	// Request starting from offset 50 — should NOT include memory context.
	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "paged.go",
		"offset":         50,
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if called {
		t.Error("memory resolver should not be called for non-first page")
	}

	text := codeResultText(result)
	if strings.Contains(text, "Memory Context") {
		t.Error("should not include memory context on non-first page")
	}
}

func TestGetFileContent_NoMemoryResolver(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "simple.go", 10)

	// No memory resolver set.
	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "simple.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)
	if strings.Contains(text, "Memory Context") {
		t.Error("should not include memory context when resolver not set")
	}
}

func TestGetFileContent_EmptyMemoryResults(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "clean.go", 10)

	handler.SetMemoryResolver(func(_ context.Context, wsID, filePath string) ([]string, error) {
		return nil, nil // no memories
	})

	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "clean.go",
	})

	result, err := handler.getFileContent(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)
	if strings.Contains(text, "Memory Context") {
		t.Error("should not include memory context header when no memories found")
	}
}

// ---------- replace_lines tests ----------

func TestReplaceLines_Basic(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "replace.go", 10)

	// Replace lines 4-6 with two new lines.
	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "replace.go",
		"start_line":     4,
		"end_line":       6,
		"new_content":    "replaced line A\nreplaced line B",
	})

	result, err := handler.replaceLines(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)
	if !strings.Contains(text, "Replaced lines 4-6") {
		t.Errorf("expected confirmation of replacement, got:\n%s", firstLine(text))
	}

	// Verify file contents on disk.
	content, err := os.ReadFile(filepath.Join(root, "replace.go"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")

	// Original: 10 lines, removed 3 (4-6), added 2 => 9 lines.
	if len(lines) != 9 {
		t.Fatalf("expected 9 lines after replacement, got %d", len(lines))
	}
	if lines[3] != "replaced line A" {
		t.Errorf("line 4 should be replaced, got: %q", lines[3])
	}
	if lines[4] != "replaced line B" {
		t.Errorf("line 5 should be replaced, got: %q", lines[4])
	}
	// Line after replacement should be original line 7.
	if lines[5] != "line 7 content" {
		t.Errorf("line 6 should be original line 7, got: %q", lines[5])
	}
}

func TestReplaceLines_LargeFile(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "large.go", 5000)

	// Replace lines 2500-2502 in a large file.
	params, _ := json.Marshal(map[string]any{
		"workspace_name": "test",
		"path":           "large.go",
		"start_line":     2500,
		"end_line":       2502,
		"new_content":    "big file replacement",
	})

	result, err := handler.replaceLines(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := codeResultText(result)
	if !strings.Contains(text, "Replaced lines 2500-2502") {
		t.Errorf("expected confirmation, got:\n%s", firstLine(text))
	}

	// Verify line count: 5000 - 3 removed + 1 added = 4998.
	content, err := os.ReadFile(filepath.Join(root, "large.go"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != 4998 {
		t.Fatalf("expected 4998 lines, got %d", len(lines))
	}
	if lines[2499] != "big file replacement" {
		t.Errorf("replacement line mismatch: %q", lines[2499])
	}
}

func TestReplaceLines_InvalidRange(t *testing.T) {
	handler, root, ctx := setupCodeTestWorkspace(t)
	writeTestFile(t, root, "invalid.go", 10)

	tests := []struct {
		name      string
		startLine int
		endLine   int
		wantErr   string
	}{
		{"zero start", 0, 5, "invalid line range: 0-5"},
		{"end before start", 5, 3, "invalid line range: 5-3"},
		{"negative start", -1, 5, "invalid line range: -1-5"},
		{"start beyond file", 20, 25, "start_line 20 exceeds file length 10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{
				"workspace_name": "test",
				"path":           "invalid.go",
				"start_line":     tt.startLine,
				"end_line":       tt.endLine,
				"new_content":    "x",
			})

			result, err := handler.replaceLines(ctx, params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			text := codeResultText(result)
			if !strings.Contains(text, tt.wantErr) {
				t.Errorf("expected error containing %q, got: %s", tt.wantErr, text)
			}
		})
	}
}

// codeResultText extracts the text content from a ToolResult.
func codeResultText(r *types.ToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// firstLine returns the first line of text for error messages.
func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}
