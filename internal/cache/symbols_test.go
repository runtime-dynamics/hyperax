package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// mockSymbolRepo is a minimal in-memory implementation of repo.SymbolRepo
// that tracks call counts for verifying cache behaviour.
type mockSymbolRepo struct {
	symbols        map[string]*repo.Symbol           // keyed by ID
	fileSymbols    map[string][]*repo.Symbol          // keyed by "ws:path"
	fileHashes     map[string]string                  // keyed by "ws:path"
	getFileCalls   int32
	upsertCalls    int32
	deleteCalls    int32
}

func newMockSymbolRepo() *mockSymbolRepo {
	return &mockSymbolRepo{
		symbols:     make(map[string]*repo.Symbol),
		fileSymbols: make(map[string][]*repo.Symbol),
		fileHashes:  make(map[string]string),
	}
}

func (m *mockSymbolRepo) UpsertFileHash(_ context.Context, workspaceID, filePath, hash string) (int64, error) {
	key := workspaceID + ":" + filePath
	m.fileHashes[key] = hash
	return 1, nil
}

func (m *mockSymbolRepo) GetFileHash(_ context.Context, workspaceID, filePath string) (string, error) {
	key := workspaceID + ":" + filePath
	if h, ok := m.fileHashes[key]; ok {
		return h, nil
	}
	return "", fmt.Errorf("hash not found")
}

func (m *mockSymbolRepo) Upsert(_ context.Context, sym *repo.Symbol) error {
	atomic.AddInt32(&m.upsertCalls, 1)
	m.symbols[sym.ID] = sym
	return nil
}

func (m *mockSymbolRepo) GetFileSymbols(_ context.Context, workspaceID, filePath string) ([]*repo.Symbol, error) {
	atomic.AddInt32(&m.getFileCalls, 1)
	key := workspaceID + ":" + filePath
	if syms, ok := m.fileSymbols[key]; ok {
		return syms, nil
	}
	return nil, nil
}

func (m *mockSymbolRepo) DeleteByFile(_ context.Context, fileID int64) error {
	atomic.AddInt32(&m.deleteCalls, 1)
	return nil
}

func (m *mockSymbolRepo) DeleteByWorkspacePath(_ context.Context, _, _ string) error {
	return nil
}

