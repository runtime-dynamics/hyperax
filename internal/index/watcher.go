// Package index — IndexWatcher subscribes to fs.* events from the Sentinel via
// the EventBus, debounces rapid file changes, resolves workspace context, and
// dispatches incremental index or remove operations to the Indexer.
package index

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fmt"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// debounceDuration controls how long the watcher waits after the last event
// for a given path before dispatching an index/remove operation.
const debounceDuration = 500 * time.Millisecond

// dispatchChanSize is the buffer size for the internal dispatch channel.
const dispatchChanSize = 256

// WorkspaceStore is the subset of the workspace repository that the
// IndexWatcher needs. Kept narrow to avoid coupling to the full repo.
type WorkspaceStore interface {
	ListWorkspaces(ctx context.Context) ([]*types.WorkspaceInfo, error)
}

// dispatchMsg carries a debounced file event to the processing goroutine.
type dispatchMsg struct {
	absPath   string
	eventType types.EventType
}

// IndexWatcher subscribes to fs.* events from the Sentinel via the EventBus,
// debounces rapid file changes, resolves the workspace context, and dispatches
// incremental index or remove operations to the Indexer.
type IndexWatcher struct {
	indexer  *Indexer
	bus      *nervous.EventBus
	sentinel *nervous.Sentinel
	store    WorkspaceStore
	logger   *slog.Logger

	mu       sync.Mutex
	timers   map[string]*time.Timer
	pending  map[string]types.EventType
	dispatch chan dispatchMsg

	wsCache   []*types.WorkspaceInfo
	wsCacheMu sync.RWMutex
}

// NewIndexWatcher creates an IndexWatcher with the provided dependencies.
//
// Parameters:
//   - indexer:  the Indexer used for IndexFile and RemoveFile operations
//   - bus:      event bus for subscribing to fs events and publishing index events
//   - sentinel: the Sentinel, used to add new watch paths
//   - store:    workspace store for resolving absolute paths to workspace context
//   - logger:   structured logger (may be nil; a no-op logger is substituted)
func NewIndexWatcher(indexer *Indexer, bus *nervous.EventBus, sentinel *nervous.Sentinel, store WorkspaceStore, logger *slog.Logger) *IndexWatcher {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(nil, nil))
	}
	return &IndexWatcher{
		indexer:  indexer,
		bus:      bus,
		sentinel: sentinel,
		store:    store,
		logger:   logger,
		timers:   make(map[string]*time.Timer),
		pending:  make(map[string]types.EventType),
		dispatch: make(chan dispatchMsg, dispatchChanSize),
	}
}

// Run starts the IndexWatcher event loop. It refreshes the workspace cache,
// subscribes to filesystem events, and processes debounced dispatch messages.
// It blocks until ctx is cancelled.
func (w *IndexWatcher) Run(ctx context.Context) {
	if err := w.RefreshWorkspaces(ctx); err != nil {
		w.logger.Warn("index watcher: initial workspace refresh failed", "error", err)
	}

	sub := w.bus.SubscribeTypes("index.watcher",
		nervous.EventFSCreate,
		nervous.EventFSModify,
		nervous.EventFSDelete,
		nervous.EventFSRename,
	)

	w.logger.Info("index watcher started")

	defer func() {
		w.bus.Unsubscribe(sub.ID)
		w.cleanupTimers()
		w.logger.Info("index watcher stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-sub.Ch:
			if !ok {
				return
			}
			w.handleEvent(event)

		case msg := <-w.dispatch:
			w.processDispatch(ctx, msg)
		}
	}
}

// fsPayload mirrors the JSON payload emitted by the Sentinel for fs events.
type fsPayload struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// handleEvent parses the filesystem event payload, filters by extension,
// and passes eligible events to the debouncer.
func (w *IndexWatcher) handleEvent(event types.NervousEvent) {
	var payload fsPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		w.logger.Warn("index watcher: failed to parse fs event payload",
			"error", err,
			"event_type", event.Type,
		)
		return
	}

	if payload.Path == "" {
		return
	}

	// For delete/rename events we always process (the file is gone, so we
	// cannot check extension on disk). For create/modify we filter.
	if event.Type == nervous.EventFSCreate || event.Type == nervous.EventFSModify {
		if !isIndexable(payload.Path) {
			return
		}
	}

	w.debounce(payload.Path, event.Type)
}

// debounce resets or creates a 500ms timer for the given path. When the timer
// fires, a dispatchMsg is sent to the dispatch channel for processing. If
// multiple events arrive for the same path within the window, only the last
// event type is dispatched.
func (w *IndexWatcher) debounce(absPath string, eventType types.EventType) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If a timer already exists for this path, stop it before resetting.
	if t, ok := w.timers[absPath]; ok {
		t.Stop()
	}

	// Always record the latest event type for this path.
	w.pending[absPath] = eventType

	w.timers[absPath] = time.AfterFunc(debounceDuration, func() {
		w.mu.Lock()
		et, ok := w.pending[absPath]
		delete(w.pending, absPath)
		delete(w.timers, absPath)
		w.mu.Unlock()

		if !ok {
			return
		}

		select {
		case w.dispatch <- dispatchMsg{absPath: absPath, eventType: et}:
		default:
			w.logger.Warn("index watcher: dispatch channel full, dropping event",
				"path", absPath,
				"event_type", et,
			)
		}
	})
}

