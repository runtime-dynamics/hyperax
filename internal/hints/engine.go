// Package hints implements the Hints Engine for Hyperax.
//
// The engine orchestrates multiple HintProvider implementations, querying them
// concurrently, merging results, and returning a relevance-sorted list trimmed
// to the caller's requested limit.
package hints

import (
	"context"
	"log/slog"
	"sort"
	"sync"
)

// Hint represents a single contextual hint returned by a provider.
type Hint struct {
	Provider  string  `json:"provider"`
	Category  string  `json:"category"`  // "standard", "error", "pattern", "doc", "git", "memory"
	Content   string  `json:"content"`
	Relevance float64 `json:"relevance"` // 0.0-1.0, higher is more relevant
	Source    string  `json:"source"`    // file path, doc name, config key, etc.
}

// HintRequest describes what kind of hints the caller is looking for.
type HintRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	Query       string   `json:"query"`       // current task / context description
	FilePath    string   `json:"file_path"`   // optional: currently edited file
	Language    string   `json:"language"`     // optional: programming language
	MaxResults  int      `json:"max_results"` // default 10
	Providers   []string `json:"providers"`   // optional: filter to specific providers
}

// defaultMaxResults is used when MaxResults is zero or negative.
const defaultMaxResults = 10

// HintProvider is the interface every hint source must implement.
type HintProvider interface {
	// Name returns the unique identifier for this provider (e.g. "standards").
	Name() string

	// GetHints queries the underlying data source and returns relevant hints.
	// Implementations MUST return a nil error with an empty slice when their
	// backing store is unavailable, rather than propagating the nil-repo error.
	GetHints(ctx context.Context, req *HintRequest) ([]Hint, error)
}

// Engine orchestrates multiple hint providers, querying them concurrently
// and merging results into a single relevance-sorted list.
type Engine struct {
	providers map[string]HintProvider
	logger    *slog.Logger
}

// NewEngine creates an Engine with no providers registered.
func NewEngine(logger *slog.Logger) *Engine {
	return &Engine{
		providers: make(map[string]HintProvider),
		logger:    logger,
	}
}

// RegisterProvider adds a provider to the engine. If a provider with the same
// name already exists it is silently replaced.
func (e *Engine) RegisterProvider(p HintProvider) {
	e.providers[p.Name()] = p
	e.logger.Info("hint provider registered", "provider", p.Name())
}

// ListProviders returns the names of all registered providers sorted
// alphabetically.
func (e *Engine) ListProviders() []string {
	names := make([]string, 0, len(e.providers))
	for name := range e.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetHints queries the requested providers concurrently, merges their results,
// sorts by descending relevance, and trims to MaxResults.
//
// If req.Providers is non-empty only those providers are queried; otherwise all
// registered providers participate. Unknown provider names are silently skipped.
func (e *Engine) GetHints(ctx context.Context, req *HintRequest) ([]Hint, error) {
	if req.MaxResults <= 0 {
		req.MaxResults = defaultMaxResults
	}

	targets := e.resolveProviders(req.Providers)
	if len(targets) == 0 {
		return []Hint{}, nil
	}

	// Collect hints from every target concurrently.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allHints []Hint
	)

	wg.Add(len(targets))

	for _, p := range targets {
		go func(prov HintProvider) {
			defer wg.Done()

			hints, err := prov.GetHints(ctx, req)
			if err != nil {
				e.logger.Warn("hint provider failed",
					"provider", prov.Name(),
					"error", err,
				)
				return
			}

			if len(hints) > 0 {
				mu.Lock()
				allHints = append(allHints, hints...)
				mu.Unlock()
			}
		}(p)
	}

	wg.Wait()

	// Sort by descending relevance (highest first).
	sort.Slice(allHints, func(i, j int) bool {
		return allHints[i].Relevance > allHints[j].Relevance
	})

	// Trim to requested limit.
	if len(allHints) > req.MaxResults {
		allHints = allHints[:req.MaxResults]
	}

	return allHints, nil
}

// resolveProviders returns the set of providers to query. When filter is
// non-empty only providers whose Name() appears in the filter are included.
func (e *Engine) resolveProviders(filter []string) []HintProvider {
	if len(filter) == 0 {
		out := make([]HintProvider, 0, len(e.providers))
		for _, p := range e.providers {
			out = append(out, p)
		}
		return out
	}

	out := make([]HintProvider, 0, len(filter))
	for _, name := range filter {
		if p, ok := e.providers[name]; ok {
			out = append(out, p)
		}
	}
	return out
}
