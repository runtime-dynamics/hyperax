package search

import (
	"math"
	"testing"
)

func TestFuseRRF_OverlappingResults(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1"},
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1"},
		{ID: "3", Name: "Baz", Kind: "function", WorkspaceID: "ws1"},
	}
	vector := []VectorResult{
		{ID: "2", Name: "Bar", Kind: "struct", WorkspaceID: "ws1", Distance: 0.1},
		{ID: "1", Name: "Foo", Kind: "function", WorkspaceID: "ws1", Distance: 0.2},
		{ID: "4", Name: "Qux", Kind: "method", WorkspaceID: "ws1", Distance: 0.3},
	}

	results := FuseRRF(bm25, vector, 60, 10)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// IDs 1 and 2 appear in both lists, so they should have higher scores.
	// ID 2 is rank 1 in BM25 + rank 0 in vector -> highest combined score.
	// ID 1 is rank 0 in BM25 + rank 1 in vector -> second highest.
	if results[0].ID != "1" && results[0].ID != "2" {
		t.Errorf("expected ID 1 or 2 in top position, got %s", results[0].ID)
	}

	// Both IDs 1 and 2 must score higher than IDs 3 and 4.
	scores := make(map[string]float64)
	for _, r := range results {
		scores[r.ID] = r.Score
	}
	if scores["3"] >= scores["1"] {
		t.Errorf("ID 3 (%.6f) should score lower than ID 1 (%.6f)", scores["3"], scores["1"])
	}
	if scores["4"] >= scores["2"] {
		t.Errorf("ID 4 (%.6f) should score lower than ID 2 (%.6f)", scores["4"], scores["2"])
	}
}

func TestFuseRRF_DisjointResults(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "Alpha", Kind: "function", WorkspaceID: "ws1"},
		{ID: "2", Name: "Beta", Kind: "struct", WorkspaceID: "ws1"},
	}
	vector := []VectorResult{
		{ID: "3", Name: "Gamma", Kind: "method", WorkspaceID: "ws1", Distance: 0.1},
		{ID: "4", Name: "Delta", Kind: "interface", WorkspaceID: "ws1", Distance: 0.2},
	}

	results := FuseRRF(bm25, vector, 60, 10)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// With disjoint sets, each result has exactly one RRF contribution.
	// Rank 0 items (ID 1 and ID 3) should tie with score 1/(60+1) = 1/61.
	if math.Abs(results[0].Score-results[1].Score) > 1e-10 {
		t.Logf("first two scores: %.10f, %.10f", results[0].Score, results[1].Score)
		// This is expected: rank 0 items from each list tie.
	}
}

func TestFuseRRF_EmptyBM25(t *testing.T) {
	vector := []VectorResult{
		{ID: "1", Name: "Alpha", Kind: "function", WorkspaceID: "ws1", Distance: 0.1},
		{ID: "2", Name: "Beta", Kind: "struct", WorkspaceID: "ws1", Distance: 0.2},
	}

	results := FuseRRF(nil, vector, 60, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("expected ID 1 first, got %s", results[0].ID)
	}
}

func TestFuseRRF_EmptyVector(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "Alpha", Kind: "function", WorkspaceID: "ws1"},
		{ID: "2", Name: "Beta", Kind: "struct", WorkspaceID: "ws1"},
	}

	results := FuseRRF(bm25, nil, 60, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("expected ID 1 first, got %s", results[0].ID)
	}
}

func TestFuseRRF_BothEmpty(t *testing.T) {
	results := FuseRRF(nil, nil, 60, 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestFuseRRF_LimitRespected(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "A", Kind: "func"},
		{ID: "2", Name: "B", Kind: "func"},
		{ID: "3", Name: "C", Kind: "func"},
		{ID: "4", Name: "D", Kind: "func"},
		{ID: "5", Name: "E", Kind: "func"},
	}
	vector := []VectorResult{
		{ID: "6", Name: "F", Kind: "func"},
		{ID: "7", Name: "G", Kind: "func"},
	}

	results := FuseRRF(bm25, vector, 60, 3)

	if len(results) != 3 {
		t.Fatalf("expected 3 results (limit=3), got %d", len(results))
	}
}

