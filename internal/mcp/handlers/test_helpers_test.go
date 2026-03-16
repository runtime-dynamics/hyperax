package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

// mockWorkspaceRepo implements repo.WorkspaceRepo backed by an in-memory map.
type mockWorkspaceRepo struct {
	workspaces map[string]*types.WorkspaceInfo
}

func (m *mockWorkspaceRepo) WorkspaceExists(_ context.Context, name string) (bool, error) {
	_, ok := m.workspaces[name]
	return ok, nil
}

func (m *mockWorkspaceRepo) ListWorkspaces(_ context.Context) ([]*types.WorkspaceInfo, error) {
	var out []*types.WorkspaceInfo
	for _, ws := range m.workspaces {
		out = append(out, ws)
	}
	return out, nil
}

func (m *mockWorkspaceRepo) GetWorkspace(_ context.Context, name string) (*types.WorkspaceInfo, error) {
	ws, ok := m.workspaces[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) CreateWorkspace(_ context.Context, ws *types.WorkspaceInfo) error {
	m.workspaces[ws.Name] = ws
	return nil
}

func (m *mockWorkspaceRepo) DeleteWorkspace(_ context.Context, name string) error {
	if _, ok := m.workspaces[name]; !ok {
		return os.ErrNotExist
	}
	delete(m.workspaces, name)
	return nil
}

// callTool is a convenience wrapper that marshals args, invokes the handler
// function, and returns the ToolResult. It fails the test on marshal errors.
func callTool(t *testing.T, fn func(context.Context, json.RawMessage) (*types.ToolResult, error), ctx context.Context, args any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := fn(ctx, data)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return result
}

// resultText extracts the text content from the first ToolContent item.
func resultText(r *types.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// writeFile creates a file relative to root with the given content.
func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
