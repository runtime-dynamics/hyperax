package agentmail

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hyperax/hyperax/pkg/types"
)

// MessengerAdapter defines the contract for external messaging backends.
// Each adapter handles one protocol (webhook, Slack, Discord, AgentMail API, etc.).
// Adapters are registered with the AdapterRegistry and used by the Mailroom
// to dispatch outbound messages and receive inbound messages.
type MessengerAdapter interface {
	// Name returns the unique adapter identifier (e.g. "webhook", "slack").
	Name() string

	// Send delivers an outbound AgentMail message via the adapter's protocol.
	// Returns an error if the message could not be delivered.
	Send(ctx context.Context, mail *types.AgentMail) error

	// Receive polls for inbound messages from the external source.
	// Returns a slice of received messages and any error encountered.
	// Implementations may return an empty slice if no messages are available.
	Receive(ctx context.Context) ([]*types.AgentMail, error)

	// Start initialises the adapter's background resources (listeners, connections).
	// Called once when the adapter is registered and enabled.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the adapter, releasing all resources.
	Stop() error

	// Healthy returns true if the adapter is operational.
	Healthy() bool
}

// AdapterRegistry manages the lifecycle of MessengerAdapter instances.
// It is goroutine-safe and supports dynamic registration/deregistration.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]MessengerAdapter
	logger   *slog.Logger
}

// NewAdapterRegistry creates an empty adapter registry.
func NewAdapterRegistry(logger *slog.Logger) *AdapterRegistry {
	return &AdapterRegistry{
		adapters: make(map[string]MessengerAdapter),
		logger:   logger,
	}
}

// Register adds an adapter to the registry. If an adapter with the same name
// is already registered, it is stopped and replaced.
func (r *AdapterRegistry) Register(adapter MessengerAdapter) error {
	if adapter == nil {
		return fmt.Errorf("adapter must not be nil")
	}
	name := adapter.Name()
	if name == "" {
		return fmt.Errorf("adapter name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.adapters[name]; ok {
		r.logger.Warn("replacing existing adapter", "name", name)
		if err := existing.Stop(); err != nil {
			r.logger.Warn("error stopping replaced adapter", "name", name, "error", err)
		}
	}

	r.adapters[name] = adapter
	r.logger.Info("adapter registered", "name", name)
	return nil
}

// Deregister removes and stops an adapter by name.
// Returns an error if the adapter is not found.
func (r *AdapterRegistry) Deregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	adapter, ok := r.adapters[name]
	if !ok {
		return fmt.Errorf("adapter %q not found", name)
	}

	if err := adapter.Stop(); err != nil {
		r.logger.Warn("error stopping deregistered adapter", "name", name, "error", err)
	}

	delete(r.adapters, name)
	r.logger.Info("adapter deregistered", "name", name)
	return nil
}

// Get returns an adapter by name.
// Returns nil if not found.
func (r *AdapterRegistry) Get(name string) MessengerAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[name]
}

// List returns the names of all registered adapters.
func (r *AdapterRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

// Healthy returns a map of adapter names to their health status.
func (r *AdapterRegistry) Healthy() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]bool, len(r.adapters))
	for name, adapter := range r.adapters {
		result[name] = adapter.Healthy()
	}
	return result
}

// StopAll gracefully stops all registered adapters.
// Errors are logged but do not prevent other adapters from stopping.
func (r *AdapterRegistry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, adapter := range r.adapters {
		if err := adapter.Stop(); err != nil {
			r.logger.Warn("error stopping adapter", "name", name, "error", err)
		}
	}
	r.adapters = make(map[string]MessengerAdapter)
	r.logger.Info("all adapters stopped")
}

// StartAll starts all registered adapters. If an adapter fails to start,
// the error is logged and the adapter remains registered but inactive.
func (r *AdapterRegistry) StartAll(ctx context.Context) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, adapter := range r.adapters {
		if err := adapter.Start(ctx); err != nil {
			r.logger.Error("adapter failed to start", "name", name, "error", err)
			continue
		}
		r.logger.Info("adapter started", "name", name)
	}
}
