package repo

import (
	"context"
	"time"
)

// Provider represents an LLM provider configuration.
type Provider struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"` // openai, anthropic, ollama, azure, custom
	BaseURL      string    `json:"base_url"`
	SecretKeyRef string    `json:"secret_key_ref"` // key in secrets store, empty for keyless
	IsDefault    bool      `json:"is_default"`
	IsEnabled    bool      `json:"is_enabled"`
	Models       string    `json:"models"`   // JSON array of model names
	Metadata     string    `json:"metadata"` // JSON bag
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ProviderRepo handles LLM provider CRUD.
type ProviderRepo interface {
	// Create inserts a new provider and returns its generated ID.
	Create(ctx context.Context, p *Provider) (string, error)

	// Get retrieves a provider by its ID.
	Get(ctx context.Context, id string) (*Provider, error)

	// GetByName retrieves a provider by its unique name.
	GetByName(ctx context.Context, name string) (*Provider, error)

	// GetDefault retrieves the provider marked as the global default.
	GetDefault(ctx context.Context) (*Provider, error)

	// List returns all providers ordered by name.
	List(ctx context.Context) ([]*Provider, error)

	// Update modifies an existing provider by ID.
	Update(ctx context.Context, id string, p *Provider) error

	// SetDefault marks a provider as the global default, clearing any existing default.
	SetDefault(ctx context.Context, id string) error

	// Delete removes a provider by its ID.
	Delete(ctx context.Context, id string) error
}