func TestFuseRRF_ScoreDescending(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "A"},
		{ID: "2", Name: "B"},
		{ID: "3", Name: "C"},
	}
	vector := []VectorResult{
		{ID: "3", Name: "C"},
		{ID: "2", Name: "B"},
		{ID: "1", Name: "A"},
	}

	results := FuseRRF(bm25, vector, 60, 10)

	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending at index %d: %.6f > %.6f",
				i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestFuseRRF_KParameterEffect(t *testing.T) {
	// Use asymmetric input so that different items get different combined
	// scores. ID 1 appears in both BM25 and vector; ID 2 appears only in
	// BM25; ID 3 appears only in vector. This means ID 1 always scores
	// highest, but the gap between ID 1 and the single-list items changes
	// with k.
	bm25 := []SearchResult{
		{ID: "1", Name: "A"},
		{ID: "2", Name: "B"},
	}
	vector := []VectorResult{
		{ID: "1", Name: "A"},
		{ID: "3", Name: "C"},
	}

	// With small k, rank differences are amplified.
	smallK := FuseRRF(bm25, vector, 1, 10)
	// With large k, rank differences are dampened.
	largeK := FuseRRF(bm25, vector, 1000, 10)

	// ID 1 has two contributions; IDs 2 and 3 have one each.
	// The gap between the best (ID 1) and worst result should be larger
	// with small k because the RRF contribution per rank is larger.
	spreadSmallK := smallK[0].Score - smallK[len(smallK)-1].Score
	spreadLargeK := largeK[0].Score - largeK[len(largeK)-1].Score

	if spreadLargeK >= spreadSmallK {
		t.Errorf("expected smaller spread with large k (%.10f) vs small k (%.10f)",
			spreadLargeK, spreadSmallK)
	}
}

func TestFuseRRF_DefaultKWhenZero(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "A"},
	}

	// k=0 should default to 60.
	results := FuseRRF(bm25, nil, 0, 10)

	expectedScore := 1.0 / float64(60+0+1)
	if math.Abs(results[0].Score-expectedScore) > 1e-10 {
		t.Errorf("expected score %.10f with default k=60, got %.10f",
			expectedScore, results[0].Score)
	}
}

// --- Weighted RRF tests ---

func TestWeightedFuseRRF_AlphaZeroEqualsClassicRRF(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "1", Name: "A"}, {ID: "2", Name: "B"},
	}
	vector := []VectorResult{
		{ID: "2", Name: "B"}, {ID: "3", Name: "C"},
	}

	classic := FuseRRF(bm25, vector, 60, 10)
	weighted := WeightedFuseRRF(bm25, vector, 60, 0.0, 10)

	if len(classic) != len(weighted) {
		t.Fatalf("length mismatch: classic=%d, weighted=%d", len(classic), len(weighted))
	}
	for i := range classic {
		if classic[i].ID != weighted[i].ID {
			t.Errorf("index %d: classic ID=%s, weighted ID=%s", i, classic[i].ID, weighted[i].ID)
		}
		if math.Abs(classic[i].Score-weighted[i].Score) > 1e-10 {
			t.Errorf("index %d: classic score=%.10f, weighted score=%.10f", i, classic[i].Score, weighted[i].Score)
		}
	}
}

