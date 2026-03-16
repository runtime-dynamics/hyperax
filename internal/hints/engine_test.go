package hints_test

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/hyperax/hyperax/internal/hints"
)

// mockProvider is a test double for HintProvider.
type mockProvider struct {
	name   string
	hints  []hints.Hint
	err    error
	called atomic.Int32
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) GetHints(_ context.Context, _ *hints.HintRequest) ([]hints.Hint, error) {
	m.called.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return m.hints, nil
}

func TestNewEngine(t *testing.T) {
	e := hints.NewEngine(slogDefault())
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if got := e.ListProviders(); len(got) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(got))
	}
}

func TestRegisterAndListProviders(t *testing.T) {
	e := hints.NewEngine(slogDefault())
	e.RegisterProvider(&mockProvider{name: "beta"})
	e.RegisterProvider(&mockProvider{name: "alpha"})

	names := e.ListProviders()
	if len(names) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(names))
	}
	// ListProviders returns sorted names.
	if names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", names)
	}
}

func TestRegisterProviderReplace(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	p1 := &mockProvider{name: "dup", hints: []hints.Hint{{Content: "old"}}}
	p2 := &mockProvider{name: "dup", hints: []hints.Hint{{Content: "new"}}}

	e.RegisterProvider(p1)
	e.RegisterProvider(p2) // should replace

	names := e.ListProviders()
	if len(names) != 1 {
		t.Fatalf("expected 1 provider after replace, got %d", len(names))
	}

	res, err := e.GetHints(context.Background(), &hints.HintRequest{Query: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Content != "new" {
		t.Fatalf("expected replaced provider's hint, got %v", res)
	}
}

func TestGetHintsEmpty(t *testing.T) {
	e := hints.NewEngine(slogDefault())
	res, err := e.GetHints(context.Background(), &hints.HintRequest{Query: "anything"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("expected 0 hints, got %d", len(res))
	}
}

func TestGetHintsConcurrentProviders(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	p1 := &mockProvider{
		name: "p1",
		hints: []hints.Hint{
			{Provider: "p1", Content: "hint-a", Relevance: 0.9},
			{Provider: "p1", Content: "hint-b", Relevance: 0.3},
		},
	}
	p2 := &mockProvider{
		name: "p2",
		hints: []hints.Hint{
			{Provider: "p2", Content: "hint-c", Relevance: 0.7},
		},
	}
	p3 := &mockProvider{
		name: "p3",
		hints: []hints.Hint{
			{Provider: "p3", Content: "hint-d", Relevance: 0.5},
		},
	}

	e.RegisterProvider(p1)
	e.RegisterProvider(p2)
	e.RegisterProvider(p3)

	res, err := e.GetHints(context.Background(), &hints.HintRequest{
		Query:      "test",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	// All providers should have been called.
	if p1.called.Load() != 1 || p2.called.Load() != 1 || p3.called.Load() != 1 {
		t.Fatal("not all providers were called")
	}

	// All 4 hints should be returned.
	if len(res) != 4 {
		t.Fatalf("expected 4 hints, got %d", len(res))
	}

	// Verify sorted by descending relevance.
	if !sort.SliceIsSorted(res, func(i, j int) bool {
		return res[i].Relevance > res[j].Relevance
	}) {
		t.Fatalf("hints not sorted by relevance: %v", res)
	}

	// First hint should be the highest relevance.
	if res[0].Relevance != 0.9 {
		t.Fatalf("expected first hint relevance 0.9, got %f", res[0].Relevance)
	}
}

func TestGetHintsMaxResults(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	var manyHints []hints.Hint
	for i := 0; i < 20; i++ {
		manyHints = append(manyHints, hints.Hint{
			Provider:  "bulk",
			Content:   "hint",
			Relevance: float64(i) / 20.0,
		})
	}
	e.RegisterProvider(&mockProvider{name: "bulk", hints: manyHints})

	res, err := e.GetHints(context.Background(), &hints.HintRequest{
		Query:      "test",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 5 {
		t.Fatalf("expected 5 hints, got %d", len(res))
	}

	// The top 5 by relevance should be the ones with highest scores.
	if res[0].Relevance < res[4].Relevance {
		t.Fatal("expected descending relevance order")
	}
}

func TestGetHintsDefaultMaxResults(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	var manyHints []hints.Hint
	for i := 0; i < 15; i++ {
		manyHints = append(manyHints, hints.Hint{
			Provider:  "bulk",
			Content:   "hint",
			Relevance: float64(i) / 15.0,
		})
	}
	e.RegisterProvider(&mockProvider{name: "bulk", hints: manyHints})

	// MaxResults = 0 should default to 10.
	res, err := e.GetHints(context.Background(), &hints.HintRequest{Query: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 10 {
		t.Fatalf("expected default 10 hints, got %d", len(res))
	}
}

func TestGetHintsFilterProviders(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	p1 := &mockProvider{
		name:  "included",
		hints: []hints.Hint{{Provider: "included", Content: "yes", Relevance: 0.5}},
	}
	p2 := &mockProvider{
		name:  "excluded",
		hints: []hints.Hint{{Provider: "excluded", Content: "no", Relevance: 0.9}},
	}
	e.RegisterProvider(p1)
	e.RegisterProvider(p2)

	res, err := e.GetHints(context.Background(), &hints.HintRequest{
		Query:     "test",
		Providers: []string{"included"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if p2.called.Load() != 0 {
		t.Fatal("excluded provider should not have been called")
	}
	if len(res) != 1 || res[0].Provider != "included" {
		t.Fatalf("expected 1 hint from 'included', got %v", res)
	}
}

func TestGetHintsProviderError(t *testing.T) {
	e := hints.NewEngine(slogDefault())

	good := &mockProvider{
		name:  "good",
		hints: []hints.Hint{{Provider: "good", Content: "ok", Relevance: 0.5}},
	}
	bad := &mockProvider{
		name: "bad",
		err:  errors.New("provider failure"),
	}
	e.RegisterProvider(good)
	e.RegisterProvider(bad)

	// The failing provider should be silently skipped.
	res, err := e.GetHints(context.Background(), &hints.HintRequest{
		Query:      "test",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 hint (failing provider skipped), got %d", len(res))
	}
}

func TestGetHintsUnknownProviderFilter(t *testing.T) {
	e := hints.NewEngine(slogDefault())
	e.RegisterProvider(&mockProvider{name: "real"})

	res, err := e.GetHints(context.Background(), &hints.HintRequest{
		Query:     "test",
		Providers: []string{"nonexistent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("expected 0 hints for unknown provider filter, got %d", len(res))
	}
}

// slogDefault returns slog.Default() for test engine construction.
func slogDefault() *slog.Logger {
	return slog.Default()
}
