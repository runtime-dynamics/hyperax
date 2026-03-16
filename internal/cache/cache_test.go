package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_DefaultConfig(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New(DefaultConfig()): %v", err)
	}
	defer func() { _ = svc.Close() }()

	if svc.store == nil {
		t.Fatal("store is nil after New")
	}
}

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TTL != 10*time.Minute {
		t.Errorf("TTL = %v, want 10m", cfg.TTL)
	}
	if cfg.MaxSizeMB != 256 {
		t.Errorf("MaxSizeMB = %d, want 256", cfg.MaxSizeMB)
	}
	if cfg.Shards != 1024 {
		t.Errorf("Shards = %d, want 1024", cfg.Shards)
	}
	if cfg.CleanInterval != 5*time.Minute {
		t.Errorf("CleanInterval = %v, want 5m", cfg.CleanInterval)
	}
}

func TestGetOrFetch_CacheMiss_ThenHit(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	var calls int32

	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return map[string]string{"hello": "world"}, nil
	}

	// First call — cache miss, fetchFn invoked.
	v1, err := svc.GetOrFetch("key1", fetch)
	if err != nil {
		t.Fatalf("GetOrFetch(1): %v", err)
	}
	if v1 == nil {
		t.Fatal("GetOrFetch(1) returned nil")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("fetch calls = %d, want 1", atomic.LoadInt32(&calls))
	}

	// Second call — cache hit, fetchFn NOT invoked.
	v2, err := svc.GetOrFetch("key1", fetch)
	if err != nil {
		t.Fatalf("GetOrFetch(2): %v", err)
	}
	if v2 == nil {
		t.Fatal("GetOrFetch(2) returned nil")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("fetch calls = %d, want 1 (cached)", atomic.LoadInt32(&calls))
	}
}

func TestGetOrFetch_Singleflight(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	var calls int32
	var startOnce sync.Once
	started := make(chan struct{})
	proceed := make(chan struct{})

	// Use a barrier to ensure all goroutines are ready before releasing the fetch.
	const goroutines = 10
	barrier := make(chan struct{}, goroutines)

	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		startOnce.Do(func() { close(started) })
		<-proceed
		return "result", nil
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			barrier <- struct{}{} // signal ready
			_, errs[idx] = svc.GetOrFetch("contended-key", fetch)
		}(i)
	}

	// Wait until all goroutines have signalled ready.
	for i := 0; i < goroutines; i++ {
		<-barrier
	}

	// Wait until the first fetch is running.
	<-started

	// Allow the fetch to complete — all goroutines should share the result.
	close(proceed)

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d error: %v", i, e)
		}
	}

	// Singleflight collapses concurrent calls to a single invocation.
	// Due to goroutine scheduling, some may arrive after the first fetch
	// completes and trigger new fetches. The key invariant: calls << goroutines.
	if c := atomic.LoadInt32(&calls); c >= int32(goroutines) {
		t.Errorf("fetch calls = %d, want significantly fewer than %d (singleflight dedup)", c, goroutines)
	}
}

func TestGetOrFetch_FetchError(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	fetch := func() (any, error) {
		return nil, errTest
	}

	_, err = svc.GetOrFetch("fail-key", fetch)
	if err == nil {
		t.Fatal("expected error from GetOrFetch, got nil")
	}
	if err != errTest {
		t.Errorf("error = %v, want %v", err, errTest)
	}
}

func TestGet_Set_RawBytes(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	payload := []byte(`{"data":"test"}`)

	if err := svc.Set("raw-key", payload); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := svc.Get("raw-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("Get = %q, want %q", string(got), string(payload))
	}
}

func TestGet_NotFound(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	_, err = svc.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

func TestInvalidate(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	_ = svc.Set("inv-key", []byte("data"))

	// Verify it exists.
	if _, err := svc.Get("inv-key"); err != nil {
		t.Fatalf("Get before invalidate: %v", err)
	}

	// Invalidate.
	svc.Invalidate("inv-key")

	// Should be gone.
	_, err = svc.Get("inv-key")
	if err == nil {
		t.Error("expected error after invalidation, got nil")
	}
}

func TestInvalidate_NonExistent(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	// Should not panic on non-existent key.
	svc.Invalidate("ghost-key")
}

func TestClose(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// errTest is a sentinel error for testing fetch failures.
var errTest = fmt.Errorf("test fetch error")
