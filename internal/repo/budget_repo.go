package repo

import "context"

// BudgetRepo handles context economics budget tracking.
type BudgetRepo interface {
	GetCumulativeEnergyCost(ctx context.Context, scope string) (float64, error)
	GetBudgetThreshold(ctx context.Context, scope string) (float64, error)
	SetBudgetThreshold(ctx context.Context, scope string, threshold float64) error
	RecordEnergyCost(ctx context.Context, scope string, cost float64, providerID string, model string) error
	ListThresholdScopes(ctx context.Context) ([]string, error)
}
