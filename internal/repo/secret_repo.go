package repo

import "context"

// SecretEntry represents a secret with its metadata (not its value).
type SecretEntry struct {
	Key         string `json:"key"`
	Scope       string `json:"scope"`
	AccessScope string `json:"access_scope"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// SecretRepo handles secret storage with scoped access.
type SecretRepo interface {
	Get(ctx context.Context, key string, scope string) (string, error)
	Set(ctx context.Context, key string, value string, scope string) error
	SetWithAccess(ctx context.Context, key string, value string, scope string, accessScope string) error
	Delete(ctx context.Context, key string, scope string) error
	List(ctx context.Context, scope string) ([]string, error)
	ListEntries(ctx context.Context, scope string) ([]SecretEntry, error)
	GetAccessScope(ctx context.Context, key string, scope string) (string, error)
	UpdateAccessScope(ctx context.Context, key string, scope string, accessScope string) error
}
