package memory

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// mockMemoryRepo implements repo.MemoryRepo for testing the retrieval engine.
type mockMemoryRepo struct {
	memories []*types.Memory
}

func (m *mockMemoryRepo) Store(_ context.Context, mem *types.Memory) (string, error) {
	m.memories = append(m.memories, mem)
	return mem.ID, nil
}

func (m *mockMemoryRepo) Get(_ context.Context, id string) (*types.Memory, error) {
	for _, mem := range m.memories {
		if mem.ID == id {
			return mem, nil
		}
	}
	return nil, nil
}

func (m *mockMemoryRepo) Delete(_ context.Context, _ string) error { return nil }

func (m *mockMemoryRepo) Recall(_ context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error) {
	var results []*types.Memory
	for _, mem := range m.memories {
		if scope != "" && mem.Scope != scope {
			continue
		}
		if workspaceID != "" && mem.WorkspaceID != workspaceID && mem.WorkspaceID != "" {
			continue
		}
		if personaID != "" && mem.PersonaID != personaID && mem.PersonaID != "" {
			continue
		}
		if mem.ConsolidatedInto != "" || mem.ContestedBy != "" {
			continue
		}
		results = append(results, mem)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (m *mockMemoryRepo) TouchAccess(_ context.Context, _ string) error { return nil }

func (m *mockMemoryRepo) ListConsolidationCandidates(_ context.Context, _ types.MemoryScope, _ int, _ int) ([]*types.Memory, error) {
	return nil, nil
}

func (m *mockMemoryRepo) MarkConsolidated(_ context.Context, _ []string, _ string) error {
	return nil
}

func (m *mockMemoryRepo) MarkContested(_ context.Context, _, _ string) error { return nil }

func (m *mockMemoryRepo) Count(_ context.Context, _ types.MemoryScope, _ string) (int, error) {
	return len(m.memories), nil
}

func (m *mockMemoryRepo) CountByType(_ context.Context, _ types.MemoryScope, _ string) (map[types.MemoryType]int, error) {
	return nil, nil
}

func (m *mockMemoryRepo) StoreAnnotation(_ context.Context, _ *types.MemoryAnnotation) (string, error) {
	return "", nil
}

func (m *mockMemoryRepo) GetAnnotations(_ context.Context, _ string) ([]*types.MemoryAnnotation, error) {
	return nil, nil
}

func TestTemporalDecay(t *testing.T) {
	tests := []struct {
		days     float64
		halfLife int
		wantMin  float64
		wantMax  float64
	}{
		{0, 30, 0.99, 1.01},    // Today: decay ≈ 1.0
		{30, 30, 0.49, 0.51},   // 30 days = half-life: decay ≈ 0.5
		{60, 30, 0.24, 0.26},   // 60 days: decay ≈ 0.25
		{1, 30, 0.97, 0.98},    // Yesterday: decay ≈ 0.977
	}

	for _, tt := range tests {
		decay := TemporalDecay(tt.days, tt.halfLife)
		if decay < tt.wantMin || decay > tt.wantMax {
			t.Errorf("TemporalDecay(%v, %d) = %v, want [%v, %v]",
				tt.days, tt.halfLife, decay, tt.wantMin, tt.wantMax)
		}
	}
}

func TestTemporalDecay_NegativeDays(t *testing.T) {
	// Negative days should clamp to 0.
	decay := TemporalDecay(-5, 30)
	if math.Abs(decay-1.0) > 0.01 {
		t.Errorf("TemporalDecay(-5, 30) = %v, want ≈1.0", decay)
	}
}

func TestRetrievalEngine_ScopeCascade(t *testing.T) {
	now := time.Now()

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "p1", Scope: types.MemoryScopePersona, Content: "persona memory", PersonaID: "agent-1", WorkspaceID: "ws-1", AccessedAt: now},
			{ID: "pr1", Scope: types.MemoryScopeProject, Content: "project memory", WorkspaceID: "ws-1", AccessedAt: now},
			{ID: "g1", Scope: types.MemoryScopeGlobal, Content: "global memory", AccessedAt: now},
		},
	}

	engine := NewRetrievalEngine(repo, DefaultRetrievalConfig())

	results, err := engine.Recall(context.Background(), types.MemoryQuery{
		Query:       "memory",
		PersonaID:   "agent-1",
		WorkspaceID: "ws-1",
		MaxResults:  10,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// Should get all 3 scopes.
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results should be ranked by score.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: [%d].Score=%v > [%d].Score=%v",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestRetrievalEngine_EmptyQuery(t *testing.T) {
	engine := NewRetrievalEngine(&mockMemoryRepo{}, DefaultRetrievalConfig())

	results, err := engine.Recall(context.Background(), types.MemoryQuery{})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestRetrievalEngine_GlobalOnly(t *testing.T) {
	now := time.Now()

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "g1", Scope: types.MemoryScopeGlobal, Content: "rule one", AccessedAt: now},
			{ID: "g2", Scope: types.MemoryScopeGlobal, Content: "rule two", AccessedAt: now},
			{ID: "g3", Scope: types.MemoryScopeGlobal, Content: "rule three", AccessedAt: now},
		},
	}

	cfg := DefaultRetrievalConfig()
	cfg.GlobalLimit = 2
	engine := NewRetrievalEngine(repo, cfg)

	results, err := engine.Recall(context.Background(), types.MemoryQuery{
		Query:      "rule",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// Should be capped at GlobalLimit.
	if len(results) != 2 {
		t.Errorf("expected 2 results (global limit), got %d", len(results))
	}
}

func TestRetrievalEngine_TemporalDecayAffectsRanking(t *testing.T) {
	now := time.Now()
	old := now.Add(-60 * 24 * time.Hour) // 60 days ago

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "old", Scope: types.MemoryScopeGlobal, Content: "old memory", AccessedAt: old},
			{ID: "new", Scope: types.MemoryScopeGlobal, Content: "new memory", AccessedAt: now},
		},
	}

	engine := NewRetrievalEngine(repo, DefaultRetrievalConfig())

	results, err := engine.Recall(context.Background(), types.MemoryQuery{
		Query:      "memory",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// New memory should rank higher due to temporal decay.
	if results[0].Memory.ID != "new" {
		t.Errorf("expected 'new' to rank first, got %q", results[0].Memory.ID)
	}
}

func TestRetrievalEngine_AnchoredBoost(t *testing.T) {
	now := time.Now()

	repo := &mockMemoryRepo{
		memories: []*types.Memory{
			{ID: "normal", Scope: types.MemoryScopeGlobal, Content: "normal memory", AccessedAt: now},
			{ID: "anchored", Scope: types.MemoryScopeGlobal, Content: "anchored law",
				Metadata: map[string]any{"anchored": true}, AccessedAt: now},
		},
	}

	engine := NewRetrievalEngine(repo, DefaultRetrievalConfig())

	results, err := engine.Recall(context.Background(), types.MemoryQuery{
		Query:      "memory",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// Anchored memory should rank higher.
	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}
	if results[0].Memory.ID != "anchored" {
		t.Errorf("expected 'anchored' to rank first, got %q", results[0].Memory.ID)
	}
}

func TestDedup(t *testing.T) {
	results := []types.MemoryContext{
		{Memory: types.Memory{ID: "a"}, Score: 0.5},
		{Memory: types.Memory{ID: "b"}, Score: 0.3},
		{Memory: types.Memory{ID: "a"}, Score: 0.8}, // duplicate with higher score
	}

	deduped := dedup(results)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 after dedup, got %d", len(deduped))
	}

	// The "a" entry should have the higher score.
	for _, mc := range deduped {
		if mc.Memory.ID == "a" && mc.Score != 0.8 {
			t.Errorf("expected score 0.8 for 'a', got %v", mc.Score)
		}
	}
}
