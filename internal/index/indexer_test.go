package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
)

// ---------- mock repositories ----------

// mockSymbolRepo is an in-memory implementation of repo.SymbolRepo for testing.
type mockSymbolRepo struct {
	mu         sync.Mutex
	fileHashes map[string]mockFileEntry // key: "workspaceID|filePath"
	symbols    map[int64][]*repo.Symbol
	nextFileID int64
}

type mockFileEntry struct {
	fileID int64
	hash   string
}

func newMockSymbolRepo() *mockSymbolRepo {
	return &mockSymbolRepo{
		fileHashes: make(map[string]mockFileEntry),
		symbols:    make(map[int64][]*repo.Symbol),
		nextFileID: 1,
	}
}

func (m *mockSymbolRepo) key(workspaceID, filePath string) string {
	return workspaceID + "|" + filePath
}

func (m *mockSymbolRepo) UpsertFileHash(_ context.Context, workspaceID, filePath, hash string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(workspaceID, filePath)
	if entry, ok := m.fileHashes[k]; ok {
		entry.hash = hash
		m.fileHashes[k] = entry
		return entry.fileID, nil
	}

	id := m.nextFileID
	m.nextFileID++
	m.fileHashes[k] = mockFileEntry{fileID: id, hash: hash}
	return id, nil
}

func (m *mockSymbolRepo) GetFileHash(_ context.Context, workspaceID, filePath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(workspaceID, filePath)
	entry, ok := m.fileHashes[k]
	if !ok {
		return "", fmt.Errorf("get file hash: %w", sql.ErrNoRows)
	}
	return entry.hash, nil
}

func (m *mockSymbolRepo) Upsert(_ context.Context, sym *repo.Symbol) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.symbols[sym.FileID] = append(m.symbols[sym.FileID], sym)
	return nil
}

func (m *mockSymbolRepo) GetFileSymbols(_ context.Context, workspaceID, filePath string) ([]*repo.Symbol, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(workspaceID, filePath)
	entry, ok := m.fileHashes[k]
	if !ok {
		return nil, nil
	}
	return m.symbols[entry.fileID], nil
}

func (m *mockSymbolRepo) DeleteByFile(_ context.Context, fileID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.symbols, fileID)
	return nil
}

func (m *mockSymbolRepo) DeleteByWorkspacePath(_ context.Context, workspaceID, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := workspaceID + "|" + filePath
	entry, ok := m.fileHashes[key]
	if ok {
		delete(m.symbols, entry.fileID)
		delete(m.fileHashes, key)
	}
	return nil
}

func (m *mockSymbolRepo) allSymbols() []*repo.Symbol {
	m.mu.Lock()
	defer m.mu.Unlock()

	var all []*repo.Symbol
	for _, syms := range m.symbols {
		all = append(all, syms...)
	}
	return all
}

// mockSearchRepo is an in-memory implementation of repo.SearchRepo for testing.
type mockSearchRepo struct {
	mu     sync.Mutex
	chunks []*repo.DocChunk
}

func newMockSearchRepo() *mockSearchRepo {
	return &mockSearchRepo{}
}

func (m *mockSearchRepo) SearchSymbols(_ context.Context, _ []string, _ string, _ string, _ int) ([]*repo.Symbol, error) {
	return nil, nil
}

func (m *mockSearchRepo) UpsertDocChunk(_ context.Context, chunk *repo.DocChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.chunks = append(m.chunks, chunk)
	return nil
}

func (m *mockSearchRepo) SearchDocs(_ context.Context, _ []string, _ string, _ int) ([]*repo.DocChunk, error) {
	return nil, nil
}

func (m *mockSearchRepo) SearchCodeContent(_ context.Context, _ []string, _ string, _ int) ([]*repo.DocChunk, error) {
	return nil, nil
}

