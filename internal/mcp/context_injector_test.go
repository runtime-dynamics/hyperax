package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestExtractQueryFromParams(t *testing.T) {
	tests := []struct {
		name   string
		params string
		want   string
	}{
		{
			name:   "query field",
			params: `{"query": "error handling"}`,
			want:   "error handling",
		},
		{
			name:   "content field",
			params: `{"content": "some content here"}`,
			want:   "some content here",
		},
		{
			name:   "name field",
			params: `{"name": "my_function"}`,
			want:   "my_function",
		},
		{
			name:   "empty params",
			params: `{}`,
			want:   "",
		},
		{
			name:   "null params",
			params: ``,
			want:   "",
		},
		{
			name:   "numeric field not extracted",
			params: `{"limit": 10}`,
			want:   "",
		},
		{
			name:   "query preferred over content",
			params: `{"query": "primary", "content": "secondary"}`,
			want:   "primary",
		},
		{
			name:   "path field",
			params: `{"path": "internal/commhub/hub.go"}`,
			want:   "internal/commhub/hub.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractQueryFromParams(json.RawMessage(tt.params))
			if got != tt.want {
				t.Errorf("extractQueryFromParams(%s) = %q, want %q", tt.params, got, tt.want)
			}
		})
	}
}

func TestNewMemoryContextInjector_NilRepo(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	injector := NewMemoryContextInjector(nil, logger)
	if injector != nil {
		t.Error("expected nil injector when memRepo is nil")
	}
}

func TestContextInjector_ExcludedTools(t *testing.T) {
	for tool := range toolsExcludedFromInjection {
		t.Run(tool, func(t *testing.T) {
			// Verify tool is in exclusion set.
			if !toolsExcludedFromInjection[tool] {
				t.Errorf("tool %q should be in exclusion set", tool)
			}
		})
	}
}

func TestContextInjector_AppendsMemoryContent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "m-1", Content: "relevant memory", Scope: types.MemoryScopeGlobal},
		},
	}

	injector := NewMemoryContextInjector(repo, logger)
	if injector == nil {
		t.Fatal("expected non-nil injector")
	}

	result := types.NewToolResult("original result")
	params := json.RawMessage(`{"query": "test query"}`)

	injector(context.Background(), "search_code", params, result)

	if len(result.Content) < 2 {
		t.Fatalf("expected at least 2 content items after injection, got %d", len(result.Content))
	}

	lastContent := result.Content[len(result.Content)-1]
	if !strings.Contains(lastContent.Text, "[Memory Context]") {
		t.Errorf("expected memory context marker in injected content, got %q", lastContent.Text)
	}
	if !strings.Contains(lastContent.Text, "m-1") {
		t.Errorf("expected memory ID 'm-1' in injected content, got %q", lastContent.Text)
	}
}

func TestContextInjector_NoQueryField_Skips(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "m-1", Content: "should not appear"},
		},
	}

	injector := NewMemoryContextInjector(repo, logger)
	result := types.NewToolResult("original result")
	params := json.RawMessage(`{"limit": 10}`) // No query-extractable field.

	injector(context.Background(), "list_cadences", params, result)

	if len(result.Content) != 1 {
		t.Errorf("expected 1 content item (no injection), got %d", len(result.Content))
	}
}

// mockMemoryRepo implements repo.MemoryRepo for testing the context injector.
type mockMemoryRepo struct {
	memories []*types.Memory
}

func (m *mockMemoryRepo) Store(ctx context.Context, mem *types.Memory) (string, error) {
	return mem.ID, nil
}

func (m *mockMemoryRepo) Get(ctx context.Context, id string) (*types.Memory, error) {
	for _, mem := range m.memories {
		if mem.ID == id {
			return mem, nil
		}
	}
	return nil, nil
}

func (m *mockMemoryRepo) Delete(ctx context.Context, id string) error { return nil }

func (m *mockMemoryRepo) Recall(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	if limit > len(m.memories) {
		limit = len(m.memories)
	}
	return m.memories[:limit], nil
}

func (m *mockMemoryRepo) TouchAccess(ctx context.Context, id string) error { return nil }

func (m *mockMemoryRepo) ListConsolidationCandidates(ctx context.Context, scope types.MemoryScope, olderThanDays, limit int) ([]*types.Memory, error) {
	return nil, nil
}

func (m *mockMemoryRepo) MarkConsolidated(ctx context.Context, ids []string, targetID string) error {
	return nil
}

func (m *mockMemoryRepo) MarkContested(ctx context.Context, id, contestedByID string) error {
	return nil
}

func (m *mockMemoryRepo) Count(ctx context.Context, scope types.MemoryScope, workspaceID string) (int, error) {
	return len(m.memories), nil
}

func (m *mockMemoryRepo) CountByType(ctx context.Context, scope types.MemoryScope, workspaceID string) (map[types.MemoryType]int, error) {
	return nil, nil
}

func (m *mockMemoryRepo) StoreAnnotation(ctx context.Context, ann *types.MemoryAnnotation) (string, error) {
	return "", nil
}

func (m *mockMemoryRepo) GetAnnotations(ctx context.Context, memoryID string) ([]*types.MemoryAnnotation, error) {
	return nil, nil
}
