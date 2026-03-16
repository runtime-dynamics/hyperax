package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// Sandbox enforces that all file operations stay within registered workspace
// boundaries. Agents cannot access paths outside configured workspaces or the
// org workspace. If a path is requested outside all known workspaces, the caller
// should instruct the user to add the location as a workspace.
type Sandbox struct {
	workspaces repo.WorkspaceRepo
}

// NewSandbox creates a workspace sandbox validator.
func NewSandbox(workspaces repo.WorkspaceRepo) *Sandbox {
	return &Sandbox{workspaces: workspaces}
}

// ResolveAndValidate resolves a relative path within a named workspace and
// validates that the resulting absolute path stays within the workspace root.
// Returns the absolute path or an error if the path escapes the workspace.
func (s *Sandbox) ResolveAndValidate(ctx context.Context, workspaceName, relPath string) (string, error) {
	ws, err := s.workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", fmt.Errorf("workspace %q not found — use list_workspaces to see available workspaces", workspaceName)
	}

	return ValidatePath(ws.RootPath, relPath)
}

// ValidatePath checks that a relative path resolved against rootDir does not
// escape the root via traversal. Returns the clean absolute path.
func ValidatePath(rootDir, relPath string) (string, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("workspace.ValidatePath: %w", err)
	}

	// Reject absolute paths — all paths must be relative to workspace
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute paths not allowed — use a path relative to the workspace root")
	}

	resolved := filepath.Join(absRoot, filepath.Clean(relPath))
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("workspace.ValidatePath: %w", err)
	}

	// Ensure the resolved path is under the workspace root
	if !strings.HasPrefix(absResolved, absRoot+string(filepath.Separator)) && absResolved != absRoot {
		return "", fmt.Errorf("path %q escapes workspace boundary — access denied", relPath)
	}

	return absResolved, nil
}

// IsPathInAnyWorkspace checks whether an absolute path falls within any
// registered workspace. Returns the workspace info if found, nil otherwise.
func (s *Sandbox) IsPathInAnyWorkspace(ctx context.Context, absPath string) (*types.WorkspaceInfo, error) {
	workspaces, err := s.workspaces.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("workspace.Sandbox.IsPathInAnyWorkspace: %w", err)
	}

	cleanPath, err := filepath.Abs(absPath)
	if err != nil {
		return nil, fmt.Errorf("workspace.Sandbox.IsPathInAnyWorkspace: %w", err)
	}

	for _, ws := range workspaces {
		wsRoot, err := filepath.Abs(ws.RootPath)
		if err != nil {
			continue
		}
		if strings.HasPrefix(cleanPath, wsRoot+string(filepath.Separator)) || cleanPath == wsRoot {
			return ws, nil
		}
	}

	return nil, nil
}
