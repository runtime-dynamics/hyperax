package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// publishAndDrain publishes an event and waits briefly for the sink to process it.
func publishAndDrain(bus *nervous.EventBus, event types.NervousEvent) {
	bus.Publish(event)
	time.Sleep(50 * time.Millisecond)
}

func makeEvent(eventType string, source string) types.NervousEvent {
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	return types.NervousEvent{
		Type:      types.EventType(eventType),
		Source:    source,
		Scope:     "test",
		Payload:   payload,
		TraceID:   "trace-001",
		Timestamp: time.Now(),
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("failed to scan %s: %v", path, err)
	}
	return lines
}

func TestJSONLSink_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sink := NewJSONLSink(filePath, bus, logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	// Let the sink subscribe.
	time.Sleep(20 * time.Millisecond)

	publishAndDrain(bus, makeEvent("audit.test", "test-source"))
	publishAndDrain(bus, makeEvent("pipeline.completed", "pipeline-runner"))

	cancel()
	<-done

	lines := readLines(t, filePath)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Verify both lines are valid JSON with expected fields.
	for i, line := range lines {
		var record JSONLRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
			continue
		}
		if record.EventType == "" {
			t.Errorf("line %d: empty event_type", i)
		}
		if record.Source == "" {
			t.Errorf("line %d: empty source", i)
		}
		if record.SequenceID == 0 {
			t.Errorf("line %d: zero sequence_id", i)
		}
	}
}

func TestJSONLSink_EventFiltering(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sink := NewJSONLSink(filePath, bus, logger,
		WithEventFilters([]string{"interject.*", "budget.*"}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	// Publish 3 events: only 2 should match the filters.
	publishAndDrain(bus, makeEvent("interject.created", "source-a"))
	publishAndDrain(bus, makeEvent("pipeline.completed", "source-b")) // no match
	publishAndDrain(bus, makeEvent("budget.exceeded", "source-c"))

	cancel()
	<-done

	lines := readLines(t, filePath)
	if len(lines) != 2 {
		t.Fatalf("expected 2 filtered lines, got %d", len(lines))
	}

	var r0, r1 JSONLRecord
	if err := json.Unmarshal([]byte(lines[0]), &r0); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &r1); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}

	if r0.EventType != "interject.created" {
		t.Errorf("line 0: expected interject.created, got %s", r0.EventType)
	}
	if r1.EventType != "budget.exceeded" {
		t.Errorf("line 1: expected budget.exceeded, got %s", r1.EventType)
	}
}

func TestJSONLSink_FileRotation(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use maxSizeMB=1 and pre-seed the written counter to just below the
	// 1MB threshold so the first event triggers rotation.
	sink := NewJSONLSink(filePath, bus, logger, WithMaxSizeMB(1))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	// Pre-seed the written counter to just below the 1MB threshold.
	sink.mu.Lock()
	sink.written = 1*1024*1024 - 1
	sink.mu.Unlock()

	// This event should push us over and trigger rotation.
	publishAndDrain(bus, makeEvent("rotation.trigger", "test"))

	// Publish one more to the new file.
	publishAndDrain(bus, makeEvent("after.rotation", "test"))

	cancel()
	<-done

	// The new file at filePath should contain the post-rotation event.
	lines := readLines(t, filePath)
	if len(lines) == 0 {
		t.Fatal("expected at least 1 line in the new file after rotation")
	}

	// Check a rotated file exists in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	rotatedCount := 0
	for _, e := range entries {
		if matched, _ := filepath.Match("audit.jsonl.*.jsonl", e.Name()); matched {
			rotatedCount++
		}
	}
	if rotatedCount == 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected at least 1 rotated file, got 0. files: %v", names)
	}
}

func TestJSONLSink_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sink := NewJSONLSink(filePath, bus, logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	publishAndDrain(bus, makeEvent("ctx.test", "test"))

	cancel()
	<-done

	// After cancellation the file should be closed.
	sink.mu.Lock()
	closed := sink.file == nil
	sink.mu.Unlock()

	if !closed {
		t.Error("expected file to be nil (closed) after context cancellation")
	}

	// The event written before cancellation should still be in the file.
	lines := readLines(t, filePath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestJSONLSink_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sink := NewJSONLSink(filePath, bus, logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	events := []types.NervousEvent{
		makeEvent("pipeline.started", "executor"),
		makeEvent("cron.fire", "scheduler"),
		makeEvent("lifecycle.transition", "agent-mgr"),
	}
	for _, e := range events {
		publishAndDrain(bus, e)
	}

	cancel()
	<-done

	lines := readLines(t, filePath)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var record JSONLRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d: not valid JSON: %v\nraw: %s", i, err, line)
			continue
		}

		// Verify required fields are populated.
		if record.ExportedAt.IsZero() {
			t.Errorf("line %d: exported_at is zero", i)
		}
		if record.EventType != string(events[i].Type) {
			t.Errorf("line %d: event_type mismatch: got %s, want %s", i, record.EventType, events[i].Type)
		}
		if record.Timestamp.IsZero() {
			t.Errorf("line %d: timestamp is zero", i)
		}
		if record.Scope != "test" {
			t.Errorf("line %d: scope mismatch: got %s, want test", i, record.Scope)
		}
		if record.TraceID != "trace-001" {
			t.Errorf("line %d: trace_id mismatch: got %s, want trace-001", i, record.TraceID)
		}

		// Verify payload round-trips.
		var payload map[string]string
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			t.Errorf("line %d: payload not valid JSON: %v", i, err)
		} else if payload["key"] != "value" {
			t.Errorf("line %d: payload key mismatch: got %s", i, payload["key"])
		}
	}
}

func TestJSONLSink_NoFiltersExportsAll(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "audit.jsonl")
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// No WithEventFilters option — should export everything.
	sink := NewJSONLSink(filePath, bus, logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sink.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	publishAndDrain(bus, makeEvent("a.b", "src1"))
	publishAndDrain(bus, makeEvent("c.d", "src2"))
	publishAndDrain(bus, makeEvent("e.f", "src3"))

	cancel()
	<-done

	lines := readLines(t, filePath)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines with no filters, got %d", len(lines))
	}
}
