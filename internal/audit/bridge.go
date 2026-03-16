package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// PluginWriter is a callback that forwards an audit event to a plugin's
// write_audit_event MCP tool. The function should be non-blocking and return
// quickly. It is safe for concurrent use.
type PluginWriter func(ctx context.Context, event *AuditEvent) error

// AuditEvent is the payload forwarded to audit plugins via the bridge.
type AuditEvent struct {
	EventType  string          `json:"event_type"`
	Source     string          `json:"source"`
	Scope      string          `json:"scope,omitempty"`
	TraceID    string          `json:"trace_id,omitempty"`
	SequenceID uint64          `json:"sequence_id"`
	Timestamp  string          `json:"timestamp"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// PluginAuditSink subscribes to comm.* events on the EventBus and fans them
// out to registered audit plugin writers. It is non-blocking: events are
// buffered in a channel and dropped if the buffer is full.
type PluginAuditSink struct {
	bus    *nervous.EventBus
	logger *slog.Logger

	mu      sync.RWMutex
	writers map[string]PluginWriter // keyed by plugin name

	eventCh chan types.NervousEvent
	bufSize int
}

// NewPluginAuditSink creates an audit sink that forwards comm.* events to
// registered plugin writers. bufSize controls the internal event channel
// buffer; events are dropped if the buffer is full (non-blocking).
func NewPluginAuditSink(bus *nervous.EventBus, logger *slog.Logger, bufSize int) *PluginAuditSink {
	if bufSize <= 0 {
		bufSize = 1000
	}
	return &PluginAuditSink{
		bus:     bus,
		logger:  logger.With("component", "audit-plugin-sink"),
		writers: make(map[string]PluginWriter),
		eventCh: make(chan types.NervousEvent, bufSize),
		bufSize: bufSize,
	}
}

// RegisterWriter adds a plugin audit writer. Safe for concurrent use.
func (s *PluginAuditSink) RegisterWriter(name string, w PluginWriter) {
	s.mu.Lock()
	s.writers[name] = w
	s.mu.Unlock()
	s.logger.Info("audit writer registered", "plugin", name)
}

// UnregisterWriter removes a plugin audit writer. Safe for concurrent use.
func (s *PluginAuditSink) UnregisterWriter(name string) {
	s.mu.Lock()
	delete(s.writers, name)
	s.mu.Unlock()
	s.logger.Info("audit writer unregistered", "plugin", name)
}

// WriterCount returns the number of registered writers.
func (s *PluginAuditSink) WriterCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.writers)
}

// Run subscribes to comm.* events on the EventBus and drains them to
// registered plugin writers. Blocks until ctx is cancelled.
func (s *PluginAuditSink) Run(ctx context.Context) {
	// Subscribe only to comm.* events — the user's requirement is that only
	// events transitioning through the CommHub are captured.
	sub := s.bus.Subscribe("audit-plugin-sink", func(e types.NervousEvent) bool {
		return strings.HasPrefix(string(e.Type), "comm.")
	})

	defer s.bus.Unsubscribe("audit-plugin-sink")

	// Fan events into the buffered channel for the drain goroutine.
	drainCtx, drainCancel := context.WithCancel(ctx)
	defer drainCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.drain(drainCtx)
	}()

	for {
		select {
		case <-ctx.Done():
			drainCancel()
			wg.Wait()
			return

		case event, ok := <-sub.Ch:
			if !ok {
				drainCancel()
				wg.Wait()
				return
			}
			// Non-blocking send — drop if buffer full.
			select {
			case s.eventCh <- event:
			default:
				s.logger.Warn("audit-plugin-sink: event dropped (buffer full)",
					"event_type", event.Type,
					"sequence_id", event.SequenceID,
				)
				if s.bus != nil {
					s.bus.Publish(nervous.NewEvent(types.EventAuditDropped, "audit-plugin-sink", "", map[string]any{
						"event_type":  string(event.Type),
						"sequence_id": event.SequenceID,
					}))
				}
			}
		}
	}
}

// drain reads events from the buffered channel and forwards them to all
// registered writers concurrently.
func (s *PluginAuditSink) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.eventCh:
			if !ok {
				return
			}
			s.forward(ctx, event)
		}
	}
}

// forward sends an event to all registered writers.
func (s *PluginAuditSink) forward(ctx context.Context, event types.NervousEvent) {
	auditEvent := &AuditEvent{
		EventType:  string(event.Type),
		Source:     event.Source,
		Scope:      event.Scope,
		TraceID:    event.TraceID,
		SequenceID: event.SequenceID,
		Timestamp:  event.Timestamp.Format(time.RFC3339Nano),
		Payload:    event.Payload,
	}

	s.mu.RLock()
	writers := make(map[string]PluginWriter, len(s.writers))
	for k, v := range s.writers {
		writers[k] = v
	}
	s.mu.RUnlock()

	for name, w := range writers {
		if err := w(ctx, auditEvent); err != nil {
			s.logger.Error("audit-plugin-sink: writer error",
				"plugin", name,
				"event_type", auditEvent.EventType,
				"error", err,
			)
		}
	}
}
