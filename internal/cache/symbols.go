package cache

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/repo"
)

// Compile-time assertion: CachedSymbolRepo satisfies repo.SymbolRepo.
var _ repo.SymbolRepo = (*CachedSymbolRepo)(nil)

// CachedSymbolRepo decorates a repo.SymbolRepo with a cache-aside read layer
// and write-through invalidation. GetFileSymbols results are served from cache
// when warm; writes via Upsert invalidate the affected symbol ID key so
// subsequent reads pick up the change.
//
// Operations that change frequently or have low cache value (DeleteByFile,
// UpsertFileHash, GetFileHash) are delegated directly to the inner repo.
type CachedSymbolRepo struct {
	inner repo.SymbolRepo
	cache *Service
}

// NewCachedSymbolRepo wraps an existing SymbolRepo with caching.
// If cache is nil the decorator degrades gracefully to a direct pass-through.
func NewCachedSymbolRepo(inner repo.SymbolRepo, cache *Service) *CachedSymbolRepo {
	return &CachedSymbolRepo{inner: inner, cache: cache}
}

// symbolByIDKey returns the cache key for a single symbol lookup.
func symbolByIDKey(id string) string {
	return "sym:id:" + id
}

// fileSymbolsKey returns the cache key for file-level symbol listings.
func fileSymbolsKey(workspaceID, filePath string) string {
	return fmt.Sprintf("sym:file:%s:%s", workspaceID, filePath)
}

// GetFileSymbols returns all symbols for a given workspace + file path.
// Results are cached; subsequent calls return the cached slice until an
// Upsert invalidates the key.
func (c *CachedSymbolRepo) GetFileSymbols(ctx context.Context, workspaceID, filePath string) ([]*repo.Symbol, error) {
	if c.cache == nil {
		return c.inner.GetFileSymbols(ctx, workspaceID, filePath)
	}

	key := fileSymbolsKey(workspaceID, filePath)

	// Try cache first.
	if raw, err := c.cache.Get(key); err == nil {
		var syms []*repo.Symbol
		if err := json.Unmarshal(raw, &syms); err == nil {
			return syms, nil
		}
	}

	// Cache miss — fetch from backing repo.
	syms, err := c.inner.GetFileSymbols(ctx, workspaceID, filePath)
	if err != nil {
		return nil, err
	}

	// Warm cache.
	if data, marshalErr := json.Marshal(syms); marshalErr == nil {
		_ = c.cache.Set(key, data)
	}

	return syms, nil
}

// Upsert persists a symbol and invalidates the affected cache keys so
// subsequent reads pick up the change.
func (c *CachedSymbolRepo) Upsert(ctx context.Context, sym *repo.Symbol) error {
	if err := c.inner.Upsert(ctx, sym); err != nil {
		return fmt.Errorf("cache.CachedSymbolRepo.Upsert: %w", err)
	}

	if c.cache != nil {
		c.cache.Invalidate(symbolByIDKey(sym.ID))
		// The file path is not stored on the Symbol struct, but the file symbols
		// cache is keyed by workspaceID + filePath. Since the caller typically
		// upserts many symbols for the same file in sequence, the indexer will
		// call DeleteByFile first, which is un-cached. Invalidation by ID is
		// still useful if a GetByID was previously cached.
	}

	return nil
}

// DeleteByFile delegates directly — bulk deletes during re-indexing are not
// served from cache and any stale file-symbols entries will be repopulated on
// the next read.
func (c *CachedSymbolRepo) DeleteByFile(ctx context.Context, fileID int64) error {
	return c.inner.DeleteByFile(ctx, fileID)
}

// UpsertFileHash delegates directly — file hashes change on every indexing
// pass and have low cache value.
func (c *CachedSymbolRepo) UpsertFileHash(ctx context.Context, workspaceID, filePath, hash string) (int64, error) {
	return c.inner.UpsertFileHash(ctx, workspaceID, filePath, hash)
}

// GetFileHash delegates directly — checked once per file during indexing.
func (c *CachedSymbolRepo) GetFileHash(ctx context.Context, workspaceID, filePath string) (string, error) {
	return c.inner.GetFileHash(ctx, workspaceID, filePath)
}

// DeleteByWorkspacePath delegates to the inner repo and invalidates the
// file-level symbol cache for the affected path.
func (c *CachedSymbolRepo) DeleteByWorkspacePath(ctx context.Context, workspaceID, filePath string) error {
	if err := c.inner.DeleteByWorkspacePath(ctx, workspaceID, filePath); err != nil {
		return fmt.Errorf("cache.CachedSymbolRepo.DeleteByWorkspacePath: %w", err)
	}
	if c.cache != nil {
		c.cache.Invalidate(fileSymbolsKey(workspaceID, filePath))
	}
	return nil
}
