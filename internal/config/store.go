package config

import (
	"context"
	"fmt"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigStore provides scoped configuration resolution.
// Resolution order: agent -> workspace -> global -> config_keys.default_val
type ConfigStore struct {
	repo repo.ConfigRepo
	bus  *nervous.EventBus
}

// NewConfigStore creates a ConfigStore backed by the given repo and EventBus.
func NewConfigStore(repo repo.ConfigRepo, bus *nervous.EventBus) *ConfigStore {
	return &ConfigStore{repo: repo, bus: bus}
}

// Resolve looks up a config value using the scope resolution chain:
// agent -> workspace -> global -> default_val from config_keys.
func (s *ConfigStore) Resolve(ctx context.Context, key, agentID, workspaceID string) (string, error) {
	// Try agent scope
	if agentID != "" {
		val, err := s.repo.GetValue(ctx, key, types.ConfigScope{Type: "agent", ID: agentID})
		if err == nil {
			return val, nil
		}
	}

	// Try workspace scope
	if workspaceID != "" {
		val, err := s.repo.GetValue(ctx, key, types.ConfigScope{Type: "workspace", ID: workspaceID})
		if err == nil {
			return val, nil
		}
	}

	// Try global scope
	val, err := s.repo.GetValue(ctx, key, types.ConfigScope{Type: "global", ID: ""})
	if err == nil {
		return val, nil
	}

	// Fall back to default from key metadata
	meta, err := s.repo.GetKeyMeta(ctx, key)
	if err != nil {
		return "", fmt.Errorf("config.ConfigStore.Resolve: config key %q not found", key)
	}
	return meta.DefaultVal, nil
}

// Set stores a config value at the given scope and publishes a change event.
func (s *ConfigStore) Set(ctx context.Context, key, value string, scope types.ConfigScope, actor string) error {
	// Check if key is critical (requires dashboard confirmation)
	meta, err := s.repo.GetKeyMeta(ctx, key)
	if err != nil {
		return fmt.Errorf("unknown config key %q: %w", key, err)
	}

	if err := s.repo.SetValue(ctx, key, value, scope, actor); err != nil {
		return fmt.Errorf("config.ConfigStore.Set: %w", err)
	}

	// Publish change event
	if s.bus != nil {
		s.bus.Publish(nervous.NewEvent(
			types.EventType("config.changed"),
			actor,
			scope.ID,
			map[string]string{
				"key":        key,
				"value":      value,
				"scope_type": scope.Type,
				"scope_id":   scope.ID,
				"critical":   fmt.Sprintf("%v", meta.Critical),
			},
		))
	}

	return nil
}

// IsCritical checks if a config key requires dashboard confirmation.
func (s *ConfigStore) IsCritical(ctx context.Context, key string) (bool, error) {
	meta, err := s.repo.GetKeyMeta(ctx, key)
	if err != nil {
		return false, fmt.Errorf("config.ConfigStore.IsCritical: %w", err)
	}
	return meta.Critical, nil
}

// ListKeys returns all registered config key definitions.
func (s *ConfigStore) ListKeys(ctx context.Context) ([]types.ConfigKeyMeta, error) {
	return s.repo.ListKeys(ctx)
}
