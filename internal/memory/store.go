package memory

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// MemoryStore is the top-level memory system facade, wrapping the repository
// with event publishing and orchestrating retrieval and consolidation.
type MemoryStore struct {
	repo          repo.MemoryRepo
	bus           *nervous.EventBus
	logger        *slog.Logger
	retrieval     *RetrievalEngine
	consolidation *ConsolidationEngine
}

// NewMemoryStore creates a MemoryStore with default configuration.
func NewMemoryStore(memRepo repo.MemoryRepo, bus *nervous.EventBus, logger *slog.Logger) *MemoryStore {
	ret := NewRetrievalEngine(memRepo, DefaultRetrievalConfig())
	con := NewConsolidationEngine(memRepo, bus, logger, DefaultConsolidationConfig())

	return &MemoryStore{
		repo:          memRepo,
		bus:           bus,
		logger:        logger,
		retrieval:     ret,
		consolidation: con,
	}
}

// Store inserts a new memory entry. Returns the generated ID.
func (s *MemoryStore) Store(ctx context.Context, memory *types.Memory) (string, error) {
	id, err := s.repo.Store(ctx, memory)
	if err != nil {
		return "", fmt.Errorf("memory.MemoryStore.Store: %w", err)
	}

	s.logger.Debug("memory stored",
		"id", id,
		"scope", memory.Scope,
		"type", memory.Type,
	)

	return id, nil
}

// Recall performs a scope-cascaded hybrid recall using the retrieval engine.
func (s *MemoryStore) Recall(ctx context.Context, query types.MemoryQuery) ([]types.MemoryContext, error) {
	results, err := s.retrieval.Recall(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory.MemoryStore.Recall: %w", err)
	}

	// Publish recall event.
	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventMemoryRecall,
			"memory",
			query.WorkspaceID,
			map[string]any{
				"query":        query.Query,
				"result_count": len(results),
				"persona_id":   query.PersonaID,
			},
		))
	}

	// Touch access for recalled memories.
	for _, mc := range results {
		if err := s.repo.TouchAccess(ctx, mc.Memory.ID); err != nil {
			s.logger.Warn("touch access failed", "id", mc.Memory.ID, "error", err)
		}
	}

	return results, nil
}

// Get retrieves a single memory by ID.
func (s *MemoryStore) Get(ctx context.Context, id string) (*types.Memory, error) {
	return s.repo.Get(ctx, id)
}

// Delete removes a memory by ID.
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// Consolidate runs the consolidation engine for the given scope.
func (s *MemoryStore) Consolidate(ctx context.Context, scope types.MemoryScope, olderThanDays int, dryRun bool) (int, error) {
	return s.consolidation.Run(ctx, scope, olderThanDays, dryRun)
}

// Count returns memory count for a scope and workspace.
func (s *MemoryStore) Count(ctx context.Context, scope types.MemoryScope, workspaceID string) (int, error) {
	return s.repo.Count(ctx, scope, workspaceID)
}

// CountByType returns counts grouped by memory type.
func (s *MemoryStore) CountByType(ctx context.Context, scope types.MemoryScope, workspaceID string) (map[types.MemoryType]int, error) {
	return s.repo.CountByType(ctx, scope, workspaceID)
}

// StoreAnnotation inserts a new annotation on an existing memory.
func (s *MemoryStore) StoreAnnotation(ctx context.Context, ann *types.MemoryAnnotation) (string, error) {
	return s.repo.StoreAnnotation(ctx, ann)
}

// GetAnnotations retrieves annotations for a memory.
func (s *MemoryStore) GetAnnotations(ctx context.Context, memoryID string) ([]*types.MemoryAnnotation, error) {
	return s.repo.GetAnnotations(ctx, memoryID)
}

// Retrieval returns the underlying retrieval engine for direct access.
func (s *MemoryStore) Retrieval() *RetrievalEngine {
	return s.retrieval
}

// Repo returns the underlying memory repository.
func (s *MemoryStore) Repo() repo.MemoryRepo {
	return s.repo
}
