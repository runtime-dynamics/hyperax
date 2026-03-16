package refactor

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// newTestTxMgr creates a TransactionManager with a discarding logger.
func newTestTxMgr() *TransactionManager {
	return NewTransactionManager(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// writeTempFile creates a temporary file with the given content and returns its
// absolute path. The file is automatically removed when the test finishes.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "txtest-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

func TestBeginCreatesTransaction(t *testing.T) {
	tm := newTestTxMgr()

	txID, err := tm.Begin()
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	if txID == "" {
		t.Fatal("Begin() returned empty transaction ID")
	}

	tx, err := tm.Get(txID)
	if err != nil {
		t.Fatalf("Get(%q) error: %v", txID, err)
	}
	if tx.ID != txID {
		t.Errorf("tx.ID = %q, want %q", tx.ID, txID)
	}
	if tx.Snapshots == nil {
		t.Error("tx.Snapshots is nil, expected initialised map")
	}
	if tx.Modified == nil {
		t.Error("tx.Modified is nil, expected initialised map")
	}
}

func TestCommitClearsSnapshots(t *testing.T) {
	tm := newTestTxMgr()

	txID, _ := tm.Begin()
	path := writeTempFile(t, "original content\n")

	if err := tm.SnapshotFile(txID, path); err != nil {
		t.Fatalf("SnapshotFile() error: %v", err)
	}

	if err := tm.Commit(txID); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	// Transaction should no longer be accessible.
	if _, err := tm.Get(txID); err == nil {
		t.Error("Get() after Commit should return error, got nil")
	}
}

func TestRollbackRestoresFiles(t *testing.T) {
	tm := newTestTxMgr()

	original := "line one\nline two\nline three\n"
	path := writeTempFile(t, original)

	txID, _ := tm.Begin()
	if err := tm.SnapshotFile(txID, path); err != nil {
		t.Fatalf("SnapshotFile() error: %v", err)
	}

	// Overwrite the file with different content.
	if err := os.WriteFile(path, []byte("completely different content\n"), 0644); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	if err := tm.Rollback(txID); err != nil {
		t.Fatalf("Rollback() error: %v", err)
	}

	// Verify the file was restored.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after rollback: %v", err)
	}
	if string(data) != original {
		t.Errorf("file content after rollback = %q, want %q", string(data), original)
	}
}

func TestSnapshotCapturesFileContent(t *testing.T) {
	tm := newTestTxMgr()
	txID, _ := tm.Begin()

	content := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	path := writeTempFile(t, content)

	if err := tm.SnapshotFile(txID, path); err != nil {
		t.Fatalf("SnapshotFile() error: %v", err)
	}

	tx, _ := tm.Get(txID)
	tx.mu.Lock()
	snap, ok := tx.Snapshots[path]
	tx.mu.Unlock()

	if !ok {
		t.Fatal("snapshot not found for file")
	}
	if string(snap) != content {
		t.Errorf("snapshot content = %q, want %q", string(snap), content)
	}
}

func TestGetUnknownTransactionReturnsError(t *testing.T) {
	tm := newTestTxMgr()

	_, err := tm.Get("nonexistent-id")
	if err == nil {
		t.Error("Get() with unknown ID should return error, got nil")
	}
}

func TestDoubleSnapshotDoesNotOverwrite(t *testing.T) {
	tm := newTestTxMgr()
	txID, _ := tm.Begin()

	original := "original content\n"
	path := writeTempFile(t, original)

	if err := tm.SnapshotFile(txID, path); err != nil {
		t.Fatalf("first SnapshotFile() error: %v", err)
	}

	// Overwrite the file, then snapshot again.
	if err := os.WriteFile(path, []byte("modified content\n"), 0644); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	if err := tm.SnapshotFile(txID, path); err != nil {
		t.Fatalf("second SnapshotFile() error: %v", err)
	}

	// The snapshot should still hold the original content.
	tx, _ := tm.Get(txID)
	tx.mu.Lock()
	snap := tx.Snapshots[path]
	tx.mu.Unlock()

	if string(snap) != original {
		t.Errorf("snapshot after double-call = %q, want %q (original)", string(snap), original)
	}
}

func TestSnapshotNonexistentFileReturnsError(t *testing.T) {
	tm := newTestTxMgr()
	txID, _ := tm.Begin()

	err := tm.SnapshotFile(txID, filepath.Join(t.TempDir(), "does-not-exist.go"))
	if err == nil {
		t.Error("SnapshotFile() on nonexistent file should return error, got nil")
	}
}

func TestCommitUnknownTransactionReturnsError(t *testing.T) {
	tm := newTestTxMgr()

	if err := tm.Commit("bad-id"); err == nil {
		t.Error("Commit() with unknown ID should return error, got nil")
	}
}

func TestRollbackUnknownTransactionReturnsError(t *testing.T) {
	tm := newTestTxMgr()

	if err := tm.Rollback("bad-id"); err == nil {
		t.Error("Rollback() with unknown ID should return error, got nil")
	}
}

func TestMarkModified(t *testing.T) {
	tm := newTestTxMgr()
	txID, _ := tm.Begin()

	path := "/some/file.go"
	if err := tm.MarkModified(txID, path); err != nil {
		t.Fatalf("MarkModified() error: %v", err)
	}

	tx, _ := tm.Get(txID)
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.Modified[path] {
		t.Errorf("Modified[%q] = false, want true", path)
	}
}
