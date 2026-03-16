package pulse

import "sync"

// BackpressureMonitor tracks whether the system is under backpressure.
// When backpressure is active, Background-priority cadences are deferred
// to avoid overloading the event bus or downstream consumers.
//
// The monitor uses a simple boolean flag protected by a mutex. External
// callers (or the engine's own tick loop) set the state based on queue
// depth, CPU load, or any other signal appropriate for the deployment.
type BackpressureMonitor struct {
	threshold      int
	inBackpressure bool
	mu             sync.Mutex
}

// NewBackpressureMonitor creates a monitor with the given threshold.
// The threshold is informational — the actual backpressure state is
// toggled explicitly via SetBackpressure.
func NewBackpressureMonitor(threshold int) *BackpressureMonitor {
	if threshold <= 0 {
		threshold = 100
	}
	return &BackpressureMonitor{
		threshold: threshold,
	}
}

// Check reports whether the system is currently under backpressure.
func (m *BackpressureMonitor) Check() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inBackpressure
}

// SetBackpressure sets the backpressure state. When v is true, Background
// cadences will be deferred until backpressure is released.
func (m *BackpressureMonitor) SetBackpressure(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inBackpressure = v
}

// Threshold returns the configured backpressure threshold.
func (m *BackpressureMonitor) Threshold() int {
	return m.threshold
}