func (m *mockSearchRepo) DeleteDocChunksByPath(_ context.Context, workspaceID, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := m.chunks[:0]
	for _, c := range m.chunks {
		if c.WorkspaceID != workspaceID || c.FilePath != filePath {
			filtered = append(filtered, c)
		}
	}
	m.chunks = filtered
	return nil
}

// ---------- helper ----------

// setupWorkspaceDir creates a temp directory with sample .go and .md files for
// testing. Returns the root directory path.
func setupWorkspaceDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	goSrc := `package main

func Hello() string {
	return "hello"
}

type Config struct {
	Port int
}
`
	mdSrc := `# Overview

This is the overview section.

## Getting Started

Follow these steps.

## API Reference

Endpoint details here.
`

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(mdSrc), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	// Create a subdirectory with another Go file.
	subDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}

	pkgSrc := `package pkg

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(subDir, "math.go"), []byte(pkgSrc), 0o644); err != nil {
		t.Fatalf("write math.go: %v", err)
	}

	return root
}

// ---------- IndexWorkspace tests ----------

func TestIndexWorkspace_ScansGoAndMdFiles(t *testing.T) {
	root := setupWorkspaceDir(t)
	symRepo := newMockSymbolRepo()
	searchRepo := newMockSearchRepo()
	bus := nervous.NewEventBus(16)
	sub := bus.Subscribe("test", nil)
	defer bus.Unsubscribe("test")

	idx := NewIndexer(symRepo, searchRepo, bus, nil)
	result, err := idx.IndexWorkspace(context.Background(), "ws-test", root)
	if err != nil {
		t.Fatalf("IndexWorkspace: %v", err)
	}

	// 2 Go files + 1 markdown file = 3 scanned.
	if result.FilesScanned != 3 {
		t.Errorf("FilesScanned = %d, want 3", result.FilesScanned)
	}

	// main.go: Hello (function), Config (struct) = 2
	// pkg/math.go: Add (function) = 1
	// Total = 3 symbols.
	if result.SymbolsFound != 3 {
		t.Errorf("SymbolsFound = %d, want 3", result.SymbolsFound)
	}

	// README.md has 3 headings = 3 chunks.
	if result.DocsChunked != 3 {
		t.Errorf("DocsChunked = %d, want 3", result.DocsChunked)
	}

	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}

	// Verify the event was published.
	select {
	case event := <-sub.Ch:
		if event.Type != EventIndexWorkspaceIndexed {
			t.Errorf("event type = %s, want %s", event.Type, EventIndexWorkspaceIndexed)
		}
		if event.Scope != "ws-test" {
			t.Errorf("event scope = %s, want ws-test", event.Scope)
		}
	default:
		t.Error("expected an event to be published")
	}
}

func TestIndexWorkspace_IgnoresHiddenAndVendorDirs(t *testing.T) {
	root := t.TempDir()

	// Create files in ignored directories.
	for _, dir := range []string{".git", "node_modules", "vendor", ".hidden"} {
		dirPath := filepath.Join(root, dir)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dirPath, "file.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatalf("write %s/file.go: %v", dir, err)
		}
	}

	// Create one valid Go file at root.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	idx := NewIndexer(newMockSymbolRepo(), newMockSearchRepo(), nil, nil)
	result, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("IndexWorkspace: %v", err)
	}

	// Only the root main.go should be scanned.
	if result.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", result.FilesScanned)
	}
	if result.SymbolsFound != 1 {
		t.Errorf("SymbolsFound = %d, want 1 (Run)", result.SymbolsFound)
	}
}

func TestShouldIgnore(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{".git", true},
		{"node_modules", true},
		{"vendor", true},
		{".build", true},
		{"__pycache__", true},
		{".hidden", true},
		{"src", false},
		{"internal", false},
		{".", false}, // current dir should not be ignored
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := shouldIgnore(tt.path)
			if got != tt.expect {
				t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.expect)
			}
		})
	}
}

func TestHashFile_Consistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello world\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash1, err := HashFile(path)
	if err != nil {
		t.Fatalf("hash1: %v", err)
	}

	hash2, err := HashFile(path)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("hashes differ: %q != %q", hash1, hash2)
	}

	// SHA-256 hex is always 64 characters.
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
}

func TestHashFile_DifferentContent(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(path1, []byte("content A"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(path2, []byte("content B"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}

	hash1, err := HashFile(path1)
	if err != nil {
		t.Fatalf("hash file 1: %v", err)
	}
	hash2, err := HashFile(path2)
	if err != nil {
		t.Fatalf("hash file 2: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different files should produce different hashes")
	}
}

func TestHashFile_NonexistentFile(t *testing.T) {
	_, err := HashFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestChunkMarkdown_BasicSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	content := `# Title

