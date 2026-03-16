package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// JSONLRecord is the schema for each line written to the audit JSONL file.
type JSONLRecord struct {
	ExportedAt time.Time        `json:"exported_at"`
	EventType  string           `json:"event_type"`
	Source     string           `json:"source"`
	Scope      string           `json:"scope"`
	Payload    json.RawMessage  `json:"payload"`
	SequenceID uint64           `json:"sequence_id"`
	TraceID    string           `json:"trace_id,omitempty"`
	Timestamp  time.Time        `json:"timestamp"`
}

// Option configures a JSONLSink.
type Option func(*JSONLSink)

// WithMaxSizeMB sets the maximum file size in megabytes before rotation.
// A value of 0 (the default) means no rotation.
func WithMaxSizeMB(mb int64) Option {
	return func(s *JSONLSink) {
		s.maxSizeMB = mb
	}
}

// WithEventFilters sets glob patterns for event types to include.
// An empty slice (the default) means all events are exported.
// Patterns follow the same semantics as nervous.MatchEventType:
//   - "*"           matches everything
//   - "pipeline.*"  matches any event starting with "pipeline."
//   - "*.completed" matches any event ending with ".completed"
//   - "cron.fire"   exact match
func WithEventFilters(patterns []string) Option {
	return func(s *JSONLSink) {
		s.filters = patterns
	}
}

// JSONLSink subscribes to the EventBus and appends each matching event
// as a single JSON line to an append-only audit file. It provides an
// external audit trail beyond the 7-day SQLite retention of the Persister.
type JSONLSink struct {
	filePath  string
	bus       *nervous.EventBus
	logger    *slog.Logger
	maxSizeMB int64    // rotate when file exceeds this (0 = no limit)
	filters   []string // glob patterns for event types to include (empty = all)

	mu      sync.Mutex
	file    *os.File
	written int64 // bytes written to current file
}

// NewJSONLSink creates a new audit sink that writes JSONL to filePath.
// The sink does not start until Run is called.
func NewJSONLSink(filePath string, bus *nervous.EventBus, logger *slog.Logger, opts ...Option) *JSONLSink {
	s := &JSONLSink{
		filePath: filePath,
		bus:      bus,
		logger:   logger,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Run subscribes to the EventBus and writes matching events to the JSONL
// file. It blocks until ctx is cancelled, then flushes and closes the file.
func (s *JSONLSink) Run(ctx context.Context) {
	f, err := os.OpenFile(s.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		s.logger.Error("audit-sink: failed to open file",
			"path", s.filePath,
			"error", err,
		)
		return
	}

	// Seed written counter from existing file size so rotation works on restart.
	if info, statErr := f.Stat(); statErr == nil {
		s.written = info.Size()
	}

	s.mu.Lock()
	s.file = f
	s.mu.Unlock()

	sub := s.bus.Subscribe("audit-jsonl-sink", nil) // nil filter = all events

	defer func() {
		s.bus.Unsubscribe("audit-jsonl-sink")
		s.mu.Lock()
		if s.file != nil {
			_ = s.file.Close()
			s.file = nil
		}
		s.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-sub.Ch:
			if !ok {
				return
			}
			if s.matchesFilters(event) {
				s.writeEvent(event)
			}
		}
	}
}

// matchesFilters returns true if the event should be exported.
// When no filters are configured, all events match.
func (s *JSONLSink) matchesFilters(event types.NervousEvent) bool {
	if len(s.filters) == 0 {
		return true
	}
	for _, pattern := range s.filters {
		if nervous.MatchEventType(pattern, event.Type) {
			return true
		}
	}
	return false
}

// writeEvent marshals the event as a JSONLRecord and appends it to the file.
func (s *JSONLSink) writeEvent(event types.NervousEvent) {
	record := JSONLRecord{
		ExportedAt: time.Now(),
		EventType:  string(event.Type),
		Source:     event.Source,
		Scope:      event.Scope,
		Payload:    event.Payload,
		SequenceID: event.SequenceID,
		TraceID:    event.TraceID,
		Timestamp:  event.Timestamp,
	}

	data, err := json.Marshal(record)
	if err != nil {
		s.logger.Error("audit-sink: failed to marshal event",
			"event_type", event.Type,
			"error", err,
		)
		return
	}

	line := append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return
	}

	n, err := s.file.Write(line)
	if err != nil {
		s.logger.Error("audit-sink: failed to write event",
			"event_type", event.Type,
			"error", err,
		)
		return
	}
	s.written += int64(n)

	// Rotate if max size exceeded.
	if s.maxSizeMB > 0 && s.written >= s.maxSizeMB*1024*1024 {
		s.rotateLocked()
	}
}

// rotateLocked closes the current file, renames it with a timestamp suffix,
// and opens a new file at the original path. Must be called with s.mu held.
func (s *JSONLSink) rotateLocked() {
	if s.file == nil {
		return
	}

	_ = s.file.Close()

	rotatedName := fmt.Sprintf("%s.%s.jsonl",
		s.filePath,
		time.Now().UTC().Format("2006-01-02T15-04-05"),
	)

	if err := os.Rename(s.filePath, rotatedName); err != nil {
		s.logger.Error("audit-sink: failed to rotate file",
			"from", s.filePath,
			"to", rotatedName,
			"error", err,
		)
	}

	f, err := os.OpenFile(s.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		s.logger.Error("audit-sink: failed to open new file after rotation",
			"path", s.filePath,
			"error", err,
		)
		s.file = nil
		return
	}

	s.file = f
	s.written = 0
}
