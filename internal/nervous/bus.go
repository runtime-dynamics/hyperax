package nervous

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/hyperax/hyperax/pkg/types"
)

// Subscriber receives events from the EventBus.
type Subscriber struct {
	ID     string
	Ch     chan types.NervousEvent
	Filter func(types.NervousEvent) bool
}

// EventBus is the central publish/subscribe message backbone.
// All inter-subsystem communication flows through the EventBus.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	lamport     atomic.Uint64
	bufferSize  int
}

// NewEventBus creates an EventBus with the given channel buffer size.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &EventBus{
		subscribers: make(map[string]*Subscriber),
		bufferSize:  bufferSize,
	}
}

// Publish sends an event to all matching subscribers.
// It increments the Lamport clock and stamps the event's SequenceID.
// Non-blocking: if a subscriber's channel is full, the event is dropped
// for that subscriber (backpressure).
func (b *EventBus) Publish(event types.NervousEvent) {
	event.SequenceID = b.lamport.Add(1)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		if sub.Filter != nil && !sub.Filter(event) {
			continue
		}
		select {
		case sub.Ch <- event:
		default:
			slog.Error("event dropped due to subscriber backpressure", "subscriber", sub.ID, "event_type", event.Type)
		}
	}
}

// Subscribe registers a subscriber. If filter is nil, all events are received.
// Returns the Subscriber (caller reads from sub.Ch).
func (b *EventBus) Subscribe(id string, filter func(types.NervousEvent) bool) *Subscriber {
	sub := &Subscriber{
		ID:     id,
		Ch:     make(chan types.NervousEvent, b.bufferSize),
		Filter: filter,
	}

	b.mu.Lock()
	b.subscribers[id] = sub
	b.mu.Unlock()

	return sub
}

// SubscribeTypes returns a subscriber that only receives events of the given types.
func (b *EventBus) SubscribeTypes(id string, eventTypes ...types.EventType) *Subscriber {
	typeSet := make(map[types.EventType]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		typeSet[t] = struct{}{}
	}
	return b.Subscribe(id, func(e types.NervousEvent) bool {
		_, ok := typeSet[e.Type]
		return ok
	})
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	sub, ok := b.subscribers[id]
	if ok {
		delete(b.subscribers, id)
	}
	b.mu.Unlock()

	if ok {
		close(sub.Ch)
	}
}

// Merge updates the local Lamport clock on receiving a remote event.
// local = max(local, remote) + 1
func (b *EventBus) Merge(remoteSeq uint64) uint64 {
	for {
		current := b.lamport.Load()
		next := remoteSeq
		if current > next {
			next = current
		}
		next++
		if b.lamport.CompareAndSwap(current, next) {
			return next
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Run blocks until the context is cancelled, then cleans up all subscribers.
func (b *EventBus) Run(ctx context.Context) {
	<-ctx.Done()

	b.mu.Lock()
	for id, sub := range b.subscribers {
		close(sub.Ch)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
}