First paragraph.

## Section A

Content of section A.

## Section B

Content of section B.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	chunks, err := ChunkMarkdown(path, "ws-1", "fakehash")
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk.
	if chunks[0].SectionHeader != "Title" {
		t.Errorf("chunk 0 header = %q, want Title", chunks[0].SectionHeader)
	}
	if chunks[0].ChunkIndex != 0 {
		t.Errorf("chunk 0 index = %d, want 0", chunks[0].ChunkIndex)
	}
	if !strings.Contains(chunks[0].Content, "First paragraph") {
		t.Error("chunk 0 should contain 'First paragraph'")
	}

	// Second chunk.
	if chunks[1].SectionHeader != "Section A" {
		t.Errorf("chunk 1 header = %q, want Section A", chunks[1].SectionHeader)
	}

	// Third chunk.
	if chunks[2].SectionHeader != "Section B" {
		t.Errorf("chunk 2 header = %q, want Section B", chunks[2].SectionHeader)
	}

	// Verify token count is set.
	for i, c := range chunks {
		if c.TokenCount < 1 {
			t.Errorf("chunk %d: token count should be >= 1, got %d", i, c.TokenCount)
		}
	}
}

func TestChunkMarkdown_NoHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	content := "Just some plain text without headings.\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	chunks, err := ChunkMarkdown(path, "ws-1", "hash")
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].SectionHeader != "" {
		t.Errorf("expected empty header for no-heading doc, got %q", chunks[0].SectionHeader)
	}
}

func TestIndexWorkspace_IncrementalSkipsUnchanged(t *testing.T) {
	root := t.TempDir()

	goSrc := `package main

func Main() {}
`
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	symRepo := newMockSymbolRepo()
	searchRepo := newMockSearchRepo()
	idx := NewIndexer(symRepo, searchRepo, nil, nil)

	// First index should scan the file.
	result1, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	if result1.FilesScanned != 1 {
		t.Fatalf("first scan: FilesScanned = %d, want 1", result1.FilesScanned)
	}

	// Second index should skip it (same hash).
	result2, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if result2.FilesSkipped != 1 {
		t.Errorf("second scan: FilesSkipped = %d, want 1", result2.FilesSkipped)
	}
	if result2.FilesScanned != 0 {
		t.Errorf("second scan: FilesScanned = %d, want 0", result2.FilesScanned)
	}
}

func TestIndexWorkspace_IncrementalReindexesChanged(t *testing.T) {
	root := t.TempDir()

	v1 := `package main

func V1() {}
`
	path := filepath.Join(root, "app.go")
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	symRepo := newMockSymbolRepo()
	searchRepo := newMockSearchRepo()
	idx := NewIndexer(symRepo, searchRepo, nil, nil)

	// First index.
	_, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Modify the file.
	v2 := `package main

func V2() {}

func Extra() {}
`
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Second index should re-scan the changed file.
	result, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}

	if result.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", result.FilesScanned)
	}
	if result.SymbolsFound != 2 {
		t.Errorf("SymbolsFound = %d, want 2 (V2, Extra)", result.SymbolsFound)
	}
}

