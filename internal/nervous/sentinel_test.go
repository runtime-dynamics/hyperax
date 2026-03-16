package nervous

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestSentinel_FileCreate(t *testing.T) {
	bus := NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sentinel := NewSentinel(bus, logger)
	dir := t.TempDir()

	if err := sentinel.Watch(dir); err != nil {
		t.Fatalf("watch: %v", err)
	}

	// Subscribe to all events to capture the file create event.
	sub := bus.Subscribe("test-sentinel", nil)
	defer bus.Unsubscribe("test-sentinel")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sentinel.Run(ctx)

	// Create a file in the watched directory.
	testFile := filepath.Join(dir, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Wait for the fs.create event.
	found := waitForEventType(sub.Ch, EventFSCreate, 3*time.Second)
	if !found {
		t.Error("expected fs.create event, timed out")
	}
}

func TestSentinel_FileModify(t *testing.T) {
	bus := NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sentinel := NewSentinel(bus, logger)
	dir := t.TempDir()

	// Create a file before watching.
	testFile := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := sentinel.Watch(dir); err != nil {
		t.Fatalf("watch: %v", err)
	}

	sub := bus.Subscribe("test-sentinel-modify", nil)
	defer bus.Unsubscribe("test-sentinel-modify")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sentinel.Run(ctx)

	// Modify the file.
	if err := os.WriteFile(testFile, []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	// Wait for the fs.modify event (or fs.create if the OS fires that).
	found := waitForEventTypes(sub.Ch, []types.EventType{EventFSModify, EventFSCreate}, 3*time.Second)
	if !found {
		t.Error("expected fs.modify or fs.create event, timed out")
	}
}

func TestSentinel_FileDelete(t *testing.T) {
	bus := NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sentinel := NewSentinel(bus, logger)
	dir := t.TempDir()

	// Create a file before watching.
	testFile := filepath.Join(dir, "doomed.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := sentinel.Watch(dir); err != nil {
		t.Fatalf("watch: %v", err)
	}

	sub := bus.Subscribe("test-sentinel-delete", nil)
	defer bus.Unsubscribe("test-sentinel-delete")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sentinel.Run(ctx)

	// Delete the file.
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	// Wait for fs.delete event.
	found := waitForEventType(sub.Ch, EventFSDelete, 3*time.Second)
	if !found {
		t.Error("expected fs.delete event, timed out")
	}
}

func TestSentinel_Unwatch(t *testing.T) {
	bus := NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sentinel := NewSentinel(bus, logger)
	dir := t.TempDir()

	if err := sentinel.Watch(dir); err != nil {
		t.Fatalf("watch: %v", err)
	}

	sentinel.Unwatch(dir)

	// After unwatching, no events should be generated.
	sub := bus.Subscribe("test-sentinel-unwatch", nil)
	defer bus.Unsubscribe("test-sentinel-unwatch")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sentinel.Run(ctx)

	testFile := filepath.Join(dir, "ignored.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// We should NOT get any event. Wait a short time to confirm.
	select {
	case e := <-sub.Ch:
		t.Errorf("unexpected event after unwatch: %s", e.Type)
	case <-time.After(500 * time.Millisecond):
		// expected: no event
	}
}

func TestSentinel_RunWithoutWatch(t *testing.T) {
	bus := NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sentinel := NewSentinel(bus, logger)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sentinel.Run(ctx)
		close(done)
	}()

	// Cancel should cause Run to return.
	cancel()

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

// waitForEventType reads events from ch and returns true if an event of the
// given type arrives within the timeout.
func waitForEventType(ch <-chan types.NervousEvent, target types.EventType, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return false
			}
			if e.Type == target {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// waitForEventTypes reads events from ch and returns true if any event of
// the given types arrives within the timeout.
func waitForEventTypes(ch <-chan types.NervousEvent, targets []types.EventType, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	targetSet := make(map[types.EventType]struct{}, len(targets))
	for _, t := range targets {
		targetSet[t] = struct{}{}
	}

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return false
			}
			if _, match := targetSet[e.Type]; match {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}
