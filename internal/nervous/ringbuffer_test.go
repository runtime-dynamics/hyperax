package nervous

import (
	"sync"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestRingBuffer_PushAndSize(t *testing.T) {
	rb := NewRingBuffer(5)

	if rb.Size() != 0 {
		t.Errorf("initial size = %d, want 0", rb.Size())
	}
	if rb.Capacity() != 5 {
		t.Errorf("capacity = %d, want 5", rb.Capacity())
	}

	for i := uint64(1); i <= 3; i++ {
		rb.Push(types.NervousEvent{SequenceID: i, Type: types.EventMCPRequest})
	}

	if rb.Size() != 3 {
		t.Errorf("size after 3 pushes = %d, want 3", rb.Size())
	}
}

func TestRingBuffer_DefaultCapacity(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.Capacity() != DefaultRingCapacity {
		t.Errorf("default capacity = %d, want %d", rb.Capacity(), DefaultRingCapacity)
	}

	rbNeg := NewRingBuffer(-1)
	if rbNeg.Capacity() != DefaultRingCapacity {
		t.Errorf("negative capacity = %d, want %d", rbNeg.Capacity(), DefaultRingCapacity)
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer(3)

	// Push 5 events into a buffer of size 3.
	for i := uint64(1); i <= 5; i++ {
		rb.Push(types.NervousEvent{SequenceID: i, Type: types.EventMCPRequest})
	}

	if rb.Size() != 3 {
		t.Fatalf("size = %d, want 3 (at capacity)", rb.Size())
	}

	snap := rb.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}

	// Should contain events 3, 4, 5 (oldest two overwritten).
	expectedSeqs := []uint64{3, 4, 5}
	for i, want := range expectedSeqs {
		if snap[i].SequenceID != want {
			t.Errorf("snapshot[%d].SequenceID = %d, want %d", i, snap[i].SequenceID, want)
		}
	}
}

func TestRingBuffer_Snapshot_Empty(t *testing.T) {
	rb := NewRingBuffer(10)
	snap := rb.Snapshot()
	if snap != nil {
		t.Errorf("expected nil snapshot for empty buffer, got %d events", len(snap))
	}
}

func TestRingBuffer_Snapshot_PartialFill(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := uint64(1); i <= 4; i++ {
		rb.Push(types.NervousEvent{SequenceID: i})
	}

	snap := rb.Snapshot()
	if len(snap) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(snap))
	}

	for i, e := range snap {
		want := uint64(i + 1)
		if e.SequenceID != want {
			t.Errorf("snapshot[%d].SequenceID = %d, want %d", i, e.SequenceID, want)
		}
	}
}

func TestRingBuffer_Replay(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := uint64(1); i <= 5; i++ {
		rb.Push(types.NervousEvent{SequenceID: i, Type: types.EventMCPRequest})
	}

	// Replay everything after seq 0 (should get all 5).
	events := rb.Replay(0)
	if len(events) != 5 {
		t.Fatalf("replay(0) len = %d, want 5", len(events))
	}

	// Replay after seq 3 (should get seq 4 and 5).
	events = rb.Replay(3)
	if len(events) != 2 {
		t.Fatalf("replay(3) len = %d, want 2", len(events))
	}
	if events[0].SequenceID != 4 {
		t.Errorf("replay(3)[0].SequenceID = %d, want 4", events[0].SequenceID)
	}
	if events[1].SequenceID != 5 {
		t.Errorf("replay(3)[1].SequenceID = %d, want 5", events[1].SequenceID)
	}

	// Replay after a very high seq (should get nothing).
	events = rb.Replay(100)
	if len(events) != 0 {
		t.Errorf("replay(100) len = %d, want 0", len(events))
	}
}

func TestRingBuffer_Replay_Empty(t *testing.T) {
	rb := NewRingBuffer(5)
	events := rb.Replay(0)
	if events != nil {
		t.Errorf("expected nil replay for empty buffer, got %d events", len(events))
	}
}

func TestRingBuffer_Replay_AfterOverflow(t *testing.T) {
	rb := NewRingBuffer(3)

	// Push 7 events into a buffer of capacity 3.
	// After all pushes: buf = [7, 5, 6], head=1, count=3.
	// Oldest index = head = 1, so iteration order is: 5, 6, 7.
	for i := uint64(1); i <= 7; i++ {
		rb.Push(types.NervousEvent{SequenceID: i})
	}

	// Replay events with SequenceID > 4 should yield 5, 6, 7.
	events := rb.Replay(4)
	if len(events) != 3 {
		t.Fatalf("replay(4) len = %d, want 3", len(events))
	}

	expectedSeqs := []uint64{5, 6, 7}
	for i, want := range expectedSeqs {
		if events[i].SequenceID != want {
			t.Errorf("events[%d].SequenceID = %d, want %d", i, events[i].SequenceID, want)
		}
	}

	// Replay events with SequenceID > 5 should yield 6, 7.
	events = rb.Replay(5)
	if len(events) != 2 {
		t.Fatalf("replay(5) len = %d, want 2", len(events))
	}
	if events[0].SequenceID != 6 {
		t.Errorf("events[0].SequenceID = %d, want 6", events[0].SequenceID)
	}
	if events[1].SequenceID != 7 {
		t.Errorf("events[1].SequenceID = %d, want 7", events[1].SequenceID)
	}
}

func TestRingBuffer_ConcurrentPush(t *testing.T) {
	rb := NewRingBuffer(1024)

	const goroutines = 10
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(base uint64) {
			defer wg.Done()
			for i := uint64(0); i < perGoroutine; i++ {
				rb.Push(types.NervousEvent{SequenceID: base*perGoroutine + i + 1})
			}
		}(uint64(g))
	}

	wg.Wait()

	total := goroutines * perGoroutine
	if rb.Size() != total {
		t.Errorf("size = %d, want %d", rb.Size(), total)
	}

	snap := rb.Snapshot()
	if len(snap) != total {
		t.Errorf("snapshot len = %d, want %d", len(snap), total)
	}
}

func TestRingBuffer_ConcurrentPushOverflow(t *testing.T) {
	rb := NewRingBuffer(50) // small buffer

	const goroutines = 10
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(base uint64) {
			defer wg.Done()
			for i := uint64(0); i < perGoroutine; i++ {
				rb.Push(types.NervousEvent{SequenceID: base*perGoroutine + i + 1})
			}
		}(uint64(g))
	}

	wg.Wait()

	// Size should be capped at capacity.
	if rb.Size() != 50 {
		t.Errorf("size = %d, want 50", rb.Size())
	}
}

func TestRingBuffer_SnapshotIsACopy(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Push(types.NervousEvent{SequenceID: 1, Source: "original"})

	snap := rb.Snapshot()
	snap[0].Source = "modified"

	snap2 := rb.Snapshot()
	if snap2[0].Source != "original" {
		t.Errorf("snapshot mutation leaked: got %q, want %q", snap2[0].Source, "original")
	}
}
