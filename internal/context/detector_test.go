package context

import (
	"os"
	"path/filepath"
	"testing"
)

// writeDetectorFile creates a file or directory at the given path relative
// to root.
func writeDetectorFile(t *testing.T, root, relPath string, isDir bool) {
	t.Helper()
	full := filepath.Join(root, relPath)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if isDir {
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
	} else {
		if err := os.WriteFile(full, []byte("# context\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func TestDetectContextFiles_Empty(t *testing.T) {
	root := t.TempDir()

	detected, err := DetectContextFiles(root)
	if err != nil {
		t.Fatalf("DetectContextFiles: %v", err)
	}

	if len(detected) != 0 {
		t.Errorf("expected empty map for bare directory, got %d entries", len(detected))
	}
}

func TestDetectContextFiles_ClaudeMD(t *testing.T) {
	root := t.TempDir()
	writeDetectorFile(t, root, "CLAUDE.md", false)

	detected, err := DetectContextFiles(root)
	if err != nil {
		t.Fatalf("DetectContextFiles: %v", err)
	}

	path, ok := detected[ContextClaude]
	if !ok {
		t.Fatal("expected CLAUDE.md to be detected")
	}
	if path != filepath.Join(root, "CLAUDE.md") {
		t.Errorf("expected path %s, got %s", filepath.Join(root, "CLAUDE.md"), path)
	}
}

func TestDetectContextFiles_GeminiDir(t *testing.T) {
	root := t.TempDir()
	writeDetectorFile(t, root, ".gemini", true)

	detected, err := DetectContextFiles(root)
	if err != nil {
		t.Fatalf("DetectContextFiles: %v", err)
	}

	if _, ok := detected[ContextGemini]; !ok {
		t.Fatal("expected .gemini to be detected")
	}
}

func TestDetectContextFiles_Multiple(t *testing.T) {
	root := t.TempDir()
	writeDetectorFile(t, root, "CLAUDE.md", false)
	writeDetectorFile(t, root, ".gemini", true)
	writeDetectorFile(t, root, "AGENTS.md", false)
	writeDetectorFile(t, root, ".cursorrules", false)

	detected, err := DetectContextFiles(root)
	if err != nil {
		t.Fatalf("DetectContextFiles: %v", err)
	}

	expected := []ContextFileType{ContextClaude, ContextGemini, ContextCodex, ContextCursor}
	for _, ct := range expected {
		if _, ok := detected[ct]; !ok {
			t.Errorf("expected %s to be detected", ct)
		}
	}

	// Copilot should NOT be detected since we didn't create it.
	if _, ok := detected[ContextCopilot]; ok {
		t.Error("copilot should not be detected when .github/copilot-instructions.md does not exist")
	}
}

func TestDetectContextFiles_CopilotNested(t *testing.T) {
	root := t.TempDir()
	writeDetectorFile(t, root, ".github/copilot-instructions.md", false)

	detected, err := DetectContextFiles(root)
	if err != nil {
		t.Fatalf("DetectContextFiles: %v", err)
	}

	if _, ok := detected[ContextCopilot]; !ok {
		t.Fatal("expected .github/copilot-instructions.md to be detected")
	}
}

func TestDetectContextFiles_NonexistentRoot(t *testing.T) {
	_, err := DetectContextFiles("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for nonexistent root path")
	}
}

func TestDetectContextFiles_FileNotDir(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := DetectContextFiles(filePath)
	if err == nil {
		t.Fatal("expected error when rootPath is a file, not a directory")
	}
}

func TestContextFileType_Constants(t *testing.T) {
	// Verify the string values of each constant for stability.
	tests := []struct {
		ct   ContextFileType
		want string
	}{
		{ContextClaude, "CLAUDE.md"},
		{ContextGemini, ".gemini"},
		{ContextCodex, "AGENTS.md"},
		{ContextCursor, ".cursorrules"},
		{ContextCopilot, ".github/copilot-instructions.md"},
	}

	for _, tt := range tests {
		if string(tt.ct) != tt.want {
			t.Errorf("ContextFileType %q has value %q, want %q", tt.ct, string(tt.ct), tt.want)
		}
	}
}
