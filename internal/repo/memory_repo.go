package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// MemoryRepo handles scoped agent memory storage and hybrid retrieval.
type MemoryRepo interface {
	// Store inserts a new memory and returns its generated ID.
	Store(ctx context.Context, memory *types.Memory) (string, error)

	// Get retrieves a single memory by ID.
	Get(ctx context.Context, id string) (*types.Memory, error)

	// Delete removes a memory by ID.
	Delete(ctx context.Context, id string) error

	// Recall searches memories via FTS5 BM25 (falls back to LIKE if FTS5 unavailable).
	// Scope, workspaceID, and personaID filter the result set. Empty values are ignored.
	Recall(ctx context.Context, query string, scope types.MemoryScope, workspaceID, personaID string, limit int) ([]*types.Memory, error)

	// TouchAccess updates accessed_at and increments access_count for the given memory.
	TouchAccess(ctx context.Context, id string) error

	// ListConsolidationCandidates returns episodic memories older than the given
	// threshold that are candidates for consolidation into semantic memories.
	ListConsolidationCandidates(ctx context.Context, scope types.MemoryScope, olderThanDays int, limit int) ([]*types.Memory, error)

	// MarkConsolidated sets consolidated_into on the given memory IDs, pointing
	// them to the newly created target memory.
	MarkConsolidated(ctx context.Context, ids []string, targetID string) error

	// MarkContested flags a memory as contested by another memory.
	MarkContested(ctx context.Context, id, contestedByID string) error

	// Count returns the number of memories for the given scope and workspace.
	// Empty workspaceID counts across all workspaces for that scope.
	Count(ctx context.Context, scope types.MemoryScope, workspaceID string) (int, error)

	// CountByType returns memory counts grouped by type for a given scope.
	CountByType(ctx context.Context, scope types.MemoryScope, workspaceID string) (map[types.MemoryType]int, error)

	// StoreAnnotation inserts a new annotation attached to a memory.
	StoreAnnotation(ctx context.Context, ann *types.MemoryAnnotation) (string, error)

	// GetAnnotations retrieves all annotations for the given memory ID.
	GetAnnotations(ctx context.Context, memoryID string) ([]*types.MemoryAnnotation, error)
}
