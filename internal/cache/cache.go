// Package cache provides an in-memory caching layer built on BigCache with
// singleflight stampede protection. It is designed to sit between MCP handlers
// and the repository layer, reducing SQLite load for hot-path reads (symbols,
// config, file hashes) while keeping write-through semantics simple.
//
// Usage:
//
//	svc, err := cache.New(cache.DefaultConfig())
//	val, err := svc.GetOrFetch("symbols:ws:main.go", func() (any, error) {
//	    return repo.GetFileSymbols(ctx, ws, "main.go")
//	})
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/allegro/bigcache/v3"
	"golang.org/x/sync/singleflight"
)

// Config holds cache configuration parameters.
type Config struct {
	// TTL is the time-to-live for each cache entry.
	TTL time.Duration `yaml:"ttl"`

	// MaxSizeMB is the hard maximum cache size in megabytes.
	MaxSizeMB int `yaml:"max_size_mb"`

	// Shards is the number of internal cache shards. Must be a power of two.
	Shards int `yaml:"shards"`

	// CleanInterval is the frequency of the background eviction sweep.
	CleanInterval time.Duration `yaml:"clean_interval"`
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		TTL:           10 * time.Minute,
		MaxSizeMB:     256,
		Shards:        1024,
		CleanInterval: 5 * time.Minute,
	}
}

// Service wraps BigCache with singleflight stampede protection.
// All methods are safe for concurrent use.
type Service struct {
	store *bigcache.BigCache
	sf    singleflight.Group
}

// New creates a cache Service from the given configuration.
// Returns an error if BigCache cannot be initialised (e.g. invalid shard count).
func New(cfg Config) (*Service, error) {
	bcfg := bigcache.DefaultConfig(cfg.TTL)
	bcfg.HardMaxCacheSize = cfg.MaxSizeMB
	bcfg.Shards = cfg.Shards
	bcfg.CleanWindow = cfg.CleanInterval

	store, err := bigcache.New(context.Background(), bcfg)
	if err != nil {
		return nil, fmt.Errorf("cache.New: %w", err)
	}
	return &Service{store: store}, nil
}

// GetOrFetch checks the cache for the given key. On a miss it uses singleflight
// to ensure only one goroutine executes fetchFn, then warms the cache with the
// result for subsequent callers.
//
// The value returned by fetchFn must be JSON-serialisable because BigCache
// stores raw bytes. On cache hit the returned value is a generic any obtained
// via json.Unmarshal.
func (c *Service) GetOrFetch(key string, fetchFn func() (any, error)) (any, error) {
	// 1. Check cache.
	if cached, err := c.store.Get(key); err == nil {
		var result any
		if err := json.Unmarshal(cached, &result); err == nil {
			return result, nil
		}
	}

	// 2. Singleflight: only one goroutine fetches from the backing store.
	v, err, _ := c.sf.Do(key, func() (any, error) {
		result, err := fetchFn()
		if err != nil {
			return nil, err
		}

		// 3. Warm cache for subsequent requests.
		data, marshalErr := json.Marshal(result)
		if marshalErr == nil {
			_ = c.store.Set(key, data)
		}

		return result, nil
	})

	return v, err
}

// Get retrieves a raw cached value by key.
// Returns bigcache.ErrEntryNotFound if the key is not present.
func (c *Service) Get(key string) ([]byte, error) {
	return c.store.Get(key)
}

// Set stores a raw value in the cache under the given key.
func (c *Service) Set(key string, value []byte) error {
	return c.store.Set(key, value)
}

// Invalidate removes a key from the cache. It is a no-op if the key does not
// exist.
func (c *Service) Invalidate(key string) {
	_ = c.store.Delete(key)
}

// Close releases all cache resources. The Service must not be used after Close.
func (c *Service) Close() error {
	return c.store.Close()
}
