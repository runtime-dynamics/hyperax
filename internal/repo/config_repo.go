package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigRepo handles runtime configuration stored in the database.
type ConfigRepo interface {
	GetValue(ctx context.Context, key string, scope types.ConfigScope) (string, error)
	SetValue(ctx context.Context, key, value string, scope types.ConfigScope, actor string) error
	GetKeyMeta(ctx context.Context, key string) (*types.ConfigKeyMeta, error)
	ListKeys(ctx context.Context) ([]types.ConfigKeyMeta, error)
	ListValues(ctx context.Context, scope types.ConfigScope) ([]types.ConfigValue, error)
	GetHistory(ctx context.Context, key string, limit int) ([]types.ConfigChange, error)
	UpsertKeyMeta(ctx context.Context, meta *types.ConfigKeyMeta) error
}
