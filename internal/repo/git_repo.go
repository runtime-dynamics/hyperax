package repo

import "context"

// GitInfo represents git repository information.
type GitInfo struct {
	WorkspaceName string
	Branch        string
	CommitHash    string
	RemoteURL     string
	IsDirty       bool
}

// Submodule represents a git submodule.
type Submodule struct {
	Name   string
	Path   string
	URL    string
	Branch string
}

// RecentChange represents a single commit with its changed files.
type RecentChange struct {
	Hash    string   `json:"hash"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}

// GitRepo handles git-based workspace identity.
type GitRepo interface {
	GetInfo(ctx context.Context, workspaceName string) (*GitInfo, error)
	ListSubmodules(ctx context.Context, workspaceName string) ([]*Submodule, error)
	GetRecentChanges(ctx context.Context, workspaceName string, limit int, pathFilter string) ([]*RecentChange, error)
}
