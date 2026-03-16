package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// BudgetRepo implements repo.BudgetRepo for PostgreSQL.
type BudgetRepo struct {
	db *sql.DB
}

// GetCumulativeEnergyCost returns the total cost recorded for the given scope.
// Returns 0.0 if no records exist.
func (r *BudgetRepo) GetCumulativeEnergyCost(ctx context.Context, scope string) (float64, error) {
	var total float64
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM budget_records WHERE scope = $1`,
		scope,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("postgres.BudgetRepo.GetCumulativeEnergyCost: %w", err)
	}
	return total, nil
}

// GetBudgetThreshold returns the configured budget threshold for the given scope.
// Returns an error if no threshold has been set.
func (r *BudgetRepo) GetBudgetThreshold(ctx context.Context, scope string) (float64, error) {
	var threshold float64
	err := r.db.QueryRowContext(ctx,
		`SELECT threshold FROM budget_thresholds WHERE scope = $1`,
		scope,
	).Scan(&threshold)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("budget threshold for scope %q not found", scope)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres.BudgetRepo.GetBudgetThreshold: %w", err)
	}
	return threshold, nil
}

// SetBudgetThreshold creates or replaces the budget threshold for the given scope.
func (r *BudgetRepo) SetBudgetThreshold(ctx context.Context, scope string, threshold float64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO budget_thresholds (scope, threshold, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT(scope) DO UPDATE SET
		   threshold = EXCLUDED.threshold,
		   updated_at = EXCLUDED.updated_at`,
		scope, threshold,
	)
	if err != nil {
		return fmt.Errorf("postgres.BudgetRepo.SetBudgetThreshold: %w", err)
	}
	return nil
}

// RecordEnergyCost inserts a new cost record for the given scope.
// providerID and model are optional — pass empty strings when not applicable.
func (r *BudgetRepo) RecordEnergyCost(ctx context.Context, scope string, cost float64, providerID string, model string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO budget_records (scope, cost, provider_id, model) VALUES ($1, $2, $3, $4)`,
		scope, cost, providerID, model,
	)
	if err != nil {
		return fmt.Errorf("postgres.BudgetRepo.RecordEnergyCost: %w", err)
	}
	return nil
}

// ListThresholdScopes returns all distinct scopes that have a budget threshold configured.
func (r *BudgetRepo) ListThresholdScopes(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT scope FROM budget_thresholds`)
	if err != nil {
		return nil, fmt.Errorf("postgres.BudgetRepo.ListThresholdScopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("postgres.BudgetRepo.ListThresholdScopes: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.BudgetRepo.ListThresholdScopes: %w", err)
	}
	return scopes, nil
}
