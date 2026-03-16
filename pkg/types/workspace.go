package types

import "time"

// WorkspaceInfo describes a registered workspace.
type WorkspaceInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  string    `json:"metadata,omitempty"`
}

// WorkspaceConfig is the bootstrap-time workspace definition.
type WorkspaceConfig struct {
	Name     string `yaml:"name" json:"name"`
	RootPath string `yaml:"root_path" json:"root_path"`
}