func TestCachedSymbolRepo_GetFileSymbols_CacheHit(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	mock := newMockSymbolRepo()
	mock.fileSymbols["ws1:main.go"] = []*repo.Symbol{
		{ID: "s1", WorkspaceID: "ws1", Name: "main", Kind: "function"},
		{ID: "s2", WorkspaceID: "ws1", Name: "init", Kind: "function"},
	}

	cached := NewCachedSymbolRepo(mock, svc)
	ctx := context.Background()

	// First call — cache miss, hits inner repo.
	syms, err := cached.GetFileSymbols(ctx, "ws1", "main.go")
	if err != nil {
		t.Fatalf("GetFileSymbols(1): %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("GetFileSymbols(1) len = %d, want 2", len(syms))
	}
	if atomic.LoadInt32(&mock.getFileCalls) != 1 {
		t.Errorf("inner calls = %d, want 1", atomic.LoadInt32(&mock.getFileCalls))
	}

	// Second call — cache hit, inner repo NOT called.
	syms, err = cached.GetFileSymbols(ctx, "ws1", "main.go")
	if err != nil {
		t.Fatalf("GetFileSymbols(2): %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("GetFileSymbols(2) len = %d, want 2", len(syms))
	}
	if atomic.LoadInt32(&mock.getFileCalls) != 1 {
		t.Errorf("inner calls = %d, want 1 (cached)", atomic.LoadInt32(&mock.getFileCalls))
	}
}

func TestCachedSymbolRepo_Upsert_InvalidatesCache(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	mock := newMockSymbolRepo()
	mock.fileSymbols["ws1:main.go"] = []*repo.Symbol{
		{ID: "s1", WorkspaceID: "ws1", Name: "main", Kind: "function"},
	}

	cached := NewCachedSymbolRepo(mock, svc)
	ctx := context.Background()

	// Warm the cache.
	_, err = cached.GetFileSymbols(ctx, "ws1", "main.go")
	if err != nil {
		t.Fatalf("GetFileSymbols: %v", err)
	}
	if atomic.LoadInt32(&mock.getFileCalls) != 1 {
		t.Fatalf("inner calls = %d, want 1", atomic.LoadInt32(&mock.getFileCalls))
	}

	// Upsert should invalidate the symbol ID key.
	err = cached.Upsert(ctx, &repo.Symbol{ID: "s1", WorkspaceID: "ws1", Name: "main_updated"})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if atomic.LoadInt32(&mock.upsertCalls) != 1 {
		t.Errorf("upsert calls = %d, want 1", atomic.LoadInt32(&mock.upsertCalls))
	}

	// Verify the symbolByIDKey is invalidated by checking the raw cache.
	_, getErr := svc.Get(symbolByIDKey("s1"))
	if getErr == nil {
		t.Error("expected symbolByIDKey to be invalidated after Upsert")
	}
}

func TestCachedSymbolRepo_DeleteByFile_DelegatesDirect(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	mock := newMockSymbolRepo()
	cached := NewCachedSymbolRepo(mock, svc)

	if err := cached.DeleteByFile(context.Background(), 42); err != nil {
		t.Fatalf("DeleteByFile: %v", err)
	}
	if atomic.LoadInt32(&mock.deleteCalls) != 1 {
		t.Errorf("delete calls = %d, want 1", atomic.LoadInt32(&mock.deleteCalls))
	}
}

func TestCachedSymbolRepo_UpsertFileHash_DelegatesDirect(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	mock := newMockSymbolRepo()
	cached := NewCachedSymbolRepo(mock, svc)
	ctx := context.Background()

	id, err := cached.UpsertFileHash(ctx, "ws1", "main.go", "abc123")
	if err != nil {
		t.Fatalf("UpsertFileHash: %v", err)
	}
	if id != 1 {
		t.Errorf("UpsertFileHash returned %d, want 1", id)
	}
	if mock.fileHashes["ws1:main.go"] != "abc123" {
		t.Errorf("hash = %q, want abc123", mock.fileHashes["ws1:main.go"])
	}
}

func TestCachedSymbolRepo_GetFileHash_DelegatesDirect(t *testing.T) {
	svc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = svc.Close() }()

	mock := newMockSymbolRepo()
	mock.fileHashes["ws1:main.go"] = "abc123"
	cached := NewCachedSymbolRepo(mock, svc)

	hash, err := cached.GetFileHash(context.Background(), "ws1", "main.go")
	if err != nil {
		t.Fatalf("GetFileHash: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want abc123", hash)
	}
}

func TestCachedSymbolRepo_NilCache_Passthrough(t *testing.T) {
	mock := newMockSymbolRepo()
	mock.fileSymbols["ws1:main.go"] = []*repo.Symbol{
		{ID: "s1", WorkspaceID: "ws1", Name: "main", Kind: "function"},
	}

	// nil cache — all calls delegate directly.
	cached := NewCachedSymbolRepo(mock, nil)
	ctx := context.Background()

	syms, err := cached.GetFileSymbols(ctx, "ws1", "main.go")
	if err != nil {
		t.Fatalf("GetFileSymbols: %v", err)
	}
	if len(syms) != 1 {
		t.Errorf("len = %d, want 1", len(syms))
	}

	// Should hit inner repo every time (no cache).
	_, _ = cached.GetFileSymbols(ctx, "ws1", "main.go")
	if atomic.LoadInt32(&mock.getFileCalls) != 2 {
		t.Errorf("inner calls = %d, want 2 (no cache)", atomic.LoadInt32(&mock.getFileCalls))
	}
}
