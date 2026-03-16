package nervous

import (
	"sync"

	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// DefaultRingCapacity is the default number of events the ring buffer holds.
	DefaultRingCapacity = 10_000
)

// RingBuffer is a fixed-capacity circular buffer of NervousEvents.
// It provides thread-safe push, replay (from a given sequence ID), and
// snapshot operations. When the buffer is full, the oldest event is
// silently overwritten.
type RingBuffer struct {
	mu       sync.RWMutex
	buf      []types.NervousEvent
	capacity int
	head     int // next write position
	count    int // number of valid entries (0..capacity)
}

// NewRingBuffer creates a RingBuffer with the given capacity.
// If capacity is <= 0, DefaultRingCapacity is used.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingCapacity
	}
	return &RingBuffer{
		buf:      make([]types.NervousEvent, capacity),
		capacity: capacity,
	}
}

// Push adds an event to the ring buffer. When the buffer is full the
// oldest event is overwritten.
func (r *RingBuffer) Push(event types.NervousEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf[r.head] = event
	r.head = (r.head + 1) % r.capacity

	if r.count < r.capacity {
		r.count++
	}
}

// Replay returns all buffered events with a SequenceID strictly greater
// than sinceSequenceID, in insertion order. This is used for late-join
// replay so a new subscriber can catch up.
func (r *RingBuffer) Replay(sinceSequenceID uint64) []types.NervousEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	// Calculate the start index of the oldest entry.
	start := r.oldestIndex()
	result := make([]types.NervousEvent, 0, r.count)

	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.capacity
		if r.buf[idx].SequenceID > sinceSequenceID {
			result = append(result, r.buf[idx])
		}
	}

	return result
}

// Snapshot returns a copy of all buffered events in insertion order
// (oldest first).
func (r *RingBuffer) Snapshot() []types.NervousEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	start := r.oldestIndex()
	result := make([]types.NervousEvent, r.count)

	for i := 0; i < r.count; i++ {
		result[i] = r.buf[(start+i)%r.capacity]
	}

	return result
}

// Size returns the number of events currently stored.
func (r *RingBuffer) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Capacity returns the maximum number of events the buffer can hold.
func (r *RingBuffer) Capacity() int {
	return r.capacity
}

// oldestIndex returns the index of the oldest entry. Caller must hold r.mu.
func (r *RingBuffer) oldestIndex() int {
	if r.count < r.capacity {
		return 0
	}
	return r.head // head points to the next overwrite, which is the oldest
}
