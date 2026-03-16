package memory

// RetrievalConfig controls the hybrid recall pipeline parameters.
type RetrievalConfig struct {
	// FusionK is the RRF k parameter. Default: 60.
	FusionK int

	// HalfLifeDays is the temporal decay half-life in days. Default: 30.
	// A memory accessed 30 days ago gets a decay factor of 0.5.
	HalfLifeDays int

	// AnchorPenalty is the score demotion factor for scoped shadowing.
	// Default: 0.3. Applied when a global result is semantically similar
	// to a project result (cosine > ShadowThreshold).
	AnchorPenalty float64

	// ShadowThreshold is the cosine similarity threshold above which
	// scoped shadowing is applied. Default: 0.85.
	ShadowThreshold float64

	// PersonaLimit is the max results from persona scope. Default: 3.
	PersonaLimit int

	// ProjectLimit is the max results from project scope. Default: 5.
	ProjectLimit int

	// GlobalLimit is the max results from global scope. Default: 2.
	GlobalLimit int

	// TotalLimit is the hard cap on total recall results. Default: 10.
	TotalLimit int
}

// DefaultRetrievalConfig returns production defaults.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		FusionK:         60,
		HalfLifeDays:    30,
		AnchorPenalty:   0.3,
		ShadowThreshold: 0.85,
		PersonaLimit:    3,
		ProjectLimit:    5,
		GlobalLimit:     2,
		TotalLimit:      10,
	}
}

// ConsolidationConfig controls the consolidation engine parameters.
type ConsolidationConfig struct {
	// OlderThanDays is the minimum age (since last access) before episodic
	// memories become consolidation candidates. Default: 30.
	OlderThanDays int

	// BatchSize is the max number of candidates to process per run. Default: 100.
	BatchSize int

	// ProjectCapacity is the max active memories per project scope. Default: 10000.
	ProjectCapacity int

	// GlobalCapacity is the max active global memories. Default: 50000.
	GlobalCapacity int
}

// DefaultConsolidationConfig returns production defaults.
func DefaultConsolidationConfig() ConsolidationConfig {
	return ConsolidationConfig{
		OlderThanDays:   30,
		BatchSize:       100,
		ProjectCapacity: 10000,
		GlobalCapacity:  50000,
	}
}

