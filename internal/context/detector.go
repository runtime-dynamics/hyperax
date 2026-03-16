package context

import (
	"fmt"
	"os"
	"path/filepath"
)

// ContextFileType identifies the kind of agent context file.
type ContextFileType string

const (
	// ContextClaude is the Anthropic Claude agent context file (CLAUDE.md).
	ContextClaude ContextFileType = "CLAUDE.md"

	// ContextGemini is the Google Gemini agent context directory (.gemini).
	ContextGemini ContextFileType = ".gemini"

	// ContextCodex is the OpenAI Codex agent context file (codex.md or AGENTS.md).
	ContextCodex ContextFileType = "AGENTS.md"

	// ContextCursor is the Cursor IDE rules file (.cursorrules).
	ContextCursor ContextFileType = ".cursorrules"

	// ContextCopilot is the GitHub Copilot instructions file (.github/copilot-instructions.md).
	ContextCopilot ContextFileType = ".github/copilot-instructions.md"
)

// allDetectable lists every context file type that DetectContextFiles will
// look for. The paths are relative to the workspace root.
var allDetectable = []ContextFileType{
	ContextClaude,
	ContextGemini,
	ContextCodex,
	ContextCursor,
	ContextCopilot,
}

// DetectContextFiles scans a workspace root directory for known agent context
// files. It returns a map keyed by ContextFileType whose values are the
// absolute paths to the detected files or directories.
//
// Parameters:
//   - rootPath: the absolute path to the workspace root directory.
//
// Returns:
//   - A map of detected context file types to their absolute paths. The map
//     is empty (not nil) if no context files are found.
//   - An error if rootPath does not exist or is not a directory.
func DetectContextFiles(rootPath string) (map[ContextFileType]string, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, fmt.Errorf("context.DetectContextFiles: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "detect", Path: rootPath, Err: os.ErrInvalid}
	}

	detected := make(map[ContextFileType]string)

	for _, ct := range allDetectable {
		candidate := filepath.Join(rootPath, string(ct))
		if _, statErr := os.Stat(candidate); statErr == nil {
			detected[ct] = candidate
		}
	}

	return detected, nil
}
