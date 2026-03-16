package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// Provider defines the interface for secret storage backends.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Name returns the provider identifier (e.g., "local", "vault", "onepassword").
	Name() string

	// Get retrieves a secret value by key and scope.
	// Returns ErrSecretNotFound if the secret does not exist.
	Get(ctx context.Context, key, scope string) (string, error)

	// Set creates or updates a secret value for the given key and scope.
	Set(ctx context.Context, key, value, scope string) error

	// Delete removes a secret by key and scope.
	// Returns ErrSecretNotFound if the secret does not exist.
	Delete(ctx context.Context, key, scope string) error

	// List returns all secret keys for the given scope.
	List(ctx context.Context, scope string) ([]string, error)

	// SetWithAccess stores a secret with an access scope restriction.
	SetWithAccess(ctx context.Context, key, value, scope, accessScope string) error

	// ListEntries returns secret metadata (not values) for a scope.
	ListEntries(ctx context.Context, scope string) ([]repo.SecretEntry, error)

	// GetAccessScope returns the access_scope for a secret.
	GetAccessScope(ctx context.Context, key, scope string) (string, error)

	// UpdateAccessScope changes the access restriction on an existing secret.
	UpdateAccessScope(ctx context.Context, key, scope, accessScope string) error

	// Rotate replaces a secret value atomically, returning the old value.
	// Returns ErrSecretNotFound if the secret does not exist.
	Rotate(ctx context.Context, key, newValue, scope string) (oldValue string, err error)

	// Health checks whether the provider backend is reachable and operational.
	Health(ctx context.Context) error
}

// ErrSecretNotFound is returned when a requested secret does not exist.
var ErrSecretNotFound = fmt.Errorf("secret not found")

// configKeySecretsProvider is the config key used to persist the active provider selection.
const configKeySecretsProvider = "secrets.provider"

// Registry manages named secret providers and routes operations to the active one.
// The active provider is selected by the "secrets.provider" config key.
// When a ConfigRepo is provided, the selection is persisted across restarts.
type Registry struct {
	mu         sync.RWMutex
	providers  map[string]Provider
	active     string
	configRepo repo.ConfigRepo
}

// NewRegistry creates a Registry with no providers registered.
// Use SetConfigRepo to enable persistence of the active provider selection.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		active:    "local",
	}
}

// SetConfigRepo sets the config repository used for persisting the active provider.
// Call this after construction to enable persistence. If configRepo is nil, persistence
// is disabled and the registry operates in memory-only mode.
func (r *Registry) SetConfigRepo(cr repo.ConfigRepo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configRepo = cr

	// Ensure the config key metadata exists so FK constraints are satisfied.
	if cr != nil {
		_ = cr.UpsertKeyMeta(context.Background(), &types.ConfigKeyMeta{
			Key:         configKeySecretsProvider,
			ScopeType:   "global",
			ValueType:   "string",
			DefaultVal:  "local",
			Critical:    true,
			Description: "Active secret provider (local is built-in; others registered via plugins)",
		})
	}
}

// LoadActive reads the persisted provider selection from the config store.
// If no persisted value exists or the provider is not registered, the default
// ("local") is retained. Call this after all providers have been registered.
func (r *Registry) LoadActive(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.configRepo == nil {
		return
	}

	scope := types.ConfigScope{Type: "global"}
	val, err := r.configRepo.GetValue(ctx, configKeySecretsProvider, scope)
	if err != nil || val == "" {
		return
	}

	if _, ok := r.providers[val]; ok {
		r.active = val
		slog.Info("secret provider restored from config", "provider", val)
	} else {
		slog.Warn("persisted secret provider not registered, keeping default",
			"persisted", val, "active", r.active)
	}
}

// Register adds a provider to the registry. Panics if name is already registered.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if _, exists := r.providers[name]; exists {
		panic(fmt.Sprintf("secrets: provider %q already registered", name))
	}
	r.providers[name] = p
}

// Unregister removes a provider from the registry by name.
// If the removed provider was the active one, the active selection falls back to "local".
// Returns false if the provider was not registered.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; !exists {
		return false
	}
	delete(r.providers, name)

	// If we just removed the active provider, fall back to "local".
	if r.active == name {
		r.active = "local"
		slog.Info("active secret provider removed, falling back to local", "removed", name)
		// Persist the fallback so it survives restarts.
		if r.configRepo != nil {
			scope := types.ConfigScope{Type: "global"}
			_ = r.configRepo.SetValue(context.Background(), configKeySecretsProvider, "local", scope, "system")
		}
	}
	return true
}


// SetActive switches the active provider by name.
// Returns an error if the provider is not registered.
// If a ConfigRepo is configured, the selection is persisted for restart recovery.
func (r *Registry) SetActive(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("secrets: provider %q not registered", name)
	}
	r.active = name

	// Persist the selection if a config repo is available.
	if r.configRepo != nil {
		scope := types.ConfigScope{Type: "global"}
		if err := r.configRepo.SetValue(context.Background(), configKeySecretsProvider, name, scope, "system"); err != nil {
			slog.Warn("failed to persist secret provider selection", "provider", name, "error", err)
		}
	}

	return nil
}

// Active returns the currently active provider.
// Returns an error if no provider is active or the active provider is not registered.
func (r *Registry) Active() (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[r.active]
	if !ok {
		return nil, fmt.Errorf("secrets: active provider %q not registered", r.active)
	}
	return p, nil
}

// IsActive returns true if the named provider is currently the active one.
func (r *Registry) IsActive(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active == name
}

// ActiveName returns the name of the currently active provider.
func (r *Registry) ActiveName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// List returns the names of all registered providers.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Get returns a provider by name, or nil if not registered.
func (r *Registry) Get(name string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[name]
}