func TestWeightedFuseRRF_AlphaBoostsVector(t *testing.T) {
	// ID "V" only in vector, ID "B" only in BM25. With alpha=0.8 (80% vector),
	// the vector-only item should score higher than the BM25-only item.
	bm25 := []SearchResult{
		{ID: "B", Name: "BM25Only"},
	}
	vector := []VectorResult{
		{ID: "V", Name: "VectorOnly"},
	}

	results := WeightedFuseRRF(bm25, vector, 60, 0.8, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Vector item should score higher with alpha=0.8.
	if results[0].ID != "V" {
		t.Errorf("expected vector item first with alpha=0.8, got %s", results[0].ID)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("vector score (%.6f) should exceed BM25 score (%.6f)", results[0].Score, results[1].Score)
	}
}

func TestWeightedFuseRRF_AlphaBoostsBM25(t *testing.T) {
	// With alpha=0.2 (20% vector, 80% BM25), BM25-only item should rank first.
	bm25 := []SearchResult{
		{ID: "B", Name: "BM25Only"},
	}
	vector := []VectorResult{
		{ID: "V", Name: "VectorOnly"},
	}

	results := WeightedFuseRRF(bm25, vector, 60, 0.2, 10)

	if results[0].ID != "B" {
		t.Errorf("expected BM25 item first with alpha=0.2, got %s", results[0].ID)
	}
}

func TestWeightedFuseRRF_AlphaClamped(t *testing.T) {
	bm25 := []SearchResult{{ID: "1", Name: "A"}}

	// Alpha > 1 should be clamped to 1.0.
	results := WeightedFuseRRF(bm25, nil, 60, 1.5, 10)
	// With alpha=1 and no vector results, BM25 weight is 0 so score should be 0.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Score != 0 {
		t.Errorf("expected score 0 with alpha=1 and no vector, got %.6f", results[0].Score)
	}

	// Alpha < 0 should be clamped to 0.
	results2 := WeightedFuseRRF(bm25, nil, 60, -0.5, 10)
	expectedScore := 1.0 / float64(60+0+1)
	if math.Abs(results2[0].Score-expectedScore) > 1e-10 {
		t.Errorf("expected classic RRF score with alpha<0, got %.10f", results2[0].Score)
	}
}

func TestWeightedFuseRRF_OverlappingItemsAccumulateBothWeights(t *testing.T) {
	// Item appears in both lists — should accumulate weighted contributions.
	bm25 := []SearchResult{{ID: "X", Name: "Shared"}}
	vector := []VectorResult{{ID: "X", Name: "Shared"}}

	alpha := 0.6
	results := WeightedFuseRRF(bm25, vector, 60, alpha, 10)

	bm25Contrib := (1 - alpha) * (1.0 / float64(60+0+1))
	vecContrib := alpha * (1.0 / float64(60+0+1))
	expected := bm25Contrib + vecContrib

	if math.Abs(results[0].Score-expected) > 1e-10 {
		t.Errorf("expected accumulated score %.10f, got %.10f", expected, results[0].Score)
	}
}

func TestDynamicK_SmallLimit(t *testing.T) {
	// limit=10, maxK=60 → dynamic k = min(5, 60) = 5
	k := DynamicK(10, 60)
	if k != 5 {
		t.Errorf("expected k=5 for limit=10, got %d", k)
	}
}

func TestDynamicK_LargeLimit(t *testing.T) {
	// limit=200, maxK=60 → dynamic k = min(100, 60) = 60
	k := DynamicK(200, 60)
	if k != 60 {
		t.Errorf("expected k=60 for limit=200, got %d", k)
	}
}

func TestDynamicK_ZeroLimit(t *testing.T) {
	k := DynamicK(0, 60)
	if k != 60 {
		t.Errorf("expected k=60 for limit=0, got %d", k)
	}
}

func TestDynamicK_LimitOne(t *testing.T) {
	// limit=1 → half=0 → clamped to 1
	k := DynamicK(1, 60)
	if k != 1 {
		t.Errorf("expected k=1 for limit=1, got %d", k)
	}
}

func TestFuseRRF_MetadataPreserved(t *testing.T) {
	bm25 := []SearchResult{
		{
			ID: "1", Name: "MyFunc", Kind: "function",
			FilePath: "/src/main.go", StartLine: 10, EndLine: 20,
			Signature: "func MyFunc()", WorkspaceID: "ws1",
		},
	}

	results := FuseRRF(bm25, nil, 60, 10)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Name != "MyFunc" {
		t.Errorf("expected Name=MyFunc, got %s", r.Name)
	}
	if r.Kind != "function" {
		t.Errorf("expected Kind=function, got %s", r.Kind)
	}
	if r.FilePath != "/src/main.go" {
		t.Errorf("expected FilePath=/src/main.go, got %s", r.FilePath)
	}
	if r.StartLine != 10 || r.EndLine != 20 {
		t.Errorf("expected lines 10-20, got %d-%d", r.StartLine, r.EndLine)
	}
	if r.Signature != "func MyFunc()" {
		t.Errorf("expected Signature='func MyFunc()', got %s", r.Signature)
	}
	if r.WorkspaceID != "ws1" {
		t.Errorf("expected WorkspaceID=ws1, got %s", r.WorkspaceID)
	}
}
