package refactor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Transaction represents an active multi-file refactoring session. It captures
// file snapshots before modifications so the entire set of changes can be
// rolled back atomically if validation fails.
type Transaction struct {
	// ID is the unique identifier for this transaction (UUID v4).
	ID string
	// StartedAt records when the transaction was created.
	StartedAt time.Time
	// Snapshots stores the original file contents keyed by absolute path.
	// Only the first snapshot per file is retained (subsequent calls are no-ops).
	Snapshots map[string][]byte
	// Modified tracks which files have been changed during this transaction.
	Modified map[string]bool

	mu sync.Mutex
}

// TransactionManager tracks active refactoring transactions. It is safe for
// concurrent use from multiple goroutines.
type TransactionManager struct {
	active map[string]*Transaction
	mu     sync.Mutex
	logger *slog.Logger
}

// NewTransactionManager creates a TransactionManager with the given logger.
func NewTransactionManager(logger *slog.Logger) *TransactionManager {
	return &TransactionManager{
		active: make(map[string]*Transaction),
		logger: logger,
	}
}

// Begin creates a new transaction and returns its ID. The transaction remains
// active until Commit or Rollback is called.
func (tm *TransactionManager) Begin() (string, error) {
	id := uuid.New().String()

	tx := &Transaction{
		ID:        id,
		StartedAt: time.Now(),
		Snapshots: make(map[string][]byte),
		Modified:  make(map[string]bool),
	}

	tm.mu.Lock()
	tm.active[id] = tx
	tm.mu.Unlock()

	tm.logger.Info("refactor transaction started", "transaction_id", id)
	return id, nil
}

// Get retrieves an active transaction by ID. Returns an error if the
// transaction does not exist.
func (tm *TransactionManager) Get(txID string) (*Transaction, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, ok := tm.active[txID]
	if !ok {
		return nil, fmt.Errorf("transaction %q not found", txID)
	}
	return tx, nil
}

// Commit finalises a transaction, clearing all snapshots and removing it from
// the active set. After commit the snapshots are no longer available for
// rollback.
func (tm *TransactionManager) Commit(txID string) error {
	tm.mu.Lock()
	tx, ok := tm.active[txID]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("transaction %q not found", txID)
	}
	delete(tm.active, txID)
	tm.mu.Unlock()

	tx.mu.Lock()
	fileCount := len(tx.Snapshots)
	tx.Snapshots = nil
	tx.Modified = nil
	tx.mu.Unlock()

	tm.logger.Info("refactor transaction committed",
		"transaction_id", txID,
		"files_touched", fileCount,
	)
	return nil
}

// Rollback restores every snapshotted file to its original content and removes
// the transaction from the active set. Files are written atomically (write to
// temp file, then rename) to minimise the risk of partial restores.
func (tm *TransactionManager) Rollback(txID string) error {
	tm.mu.Lock()
	tx, ok := tm.active[txID]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("transaction %q not found", txID)
	}
	delete(tm.active, txID)
	tm.mu.Unlock()

	tx.mu.Lock()
	defer tx.mu.Unlock()

	var firstErr error
	for path, original := range tx.Snapshots {
		if err := atomicWrite(path, original); err != nil {
			tm.logger.Error("rollback failed for file",
				"transaction_id", txID,
				"path", path,
				"error", err,
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("rollback %s: %w", path, err)
			}
		}
	}

	tx.Snapshots = nil
	tx.Modified = nil

	if firstErr != nil {
		return firstErr
	}

	tm.logger.Info("refactor transaction rolled back", "transaction_id", txID)
	return nil
}

// SnapshotFile reads the current contents of filePath and stores them in the
// transaction. If the file has already been snapshotted in this transaction
// the call is a no-op, preserving the original pre-transaction state.
func (tm *TransactionManager) SnapshotFile(txID, filePath string) error {
	tx, err := tm.Get(txID)
	if err != nil {
		return fmt.Errorf("refactor.TransactionManager.SnapshotFile: %w", err)
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	// Only capture the first snapshot per file.
	if _, exists := tx.Snapshots[filePath]; exists {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("snapshot read %s: %w", filePath, err)
	}

	tx.Snapshots[filePath] = data
	return nil
}

// MarkModified records that a file was modified during this transaction.
func (tm *TransactionManager) MarkModified(txID, filePath string) error {
	tx, err := tm.Get(txID)
	if err != nil {
		return fmt.Errorf("refactor.TransactionManager.MarkModified: %w", err)
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	tx.Modified[filePath] = true
	return nil
}

// ActiveFiles returns a map of filePath→txID for all files being tracked
// (snapshotted or modified) in active transactions. This is used by the
// conflict detector to check whether an external filesystem change affects
// a file that is part of an in-flight refactoring transaction.
func (tm *TransactionManager) ActiveFiles() map[string]string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	result := make(map[string]string)
	for txID, tx := range tm.active {
		tx.mu.Lock()
		for path := range tx.Snapshots {
			result[path] = txID
		}
		for path := range tx.Modified {
			result[path] = txID
		}
		tx.mu.Unlock()
	}
	return result
}

// atomicWrite writes data to the target path via a temporary file and rename,
// preserving the original file's permissions where possible. The temp file is
// created in the same directory as the target to ensure rename works across
// filesystem boundaries.
func atomicWrite(path string, data []byte) error {
	// Attempt to preserve original permissions.
	perm := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hyperax-rollback-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}

	return nil
}
