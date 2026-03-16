package fsconflict

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/pkg/types"
)

// helper: create a temp file, start a transaction, and snapshot it.
func setupTrackedFile(t *testing.T, txMgr *refactor.TransactionManager) (txID string, filePath string) {
	t.Helper()

	dir := t.TempDir()
	fp := filepath.Join(dir, "tracked.go")
	if err := os.WriteFile(fp, []byte("package test"), 0644); err != nil {
		t.Fatal(err)
	}

	id, err := txMgr.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := txMgr.SnapshotFile(id, fp); err != nil {
		t.Fatal(err)
	}
	return id, fp
}

// simulateFSEvent publishes a synthetic fs event on the bus.
func simulateFSEvent(bus *nervous.EventBus, eventType types.EventType, path, op string) {
	bus.Publish(nervous.NewEvent(eventType, "test", "filesystem", map[string]string{
		"path": path,
		"op":   op,
	}))
}

func TestConflictDetection_TrackedFileModified(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := nervous.NewEventBus(64)
	txMgr := refactor.NewTransactionManager(logger)

	txID, filePath := setupTrackedFile(t, txMgr)

	// Subscribe to conflict events before starting detector.
	conflictSub := bus.SubscribeTypes("test.conflict.listener", types.EventFSConflictDetected)
	defer bus.Unsubscribe(conflictSub.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cd := NewConflictDetector(bus, txMgr, logger)
	go cd.Start(ctx)

	// Give the detector time to subscribe.
	time.Sleep(20 * time.Millisecond)

	// Simulate an external modify on the tracked file.
	simulateFSEvent(bus, nervous.EventFSModify, filePath, "WRITE")

	// Wait for the conflict event.
	select {
	case evt := <-conflictSub.Ch:
		if evt.Type != types.EventFSConflictDetected {
			t.Fatalf("expected %s, got %s", types.EventFSConflictDetected, evt.Type)
		}

		var payload map[string]any
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			t.Fatal(err)
		}

		if payload["file_path"] != filePath {
			t.Errorf("expected file_path=%q, got %q", filePath, payload["file_path"])
		}
		if payload["transaction_id"] != txID {
			t.Errorf("expected transaction_id=%q, got %q", txID, payload["transaction_id"])
		}
		if payload["fs_operation"] != "WRITE" {
			t.Errorf("expected fs_operation=WRITE, got %q", payload["fs_operation"])
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for conflict event")
	}
}

func TestConflictDetection_UntrackedFileNoConflict(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := nervous.NewEventBus(64)
	txMgr := refactor.NewTransactionManager(logger)

	// Track a file, but emit an event for a different path.
	_, _ = setupTrackedFile(t, txMgr)

	conflictSub := bus.SubscribeTypes("test.conflict.listener", types.EventFSConflictDetected)
	defer bus.Unsubscribe(conflictSub.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cd := NewConflictDetector(bus, txMgr, logger)
	go cd.Start(ctx)

	time.Sleep(20 * time.Millisecond)

	// Emit event for an untracked file.
	simulateFSEvent(bus, nervous.EventFSModify, "/some/other/file.go", "WRITE")

	// No conflict event should arrive.
	select {
	case evt := <-conflictSub.Ch:
		t.Fatalf("unexpected conflict event: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		// Expected: no conflict.
	}
}

func TestConflictDetection_DeleteAndRename(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := nervous.NewEventBus(64)
	txMgr := refactor.NewTransactionManager(logger)

	_, filePath := setupTrackedFile(t, txMgr)

	conflictSub := bus.SubscribeTypes("test.conflict.listener", types.EventFSConflictDetected)
	defer bus.Unsubscribe(conflictSub.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cd := NewConflictDetector(bus, txMgr, logger)
	go cd.Start(ctx)

	time.Sleep(20 * time.Millisecond)

	// Test delete triggers conflict.
	simulateFSEvent(bus, nervous.EventFSDelete, filePath, "REMOVE")

	select {
	case evt := <-conflictSub.Ch:
		if evt.Type != types.EventFSConflictDetected {
			t.Fatalf("expected conflict event, got %s", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete conflict event")
	}

	// Test rename triggers conflict.
	simulateFSEvent(bus, nervous.EventFSRename, filePath, "RENAME")

	select {
	case evt := <-conflictSub.Ch:
		if evt.Type != types.EventFSConflictDetected {
			t.Fatalf("expected conflict event, got %s", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rename conflict event")
	}
}

func TestConflictDetection_CreateIgnored(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := nervous.NewEventBus(64)
	txMgr := refactor.NewTransactionManager(logger)

	_, filePath := setupTrackedFile(t, txMgr)

	conflictSub := bus.SubscribeTypes("test.conflict.listener", types.EventFSConflictDetected)
	defer bus.Unsubscribe(conflictSub.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cd := NewConflictDetector(bus, txMgr, logger)
	go cd.Start(ctx)

	time.Sleep(20 * time.Millisecond)

	// fs.create events should NOT trigger a conflict even for tracked paths.
	simulateFSEvent(bus, nervous.EventFSCreate, filePath, "CREATE")

	select {
	case evt := <-conflictSub.Ch:
		t.Fatalf("unexpected conflict event for create: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		// Expected: no conflict for create.
	}
}

func TestConflictDetection_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := nervous.NewEventBus(64)
	txMgr := refactor.NewTransactionManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	cd := NewConflictDetector(bus, txMgr, logger)

	done := make(chan struct{})
	go func() {
		cd.Start(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(2 * time.Second):
		t.Fatal("detector did not stop after context cancellation")
	}

	// Verify the detector unsubscribed (bus should have no subscribers).
	if count := bus.SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after stop, got %d", count)
	}
}
