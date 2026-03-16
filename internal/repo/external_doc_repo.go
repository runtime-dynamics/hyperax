package repo

import (
	"context"
	"time"
)

// ExternalDocSource represents a read-only documentation directory linked to a workspace.
type ExternalDocSource struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	CreatedAt   time.Time `json:"created_at"`
}

// DocTag marks a document as fulfilling a workspace documentation role (architecture or standards).
type DocTag struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	FilePath    string    `json:"file_path"`
	Tag         string    `json:"tag"`         // "architecture" or "standards"
	SourceType  string    `json:"source_type"` // "internal" or "external"
	CreatedAt   time.Time `json:"created_at"`
}

// ExternalDocRepo manages external documentation sources and document tags.
type ExternalDocRepo interface {
	AddExternalDocSource(ctx context.Context, source *ExternalDocSource) error
	RemoveExternalDocSource(ctx context.Context, id string) error
	ListExternalDocSources(ctx context.Context, workspaceID string) ([]*ExternalDocSource, error)
	GetExternalDocSource(ctx context.Context, id string) (*ExternalDocSource, error)

	TagDocument(ctx context.Context, tag *DocTag) error
	UntagDocument(ctx context.Context, workspaceID, tag string) error
	ListDocTags(ctx context.Context, workspaceID string) ([]*DocTag, error)
	GetDocTag(ctx context.Context, workspaceID, tag string) (*DocTag, error)
}
