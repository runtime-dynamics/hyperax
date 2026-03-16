package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// WorkspaceRepo handles workspace listing and existence checks.
type WorkspaceRepo interface {
	WorkspaceExists(ctx context.Context, name string) (bool, error)
	ListWorkspaces(ctx context.Context) ([]*types.WorkspaceInfo, error)
	GetWorkspace(ctx context.Context, name string) (*types.WorkspaceInfo, error)
	CreateWorkspace(ctx context.Context, ws *types.WorkspaceInfo) error
	DeleteWorkspace(ctx context.Context, name string) error
}
