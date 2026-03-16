package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// LocalProvider wraps the existing SecretRepo as a Provider implementation.
// This is the default provider that stores secrets in the local SQLite database.
type LocalProvider struct {
	repo repo.SecretRepo
}

// NewLocalProvider creates a LocalProvider backed by the given SecretRepo.
// Returns nil if the repo is nil.
func NewLocalProvider(r repo.SecretRepo) *LocalProvider {
	if r == nil {
		return nil
	}
	return &LocalProvider{repo: r}
}

// Name returns "local".
func (p *LocalProvider) Name() string { return "local" }

// Get retrieves a secret from the local SQLite store.
func (p *LocalProvider) Get(ctx context.Context, key, scope string) (string, error) {
	val, err := p.repo.Get(ctx, key, scope)
	if err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
		}
		return "", fmt.Errorf("local get: %w", err)
	}
	return val, nil
}

// Set creates or updates a secret in the local SQLite store.
func (p *LocalProvider) Set(ctx context.Context, key, value, scope string) error {
	if err := p.repo.Set(ctx, key, value, scope); err != nil {
		return fmt.Errorf("local set: %w", err)
	}
	return nil
}

// Delete removes a secret from the local SQLite store.
func (p *LocalProvider) Delete(ctx context.Context, key, scope string) error {
	if err := p.repo.Delete(ctx, key, scope); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
		}
		return fmt.Errorf("local delete: %w", err)
	}
	return nil
}

// List returns all secret keys for a scope from the local SQLite store.
func (p *LocalProvider) List(ctx context.Context, scope string) ([]string, error) {
	keys, err := p.repo.List(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("local list: %w", err)
	}
	return keys, nil
}

// SetWithAccess creates or updates a secret with an access scope restriction.
func (p *LocalProvider) SetWithAccess(ctx context.Context, key, value, scope, accessScope string) error {
	if err := p.repo.SetWithAccess(ctx, key, value, scope, accessScope); err != nil {
		return fmt.Errorf("local set with access: %w", err)
	}
	return nil
}

// ListEntries returns secret metadata (not values) for a scope.
func (p *LocalProvider) ListEntries(ctx context.Context, scope string) ([]repo.SecretEntry, error) {
	entries, err := p.repo.ListEntries(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("local list entries: %w", err)
	}
	return entries, nil
}

// GetAccessScope returns the access_scope for a secret.
func (p *LocalProvider) GetAccessScope(ctx context.Context, key, scope string) (string, error) {
	as, err := p.repo.GetAccessScope(ctx, key, scope)
	if err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
		}
		return "", fmt.Errorf("local get access scope: %w", err)
	}
	return as, nil
}

// UpdateAccessScope changes the access_scope of an existing secret.
func (p *LocalProvider) UpdateAccessScope(ctx context.Context, key, scope, accessScope string) error {
	if err := p.repo.UpdateAccessScope(ctx, key, scope, accessScope); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
		}
		return fmt.Errorf("local update access scope: %w", err)
	}
	return nil
}

// Rotate replaces a secret value, returning the old value.
// This is not truly atomic since SecretRepo has no transaction support.
// If the Set fails, a rollback to the old value is attempted to maintain
// consistency. An error is always returned on Set failure — the old value
// is never returned in an error case.
func (p *LocalProvider) Rotate(ctx context.Context, key, newValue, scope string) (string, error) {
	oldVal, err := p.repo.Get(ctx, key, scope)
	if err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
		}
		return "", fmt.Errorf("local rotate get: %w", err)
	}

	if err := p.repo.Set(ctx, key, newValue, scope); err != nil {
		// Attempt rollback: restore the old value to avoid inconsistent state.
		if rbErr := p.repo.Set(ctx, key, oldVal, scope); rbErr != nil {
			return "", fmt.Errorf("local rotate set failed (%w) and rollback also failed: %v", err, rbErr)
		}
		return "", fmt.Errorf("local rotate set: %w", err)
	}

	return oldVal, nil
}

// Health always returns nil for the local provider since SQLite is always co-located.
func (p *LocalProvider) Health(ctx context.Context) error {
	return nil
}

// isNotFound checks if an error message indicates a missing secret.
// The existing SecretRepo returns formatted error strings rather than sentinel errors.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}