func TestIndexFile_SingleGoFile(t *testing.T) {
	root := t.TempDir()
	goSrc := `package main

func IndexMe() error {
	return nil
}

type Result struct {
	Value int
}
`
	if err := os.WriteFile(filepath.Join(root, "target.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	symRepo := newMockSymbolRepo()
	idx := NewIndexer(symRepo, newMockSearchRepo(), nil, nil)

	err := idx.IndexFile(context.Background(), "ws-1", root, "target.go")
	if err != nil {
		t.Fatalf("IndexFile: %v", err)
	}

	// Verify symbols were stored.
	syms := symRepo.allSymbols()
	if len(syms) != 2 {
		t.Errorf("expected 2 symbols (IndexMe, Result), got %d", len(syms))
	}
}

func TestIndexFile_SingleMarkdownFile(t *testing.T) {
	root := t.TempDir()
	mdSrc := `# Doc Title

Some content.

## Details

More content here.
`
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte(mdSrc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	searchRepo := newMockSearchRepo()
	idx := NewIndexer(newMockSymbolRepo(), searchRepo, nil, nil)

	err := idx.IndexFile(context.Background(), "ws-1", root, "notes.md")
	if err != nil {
		t.Fatalf("IndexFile: %v", err)
	}

	searchRepo.mu.Lock()
	chunkCount := len(searchRepo.chunks)
	searchRepo.mu.Unlock()

	if chunkCount != 2 {
		t.Errorf("expected 2 chunks, got %d", chunkCount)
	}
}

func TestIndexFile_NonexistentFile(t *testing.T) {
	root := t.TempDir()
	idx := NewIndexer(newMockSymbolRepo(), newMockSearchRepo(), nil, nil)

	err := idx.IndexFile(context.Background(), "ws-1", root, "missing.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestIndexWorkspace_SkipsBinaryFiles(t *testing.T) {
	root := t.TempDir()

	// Create a binary file with null bytes and a .go extension to test the
	// binary detection guard.
	binaryContent := []byte("package main\x00\x00\x00func Bad() {}")
	if err := os.WriteFile(filepath.Join(root, "binary.go"), binaryContent, 0o644); err != nil {
		t.Fatalf("write binary.go: %v", err)
	}

	// Create a valid Go file to confirm it is still indexed.
	validSrc := "package main\n\nfunc Good() {}\n"
	if err := os.WriteFile(filepath.Join(root, "valid.go"), []byte(validSrc), 0o644); err != nil {
		t.Fatalf("write valid.go: %v", err)
	}

	idx := NewIndexer(newMockSymbolRepo(), newMockSearchRepo(), nil, nil)
	result, err := idx.IndexWorkspace(context.Background(), "ws-1", root)
	if err != nil {
		t.Fatalf("IndexWorkspace: %v", err)
	}

	// Only the valid file should be scanned; the binary one should be skipped.
	if result.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", result.FilesScanned)
	}
	if result.SymbolsFound != 1 {
		t.Errorf("SymbolsFound = %d, want 1 (Good)", result.SymbolsFound)
	}
}

func TestIndexWorkspace_ContextCancellation(t *testing.T) {
	root := setupWorkspaceDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	idx := NewIndexer(newMockSymbolRepo(), newMockSearchRepo(), nil, nil)
	_, err := idx.IndexWorkspace(ctx, "ws-1", root)
	if err == nil {
		t.Error("expected error when context is already cancelled")
	}
}

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	// Text file.
	textPath := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(textPath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if isBinaryFile(textPath) {
		t.Error("text file should not be detected as binary")
	}

	// Binary file.
	binPath := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(binPath, []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if !isBinaryFile(binPath) {
		t.Error("binary file should be detected as binary")
	}
}

func TestNewIndexer_NilLogger(t *testing.T) {
	idx := NewIndexer(newMockSymbolRepo(), newMockSearchRepo(), nil, nil)
	if idx.logger == nil {
		t.Error("logger should not be nil even when passed nil")
	}
}
