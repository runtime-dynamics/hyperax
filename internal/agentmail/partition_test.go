package agentmail

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func newTestPartitionManager() *PartitionManager {
	return NewPartitionManager(nil, testPartitionLogger())
}

func testPartitionLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPartitionManager_AcquireRelease(t *testing.T) {
	pm := newTestPartitionManager()

	lock := pm.AcquireLock("ws-1", "instance-a", "test partition")
	if lock == nil {
		t.Fatal("expected lock")
	}
	if lock.WorkspaceID != "ws-1" {
		t.Fatalf("expected workspace 'ws-1', got %q", lock.WorkspaceID)
	}
	if lock.InstanceID != "instance-a" {
		t.Fatalf("expected instance 'instance-a', got %q", lock.InstanceID)
	}
	if !pm.IsLocked("ws-1") {
		t.Fatal("workspace should be locked")
	}
	if pm.Count() != 1 {
		t.Fatalf("expected count 1, got %d", pm.Count())
	}

	if err := pm.ReleaseLock("ws-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if pm.IsLocked("ws-1") {
		t.Fatal("workspace should not be locked after release")
	}
	if pm.Count() != 0 {
		t.Fatalf("expected count 0, got %d", pm.Count())
	}
}

func TestPartitionManager_ReleaseNonexistent(t *testing.T) {
	pm := newTestPartitionManager()
	err := pm.ReleaseLock("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent lock")
	}
}

func TestPartitionManager_GetLock(t *testing.T) {
	pm := newTestPartitionManager()
	pm.AcquireLock("ws-1", "inst-1", "reason")

	lock := pm.GetLock("ws-1")
	if lock == nil {
		t.Fatal("expected lock")
	}
	if lock.Reason != "reason" {
		t.Fatalf("expected reason 'reason', got %q", lock.Reason)
	}

	if lock := pm.GetLock("ws-2"); lock != nil {
		t.Fatal("expected nil for nonexistent workspace")
	}
}

func TestPartitionManager_AllLocks(t *testing.T) {
	pm := newTestPartitionManager()
	pm.AcquireLock("ws-1", "inst-1", "r1")
	pm.AcquireLock("ws-2", "inst-2", "r2")

	locks := pm.AllLocks()
	if len(locks) != 2 {
		t.Fatalf("expected 2 locks, got %d", len(locks))
	}
}

func TestPartitionManager_CheckSendAllowed_Unlocked(t *testing.T) {
	pm := newTestPartitionManager()
	if err := pm.CheckSendAllowed("ws-1"); err != nil {
		t.Fatalf("expected send allowed for unlocked workspace: %v", err)
	}
}

func TestPartitionManager_CheckSendAllowed_EmptyWorkspace(t *testing.T) {
	pm := newTestPartitionManager()
	pm.AcquireLock("ws-1", "inst-1", "blocked")
	// Empty workspace should always be allowed.
	if err := pm.CheckSendAllowed(""); err != nil {
		t.Fatalf("expected send allowed for empty workspace: %v", err)
	}
}

func TestPartitionManager_CheckSendAllowed_Locked(t *testing.T) {
	pm := newTestPartitionManager()
	pm.AcquireLock("ws-1", "inst-1", "ack expired")

	err := pm.CheckSendAllowed("ws-1")
	if err == nil {
		t.Fatal("expected error for partitioned workspace")
	}
	if !strings.Contains(err.Error(), "partitioned") {
		t.Fatalf("expected 'partitioned' in error, got %q", err.Error())
	}
}

func TestPartitionManager_LockExpiry(t *testing.T) {
	pm := newTestPartitionManager()
	pm.SetDefaultLockTTL(1 * time.Millisecond) // very short TTL

	pm.AcquireLock("ws-1", "inst-1", "expires fast")
	time.Sleep(5 * time.Millisecond) // wait for expiry

	if pm.IsLocked("ws-1") {
		t.Fatal("lock should have expired")
	}
}

func TestPartitionManager_ExpireStale(t *testing.T) {
	pm := newTestPartitionManager()
	pm.SetDefaultLockTTL(1 * time.Millisecond)

	pm.AcquireLock("ws-1", "inst-1", "r1")
	pm.AcquireLock("ws-2", "inst-2", "r2")
	time.Sleep(5 * time.Millisecond)

	n := pm.ExpireStale()
	if n != 2 {
		t.Fatalf("expected 2 expired, got %d", n)
	}
	if pm.Count() != 0 {
		t.Fatalf("expected 0 locks after expiry, got %d", pm.Count())
	}
}

func TestPartitionManager_AcquireReplace(t *testing.T) {
	pm := newTestPartitionManager()
	pm.AcquireLock("ws-1", "inst-1", "first")
	pm.AcquireLock("ws-1", "inst-2", "second")

	lock := pm.GetLock("ws-1")
	if lock == nil {
		t.Fatal("expected lock")
	}
	if lock.InstanceID != "inst-2" {
		t.Fatalf("expected replaced lock with inst-2, got %q", lock.InstanceID)
	}
	if lock.Reason != "second" {
		t.Fatalf("expected reason 'second', got %q", lock.Reason)
	}
	if pm.Count() != 1 {
		t.Fatalf("expected count 1 after replace, got %d", pm.Count())
	}
}

func TestPartitionManager_Callback(t *testing.T) {
	pm := newTestPartitionManager()
	var captured *types.PartitionLock
	pm.onLockCallback = func(lock *types.PartitionLock) {
		captured = lock
	}

	pm.AcquireLock("ws-1", "inst-1", "callback test")
	if captured == nil {
		t.Fatal("expected callback to be invoked")
	}
	if captured.WorkspaceID != "ws-1" {
		t.Fatalf("expected workspace 'ws-1' in callback, got %q", captured.WorkspaceID)
	}
}

func TestPartitionManager_RunCancellation(t *testing.T) {
	pm := newTestPartitionManager()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pm.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestPartitionManager_DefaultTTL(t *testing.T) {
	pm := newTestPartitionManager()
	lock := pm.AcquireLock("ws-1", "inst-1", "check ttl")

	expectedDuration := 10 * time.Minute
	actualDuration := lock.ExpiresAt.Sub(lock.LockedAt)
	if actualDuration < expectedDuration-time.Second || actualDuration > expectedDuration+time.Second {
		t.Fatalf("expected ~10 minute TTL, got %v", actualDuration)
	}
}

func TestPartitionManager_IsLocked_NotFound(t *testing.T) {
	pm := newTestPartitionManager()
	if pm.IsLocked("nonexistent") {
		t.Fatal("expected false for nonexistent workspace")
	}
}
