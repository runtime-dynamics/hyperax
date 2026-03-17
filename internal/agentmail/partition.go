package agentmail

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// PartitionManager enforces workspace-scoped access controls based on partition locks.
// When a workspace is partitioned (e.g. due to ack deadline expiry), the manager
// blocks outbound mail for that workspace until the partition is resolved.
//
// It integrates with the AckTracker for automatic partition detection and with the
// Nervous System EventBus for partition lifecycle events.
type PartitionManager struct {
	mu    sync.RWMutex
	locks map[string]*types.PartitionLock // workspace_id -> lock

	bus    *nervous.EventBus
	logger *slog.Logger

	// defaultLockTTL is the duration a partition lock remains active.
	// After this duration, the lock expires and the workspace is automatically unblocked.
	defaultLockTTL time.Duration

	// onLockCallback is invoked when a new lock is acquired (for testing).
	onLockCallback func(lock *types.PartitionLock)
}

// NewPartitionManager creates a PartitionManager.
func NewPartitionManager(bus *nervous.EventBus, logger *slog.Logger) *PartitionManager {
	return &PartitionManager{
		locks:          make(map[string]*types.PartitionLock),
		bus:            bus,
		logger:         logger.With("component", "partition_manager"),
		defaultLockTTL: 10 * time.Minute,
	}
}

// SetDefaultLockTTL overrides the default partition lock time-to-live.
func (pm *PartitionManager) SetDefaultLockTTL(ttl time.Duration) {
	pm.defaultLockTTL = ttl
}

// AcquireLock creates a workspace-scoped partition lock.
// If a lock already exists for the workspace, it is replaced.
// Returns the created lock.
func (pm *PartitionManager) AcquireLock(workspaceID, instanceID, reason string) *types.PartitionLock {
	now := time.Now()
	lock := &types.PartitionLock{
		WorkspaceID: workspaceID,
		InstanceID:  instanceID,
		Reason:      reason,
		LockedAt:    now,
		ExpiresAt:   now.Add(pm.defaultLockTTL),
	}

	pm.mu.Lock()
	pm.locks[workspaceID] = lock
	pm.mu.Unlock()

	pm.logger.Warn("partition lock acquired",
		"workspace_id", workspaceID,
		"instance_id", instanceID,
		"reason", reason,
		"expires_at", lock.ExpiresAt,
	)

	if pm.bus != nil {
		pm.bus.Publish(nervous.NewEvent(
			types.EventAgentMailPartitionDetected,
			"partition_manager",
			workspaceID,
			map[string]string{
				"workspace_id": workspaceID,
				"instance_id":  instanceID,
				"reason":       reason,
			},
		))
	}

	if pm.onLockCallback != nil {
		pm.onLockCallback(lock)
	}

	return lock
}

// ReleaseLock removes a partition lock for the given workspace.
// Returns an error if no lock exists.
func (pm *PartitionManager) ReleaseLock(workspaceID string) error {
	pm.mu.Lock()
	lock, ok := pm.locks[workspaceID]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("no partition lock for workspace %q", workspaceID)
	}
	delete(pm.locks, workspaceID)
	pm.mu.Unlock()

	pm.logger.Info("partition lock released",
		"workspace_id", workspaceID,
		"instance_id", lock.InstanceID,
	)

	if pm.bus != nil {
		pm.bus.Publish(nervous.NewEvent(
			types.EventAgentMailPartitionResolved,
			"partition_manager",
			workspaceID,
			map[string]string{
				"workspace_id": workspaceID,
				"instance_id":  lock.InstanceID,
			},
		))
	}

	return nil
}

// IsLocked returns true if the workspace currently has an active (non-expired) partition lock.
func (pm *PartitionManager) IsLocked(workspaceID string) bool {
	pm.mu.RLock()
	lock, ok := pm.locks[workspaceID]
	pm.mu.RUnlock()

	if !ok {
		return false
	}

	// Check expiry.
	if time.Now().After(lock.ExpiresAt) {
		// Auto-release expired locks.
		if err := pm.ReleaseLock(workspaceID); err != nil {
			pm.logger.Debug("auto-release expired lock failed (likely already released)",
				"workspace_id", workspaceID, "error", err)
		}
		return false
	}

	return true
}

// GetLock returns the active partition lock for a workspace, or nil.
func (pm *PartitionManager) GetLock(workspaceID string) *types.PartitionLock {
	pm.mu.RLock()
	lock, ok := pm.locks[workspaceID]
	pm.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(lock.ExpiresAt) {
		if err := pm.ReleaseLock(workspaceID); err != nil {
			pm.logger.Debug("auto-release expired lock failed (likely already released)",
				"workspace_id", workspaceID, "error", err)
		}
		return nil
	}

	return lock
}

// AllLocks returns a snapshot of all active (non-expired) partition locks.
func (pm *PartitionManager) AllLocks() map[string]*types.PartitionLock {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	now := time.Now()
	result := make(map[string]*types.PartitionLock, len(pm.locks))
	for k, v := range pm.locks {
		if now.Before(v.ExpiresAt) {
			result[k] = v
		}
	}
	return result
}

// CheckSendAllowed verifies that a mail message can be sent from the given workspace.
// Returns an error if the workspace is partitioned.
func (pm *PartitionManager) CheckSendAllowed(workspaceID string) error {
	if workspaceID == "" {
		return nil // unscoped messages are always allowed
	}
	if pm.IsLocked(workspaceID) {
		lock := pm.GetLock(workspaceID)
		if lock != nil {
			return fmt.Errorf("workspace %q is partitioned: %s (locked since %s, expires %s)",
				workspaceID, lock.Reason,
				lock.LockedAt.Format(time.RFC3339),
				lock.ExpiresAt.Format(time.RFC3339))
		}
	}
	return nil
}

// ExpireStale removes all expired partition locks.
// Called periodically by the partition monitor or manually for cleanup.
func (pm *PartitionManager) ExpireStale() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	expired := 0
	for wid, lock := range pm.locks {
		if now.After(lock.ExpiresAt) {
			delete(pm.locks, wid)
			expired++
			pm.logger.Info("partition lock expired",
				"workspace_id", wid,
				"instance_id", lock.InstanceID,
			)

			if pm.bus != nil {
				pm.bus.Publish(nervous.NewEvent(
					types.EventAgentMailPartitionResolved,
					"partition_manager",
					wid,
					map[string]string{
						"workspace_id": wid,
						"instance_id":  lock.InstanceID,
						"reason":       "expired",
					},
				))
			}
		}
	}
	return expired
}

// Run starts a background loop that periodically checks for expired partition locks.
// It blocks until the context is cancelled.
func (pm *PartitionManager) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := pm.ExpireStale(); n > 0 {
				pm.logger.Info("expired partition locks", "count", n)
			}
		}
	}
}

// Count returns the number of active partition locks.
func (pm *PartitionManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.locks)
}
