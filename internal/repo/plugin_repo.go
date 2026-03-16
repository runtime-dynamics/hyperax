package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// PluginRepo persists plugin state across server restarts.
// This enables plugins that were enabled before shutdown to be
// automatically re-enabled on startup.
type PluginRepo interface {
	// SavePlugin upserts the runtime state for a plugin.
	// If a record with the same name already exists, it is updated.
	SavePlugin(ctx context.Context, state *types.PluginState) error

	// GetPlugin retrieves the persisted state for a plugin by name.
	// Returns repo.ErrNotFound if the plugin has no saved state.
	GetPlugin(ctx context.Context, name string) (*types.PluginState, error)

	// ListPlugins returns all persisted plugin states.
	ListPlugins(ctx context.Context) ([]*types.PluginState, error)

	// DeletePlugin removes the persisted state for a plugin by name.
	DeletePlugin(ctx context.Context, name string) error
}
