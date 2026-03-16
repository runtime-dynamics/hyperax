package nervous

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// EventFSCreate is emitted when a file is created.
	EventFSCreate types.EventType = "fs.create"
	// EventFSModify is emitted when a file is modified.
	EventFSModify types.EventType = "fs.modify"
	// EventFSDelete is emitted when a file is deleted.
	EventFSDelete types.EventType = "fs.delete"
	// EventFSRename is emitted when a file is renamed.
	EventFSRename types.EventType = "fs.rename"
)

// Sentinel bridges fsnotify file system events to the EventBus.
// It watches directories (recursively) and publishes NervousEvents
// for file create, modify, delete, and rename operations.
type Sentinel struct {
	bus     *EventBus
	logger  *slog.Logger
	watcher *fsnotify.Watcher

	mu      sync.Mutex
	watched map[string]struct{}
}

// NewSentinel creates a Sentinel that publishes file events to the given bus.
func NewSentinel(bus *EventBus, logger *slog.Logger) *Sentinel {
	return &Sentinel{
		bus:     bus,
		logger:  logger,
		watched: make(map[string]struct{}),
	}
}

// Watch adds a directory (and all its subdirectories) to the watch set.
// The fsnotify watcher is lazily initialized on first call.
func (s *Sentinel) Watch(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watcher == nil {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("nervous.Sentinel.Watch: %w", err)
		}
		s.watcher = w
	}

	return s.addRecursive(path)
}

// Unwatch removes a directory from the watch set.
func (s *Sentinel) Unwatch(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watcher == nil {
		return
	}

	_ = s.watcher.Remove(path)
	delete(s.watched, path)
}

// Run starts the event loop reading fsnotify events and publishing them
// as NervousEvents. It blocks until the context is cancelled.
func (s *Sentinel) Run(ctx context.Context) {
	s.mu.Lock()
	w := s.watcher
	s.mu.Unlock()

	if w == nil {
		s.logger.Warn("sentinel: no watcher initialized, Run returning immediately")
		<-ctx.Done()
		return
	}

	defer func() {
		_ = w.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.Events:
			if !ok {
				return
			}
			s.handleFSEvent(event)

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			s.logger.Error("sentinel: watcher error", "error", err)
		}
	}
}

// handleFSEvent translates a single fsnotify event into a NervousEvent
// and publishes it on the EventBus.
func (s *Sentinel) handleFSEvent(event fsnotify.Event) {
	var eventType types.EventType

	switch {
	case event.Has(fsnotify.Create):
		eventType = EventFSCreate
		// If a new directory was created, add it to the watch list.
		s.maybeWatchNewDir(event.Name)
	case event.Has(fsnotify.Write):
		eventType = EventFSModify
	case event.Has(fsnotify.Remove):
		eventType = EventFSDelete
	case event.Has(fsnotify.Rename):
		eventType = EventFSRename
	default:
		return // chmod or unknown, skip
	}

	payload := map[string]string{
		"path": event.Name,
		"op":   event.Op.String(),
	}

	s.bus.Publish(NewEvent(eventType, "sentinel", "filesystem", payload))
}

// maybeWatchNewDir checks if the newly created path is a directory and
// adds it to the watcher if so.
func (s *Sentinel) maybeWatchNewDir(path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.addRecursive(path); err != nil {
		s.logger.Warn("sentinel: failed to watch new directory",
			"path", path, "error", err)
	}
}

// addRecursive walks the given path and adds all directories to the
// watcher. Caller must hold s.mu.
func (s *Sentinel) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("nervous.Sentinel.addRecursive: walk error: %w", err)
		}
		if !d.IsDir() {
			return nil
		}
		if _, already := s.watched[path]; already {
			return nil
		}

		if err := s.watcher.Add(path); err != nil {
			return fmt.Errorf("nervous.Sentinel.addRecursive: add watch: %w", err)
		}
		s.watched[path] = struct{}{}
		return nil
	})
}
