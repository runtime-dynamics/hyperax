package nervous

import (
	"sync"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// DefaultDriftThreshold is the maximum allowed divergence between
	// wall-clock time and Lamport ordering before a drift event is emitted.
	DefaultDriftThreshold = 5 * time.Second
)

// DriftGuard monitors event ordering for clock drift.
// When the wall-clock gap between consecutive events disagrees with
// Lamport ordering by more than the threshold, it emits a drift event.
type DriftGuard struct {
	mu        sync.Mutex
	bus       *EventBus
	threshold time.Duration
	lastEvent *types.NervousEvent
}

// NewDriftGuard creates a DriftGuard attached to the given EventBus.
func NewDriftGuard(bus *EventBus, threshold time.Duration) *DriftGuard {
	if threshold <= 0 {
		threshold = DefaultDriftThreshold
	}
	return &DriftGuard{
		bus:       bus,
		threshold: threshold,
	}
}

// Check examines an event for clock drift relative to the last seen event.
// Returns true if drift was detected.
func (d *DriftGuard) Check(event types.NervousEvent) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastEvent == nil {
		d.lastEvent = &event
		return false
	}

	// If Lamport says A < B but wall-clock says A > B by threshold, drift detected
	if event.SequenceID > d.lastEvent.SequenceID {
		wallDiff := d.lastEvent.Timestamp.Sub(event.Timestamp)
		if wallDiff > d.threshold {
			d.emitDrift(d.lastEvent, &event, wallDiff)
			d.lastEvent = &event
			return true
		}
	}

	// If Lamport says A > B but wall-clock says A < B by threshold, drift detected
	if event.SequenceID < d.lastEvent.SequenceID {
		wallDiff := event.Timestamp.Sub(d.lastEvent.Timestamp)
		if wallDiff > d.threshold {
			d.emitDrift(&event, d.lastEvent, wallDiff)
			d.lastEvent = &event
			return true
		}
	}

	d.lastEvent = &event
	return false
}

func (d *DriftGuard) emitDrift(older, newer *types.NervousEvent, drift time.Duration) {
	d.bus.Publish(NewEvent(
		types.EventNervousDriftDetected,
		"drift_guard",
		"global",
		map[string]any{
			"drift_ms":     drift.Milliseconds(),
			"older_seq":    older.SequenceID,
			"newer_seq":    newer.SequenceID,
			"older_time":   older.Timestamp,
			"newer_time":   newer.Timestamp,
			"older_source": older.Source,
			"newer_source": newer.Source,
		},
	))
}
