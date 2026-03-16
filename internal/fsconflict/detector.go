// Package fsconflict detects when external filesystem changes affect files
// that are currently tracked by active refactoring transactions. On conflict,
// it publishes an EventFSConflictDetected event on the EventBus.
package fsconflict

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConflictDetector subscribes to fs.* events from the EventBus and checks
// whether any modified file is currently tracked by an active refactoring
// transaction. If so, it publishes a conflict event.
type ConflictDetector struct {
	bus    *nervous.EventBus
	txMgr  *refactor.TransactionManager
	logger *slog.Logger
}

// NewConflictDetector creates a ConflictDetector with the given dependencies.
func NewConflictDetector(bus *nervous.EventBus, txMgr *refactor.TransactionManager, logger *slog.Logger) *ConflictDetector {
	return &ConflictDetector{
		bus:    bus,
		txMgr:  txMgr,
		logger: logger,
	}
}

// Start subscribes to filesystem events and runs the conflict detection loop.
// It blocks until ctx is cancelled.
func (cd *ConflictDetector) Start(ctx context.Context) {
	sub := cd.bus.Subscribe("fsconflict.detector", func(e types.NervousEvent) bool {
		return strings.HasPrefix(string(e.Type), "fs.")
	})
	defer cd.bus.Unsubscribe(sub.ID)

	cd.logger.Info("fs conflict detector started")

	for {
		select {
		case <-ctx.Done():
			cd.logger.Info("fs conflict detector stopped")
			return
		case event, ok := <-sub.Ch:
			if !ok {
				return
			}
			cd.handleEvent(event)
		}
	}
}

// fsPayload represents the JSON payload emitted by the Sentinel for fs events.
type fsPayload struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// handleEvent checks whether the filesystem event's path collides with any
// active refactoring transaction. Only modify, delete, and rename operations
// are considered conflict-worthy (creates cannot conflict with existing files).
func (cd *ConflictDetector) handleEvent(event types.NervousEvent) {
	// Only modify/delete/rename can conflict; creates are not tracked.
	switch event.Type {
	case nervous.EventFSModify, nervous.EventFSDelete, nervous.EventFSRename:
		// proceed
	default:
		return
	}

	var payload fsPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		cd.logger.Warn("fs conflict detector: failed to parse event payload",
			"error", err,
			"event_type", event.Type,
		)
		return
	}

	if payload.Path == "" {
		return
	}

	activeFiles := cd.txMgr.ActiveFiles()
	txID, tracked := activeFiles[payload.Path]
	if !tracked {
		return
	}

	now := time.Now().UTC()

	cd.logger.Warn("fs conflict detected: external modification to tracked file",
		"file_path", payload.Path,
		"transaction_id", txID,
		"fs_operation", payload.Op,
		"detected_at", now,
	)

	cd.bus.Publish(nervous.NewEvent(
		types.EventFSConflictDetected,
		"fsconflict.detector",
		"global",
		map[string]any{
			"file_path":      payload.Path,
			"transaction_id": txID,
			"fs_operation":   payload.Op,
			"detected_at":    now.Format(time.RFC3339Nano),
		},
	))
}
