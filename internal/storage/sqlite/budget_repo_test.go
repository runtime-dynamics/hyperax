package sqlite

import (
	"testing"
)

func TestBudgetRepo_RecordAndGetCumulative(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &BudgetRepo{db: db.db}

	scope := "workspace:hyperax"

	// No records initially, cost should be 0.
	cost, err := r.GetCumulativeEnergyCost(ctx, scope)
	if err != nil {
		t.Fatalf("get cumulative (empty): %v", err)
	}
	if cost != 0 {
		t.Errorf("expected 0.0, got %f", cost)
	}

	// Record some costs.
	if err := r.RecordEnergyCost(ctx, scope, 1.5, "", ""); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := r.RecordEnergyCost(ctx, scope, 2.3, "", ""); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := r.RecordEnergyCost(ctx, scope, 0.7, "", ""); err != nil {
		t.Fatalf("record 3: %v", err)
	}

	cost, err = r.GetCumulativeEnergyCost(ctx, scope)
	if err != nil {
		t.Fatalf("get cumulative: %v", err)
	}

	expected := 4.5
	if cost < expected-0.001 || cost > expected+0.001 {
		t.Errorf("cumulative cost = %f, want %f", cost, expected)
	}
}

func TestBudgetRepo_CumulativeScopedIsolation(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &BudgetRepo{db: db.db}

	if err := r.RecordEnergyCost(ctx, "scope-a", 10.0, "", ""); err != nil {
		t.Fatalf("record scope-a: %v", err)
	}
	if err := r.RecordEnergyCost(ctx, "scope-b", 5.0, "", ""); err != nil {
		t.Fatalf("record scope-b: %v", err)
	}

	costA, err := r.GetCumulativeEnergyCost(ctx, "scope-a")
	if err != nil {
		t.Fatalf("get scope-a: %v", err)
	}
	if costA < 9.99 || costA > 10.01 {
		t.Errorf("scope-a cost = %f, want 10.0", costA)
	}

	costB, err := r.GetCumulativeEnergyCost(ctx, "scope-b")
	if err != nil {
		t.Fatalf("get scope-b: %v", err)
	}
	if costB < 4.99 || costB > 5.01 {
		t.Errorf("scope-b cost = %f, want 5.0", costB)
	}
}

func TestBudgetRepo_SetAndGetThreshold(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &BudgetRepo{db: db.db}

	scope := "workspace:hyperax"

	if err := r.SetBudgetThreshold(ctx, scope, 100.0); err != nil {
		t.Fatalf("set threshold: %v", err)
	}

	threshold, err := r.GetBudgetThreshold(ctx, scope)
	if err != nil {
		t.Fatalf("get threshold: %v", err)
	}
	if threshold < 99.99 || threshold > 100.01 {
		t.Errorf("threshold = %f, want 100.0", threshold)
	}
}

func TestBudgetRepo_SetThresholdOverwrite(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &BudgetRepo{db: db.db}

	scope := "workspace:test"

	if err := r.SetBudgetThreshold(ctx, scope, 50.0); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := r.SetBudgetThreshold(ctx, scope, 200.0); err != nil {
		t.Fatalf("set second: %v", err)
	}

	threshold, err := r.GetBudgetThreshold(ctx, scope)
	if err != nil {
		t.Fatalf("get threshold: %v", err)
	}
	if threshold < 199.99 || threshold > 200.01 {
		t.Errorf("threshold = %f, want 200.0", threshold)
	}
}

func TestBudgetRepo_GetThresholdNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &BudgetRepo{db: db.db}

	_, err := r.GetBudgetThreshold(ctx, "nonexistent-scope")
	if err == nil {
		t.Fatal("expected error for nonexistent threshold")
	}
}