// processDispatch resolves the workspace for the given absolute path and
// dispatches the appropriate Indexer operation.
func (w *IndexWatcher) processDispatch(ctx context.Context, msg dispatchMsg) {
	wsID, rootPath, relPath, ok := w.resolveWorkspace(msg.absPath)
	if !ok {
		w.logger.Debug("index watcher: no workspace matched for path",
			"path", msg.absPath,
		)
		return
	}

	switch msg.eventType {
	case nervous.EventFSDelete, nervous.EventFSRename:
		if err := w.indexer.RemoveFile(ctx, wsID, relPath); err != nil {
			w.logger.Warn("index watcher: remove file failed",
				"workspace_id", wsID,
				"path", relPath,
				"error", err,
			)
			return
		}
		w.logger.Debug("index watcher: file removed from index",
			"workspace_id", wsID,
			"path", relPath,
		)
		w.bus.Publish(nervous.NewEvent(
			types.EventIndexFileRemoved,
			"index.watcher",
			wsID,
			map[string]string{
				"workspace_id": wsID,
				"path":         relPath,
			},
		))

	case nervous.EventFSCreate, nervous.EventFSModify:
		if err := w.indexer.IndexFile(ctx, wsID, rootPath, relPath); err != nil {
			w.logger.Warn("index watcher: index file failed",
				"workspace_id", wsID,
				"path", relPath,
				"error", err,
			)
			return
		}
		w.logger.Debug("index watcher: file reindexed",
			"workspace_id", wsID,
			"path", relPath,
		)
		w.bus.Publish(nervous.NewEvent(
			types.EventIndexFileReindexed,
			"index.watcher",
			wsID,
			map[string]string{
				"workspace_id": wsID,
				"path":         relPath,
			},
		))
	}
}

// resolveWorkspace finds the workspace whose RootPath is a prefix of the given
// absolute path. When multiple workspaces match (nested roots), the longest
// (most specific) match wins.
//
// Returns the workspace ID, root path, relative path within the workspace, and
// a boolean indicating whether a match was found.
func (w *IndexWatcher) resolveWorkspace(absPath string) (wsID, rootPath, relPath string, ok bool) {
	w.wsCacheMu.RLock()
	cache := w.wsCache
	w.wsCacheMu.RUnlock()

	var bestWS *types.WorkspaceInfo

	for _, ws := range cache {
		root := ws.RootPath
		// Ensure the prefix check uses a trailing separator so that
		// "/home/user/project" doesn't match "/home/user/project2".
		if !strings.HasSuffix(root, string(filepath.Separator)) {
			root += string(filepath.Separator)
		}
		if !strings.HasPrefix(absPath, root) && absPath != ws.RootPath {
			continue
		}
		// Pick the longest (most specific) match.
		if bestWS == nil || len(ws.RootPath) > len(bestWS.RootPath) {
			bestWS = ws
		}
	}

	if bestWS == nil {
		return "", "", "", false
	}

	rel, err := filepath.Rel(bestWS.RootPath, absPath)
	if err != nil {
		return "", "", "", false
	}

	// Safety: reject paths that escape the workspace root.
	if strings.HasPrefix(rel, "..") {
		return "", "", "", false
	}

	return bestWS.ID, bestWS.RootPath, rel, true
}

// RefreshWorkspaces reloads the workspace list from the store and updates the
// internal cache. This is safe to call concurrently from any goroutine.
func (w *IndexWatcher) RefreshWorkspaces(ctx context.Context) error {
	workspaces, err := w.store.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("index.IndexWatcher.RefreshWorkspaces: %w", err)
	}

	w.wsCacheMu.Lock()
	w.wsCache = workspaces
	w.wsCacheMu.Unlock()

	return nil
}

// AddWorkspace adds a new root path to the Sentinel watch set and refreshes
// the workspace cache so subsequent events for this path are resolved.
func (w *IndexWatcher) AddWorkspace(ctx context.Context, rootPath string) error {
	if err := w.sentinel.Watch(rootPath); err != nil {
		return fmt.Errorf("index.IndexWatcher.AddWorkspace: %w", err)
	}
	return w.RefreshWorkspaces(ctx)
}

// isIndexable returns true if the given absolute path refers to a file that the
// indexer can process. It checks both file extension and path segments.
func isIndexable(absPath string) bool {
	// Reject paths that pass through ignored directories.
	for _, segment := range strings.Split(absPath, string(filepath.Separator)) {
		if _, ignored := ignoredDirs[segment]; ignored {
			return false
		}
		// Hidden directories (except root ".").
		if strings.HasPrefix(segment, ".") && segment != "." && segment != "" {
			return false
		}
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if ext == "" {
		return false
	}

	// Markdown files are always indexable (doc chunking).
	if ext == ".md" {
		return true
	}

	// Check against the set of source extensions supported by the indexer.
	if ext == ".go" {
		return true
	}
	if _, supported := SupportedExtensions()[ext]; supported {
		return true
	}

	return false
}

// cleanupTimers stops all pending debounce timers. Called during shutdown.
func (w *IndexWatcher) cleanupTimers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for path, t := range w.timers {
		t.Stop()
		delete(w.timers, path)
		delete(w.pending, path)
	}
}
