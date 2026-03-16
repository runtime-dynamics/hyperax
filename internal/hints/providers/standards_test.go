package providers_test

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/hints/providers"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockConfigRepo is a test double for repo.ConfigRepo.
type mockConfigRepo struct {
	values []types.ConfigValue
	err    error
}

func (m *mockConfigRepo) GetValue(_ context.Context, _ string, _ types.ConfigScope) (string, error) {
	return "", nil
}

func (m *mockConfigRepo) SetValue(_ context.Context, _, _ string, _ types.ConfigScope, _ string) error {
	return nil
}

func (m *mockConfigRepo) GetKeyMeta(_ context.Context, _ string) (*types.ConfigKeyMeta, error) {
	return nil, nil
}

func (m *mockConfigRepo) ListKeys(_ context.Context) ([]types.ConfigKeyMeta, error) {
	return nil, nil
}

func (m *mockConfigRepo) ListValues(_ context.Context, _ types.ConfigScope) ([]types.ConfigValue, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.values, nil
}

func (m *mockConfigRepo) GetHistory(_ context.Context, _ string, _ int) ([]types.ConfigChange, error) {
	return nil, nil
}

func (m *mockConfigRepo) UpsertKeyMeta(_ context.Context, _ *types.ConfigKeyMeta) error {
	return nil
}

func TestStandardsProviderName(t *testing.T) {
	p := providers.NewStandardsProvider(nil)
	if got := p.Name(); got != "standards" {
		t.Fatalf("expected name 'standards', got %q", got)
	}
}

func TestStandardsProviderNilConfig(t *testing.T) {
	p := providers.NewStandardsProvider(nil)
	res, err := p.GetHints(context.Background(), &hints.HintRequest{Query: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("expected nil result for nil config, got %v", res)
	}
}

func TestStandardsProviderReturnsMatchingStandards(t *testing.T) {
	mock := &mockConfigRepo{
		values: []types.ConfigValue{
			{Key: "standard.go.naming", Value: "Use camelCase for Go variables"},
			{Key: "standard.python.naming", Value: "Use snake_case for Python functions"},
			{Key: "unrelated.key", Value: "This should be ignored"},
			{Key: "standard.go.errors", Value: "Always handle errors in Go"},
		},
	}

	p := providers.NewStandardsProvider(mock)
	res, err := p.GetHints(context.Background(), &hints.HintRequest{
		WorkspaceID: "ws1",
		Query:       "naming conventions",
		Language:    "go",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should return only standard.* keys (3 entries), not the "unrelated.key".
	if len(res) != 3 {
		t.Fatalf("expected 3 hints, got %d: %v", len(res), res)
	}

	for _, h := range res {
		if h.Provider != "standards" {
			t.Fatalf("expected provider 'standards', got %q", h.Provider)
		}
		if h.Category != "standard" {
			t.Fatalf("expected category 'standard', got %q", h.Category)
		}
		if h.Relevance <= 0 || h.Relevance > 1.0 {
			t.Fatalf("relevance %f out of [0,1] range", h.Relevance)
		}
	}
}

func TestStandardsProviderLanguageBoost(t *testing.T) {
	mock := &mockConfigRepo{
		values: []types.ConfigValue{
			{Key: "standard.go.naming", Value: "Go naming rules"},
			{Key: "standard.python.naming", Value: "Python naming rules"},
		},
	}

	p := providers.NewStandardsProvider(mock)
	res, err := p.GetHints(context.Background(), &hints.HintRequest{
		Query:    "naming",
		Language: "go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res) != 2 {
		t.Fatalf("expected 2 hints, got %d", len(res))
	}

	// The Go standard should have higher relevance due to language match.
	var goRelevance, pyRelevance float64
	for _, h := range res {
		if h.Source == "standard.go.naming" {
			goRelevance = h.Relevance
		}
		if h.Source == "standard.python.naming" {
			pyRelevance = h.Relevance
		}
	}
	if goRelevance <= pyRelevance {
		t.Fatalf("expected Go standard to score higher: go=%f, python=%f", goRelevance, pyRelevance)
	}
}

func TestStandardsProviderGlobalScope(t *testing.T) {
	mock := &mockConfigRepo{
		values: []types.ConfigValue{
			{Key: "standard.general", Value: "General standard"},
		},
	}

	p := providers.NewStandardsProvider(mock)

	// Empty workspace ID should query global scope.
	res, err := p.GetHints(context.Background(), &hints.HintRequest{
		Query: "general",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 hint from global scope, got %d", len(res))
	}
}

func TestStandardsProviderConfigError(t *testing.T) {
	mock := &mockConfigRepo{
		err: context.DeadlineExceeded,
	}

	p := providers.NewStandardsProvider(mock)
	res, err := p.GetHints(context.Background(), &hints.HintRequest{Query: "test"})
	if err != nil {
		t.Fatal("expected graceful degradation, got error:", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on config error, got %v", res)
	}
}
