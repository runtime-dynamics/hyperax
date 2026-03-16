package types

// BudgetStatus summarizes the current budget state for a scope.
type BudgetStatus struct {
	Scope       string  `json:"scope"`
	Threshold   float64 `json:"threshold"`
	Cumulative  float64 `json:"cumulative"`
	Remaining   float64 `json:"remaining"`
	PercentUsed float64 `json:"percent_used"`
	Exhausted   bool    `json:"exhausted"`
}

// NewBudgetStatus creates a BudgetStatus from raw values.
func NewBudgetStatus(scope string, threshold, cumulative float64) BudgetStatus {
	remaining := threshold - cumulative
	if remaining < 0 {
		remaining = 0
	}
	var pct float64
	if threshold > 0 {
		pct = (cumulative / threshold) * 100
	}
	return BudgetStatus{
		Scope:       scope,
		Threshold:   threshold,
		Cumulative:  cumulative,
		Remaining:   remaining,
		PercentUsed: pct,
		Exhausted:   cumulative >= threshold && threshold > 0,
	}
}
