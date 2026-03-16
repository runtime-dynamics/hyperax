package mysql

import (
	"context"
	"database/sql"
	"fmt"
)

// BudgetRepo implements repo.BudgetRepo for MySQL.
type BudgetRepo struct {
	db *sql.DB
}

// GetCumulativeEnergyCost returns the total cost recorded for the given scope.
func (r *BudgetRepo) GetCumulativeEnergyCost(ctx context.Context, scope string) (float64, error) {
	var total float64
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM budget_records WHERE scope = ?`,
		scope,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("mysql.BudgetRepo.GetCumulativeEnergyCost: %w", err)
	}
	return total, nil
}

// GetBudgetThreshold returns the configured budget threshold for the given scope.
func (r *BudgetRepo) GetBudgetThreshold(ctx context.Context, scope string) (float64, error) {
	var threshold float64
	err := r.db.QueryRowContext(ctx,
		`SELECT threshold FROM budget_thresholds WHERE scope = ?`,
		scope,
	).Scan(&threshold)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("budget threshold for scope %q not found", scope)
	}
	if err != nil {
		return 0, fmt.Errorf("mysql.BudgetRepo.GetBudgetThreshold: %w", err)
	}
	return threshold, nil
}

// SetBudgetThreshold creates or replaces the budget threshold for the given scope.
func (r *BudgetRepo) SetBudgetThreshold(ctx context.Context, scope string, threshold float64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO budget_thresholds (scope, threshold, updated_at)
		 VALUES (?, ?, NOW())
		 ON DUPLICATE KEY UPDATE
		   threshold = VALUES(threshold),
		   updated_at = NOW()`,
		scope, threshold,
	)
	if err != nil {
		return fmt.Errorf("mysql.BudgetRepo.SetBudgetThreshold: %w", err)
	}
	return nil
}

// RecordEnergyCost inserts a new cost record for the given scope.
// providerID and model are optional — pass empty strings when not applicable.
func (r *BudgetRepo) RecordEnergyCost(ctx context.Context, scope string, cost float64, providerID string, model string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO budget_records (scope, cost, provider_id, model) VALUES (?, ?, ?, ?)`,
		scope, cost, providerID, model,
	)
	if err != nil {
		return fmt.Errorf("mysql.BudgetRepo.RecordEnergyCost: %w", err)
	}
	return nil
}

// ListThresholdScopes returns all distinct scopes that have a budget threshold configured.
func (r *BudgetRepo) ListThresholdScopes(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT scope FROM budget_thresholds`)
	if err != nil {
		return nil, fmt.Errorf("mysql.BudgetRepo.ListThresholdScopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("mysql.BudgetRepo.ListThresholdScopes: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.BudgetRepo.ListThresholdScopes: %w", err)
	}
	return scopes, nil
}
