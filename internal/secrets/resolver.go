package secrets

import (
	"context"
	"fmt"
	"strings"
)

// ResolveSecretRef resolves a "secret:key" or "secret:key:scope" reference
// to its actual value using the active provider from the registry.
//
// Supported formats:
//   - "secret:my_key"           → Get(ctx, "my_key", "global")
//   - "secret:my_key:workspace" → Get(ctx, "my_key", "workspace")
//
// If the value does not start with "secret:", it is returned as-is (not a reference).
func ResolveSecretRef(ctx context.Context, reg *Registry, ref string) (string, error) {
	key, scope, ok := ParseSecretRef(ref)
	if !ok {
		return ref, nil
	}

	provider, err := reg.Active()
	if err != nil {
		return "", fmt.Errorf("resolve secret ref: %w", err)
	}

	val, err := provider.Get(ctx, key, scope)
	if err != nil {
		return "", fmt.Errorf("resolve secret %q: %w", ref, err)
	}
	return val, nil
}

// ParseSecretRef parses a "secret:key" or "secret:key:scope" reference.
// Returns the key, scope, and whether the value is a secret reference.
// If no scope is provided, defaults to "global".
func ParseSecretRef(ref string) (key, scope string, ok bool) {
	if !strings.HasPrefix(ref, "secret:") {
		return "", "", false
	}

	rest := ref[7:] // strip "secret:" prefix
	if rest == "" {
		return "", "", false
	}

	parts := strings.SplitN(rest, ":", 2)
	key = parts[0]
	scope = "global"
	if len(parts) == 2 && parts[1] != "" {
		scope = parts[1]
	}

	return key, scope, true
}

// BuildResolverFunc creates a function compatible with the SensorManager.SecretResolver
// type, backed by the secret provider registry.
func BuildResolverFunc(reg *Registry) func(ctx context.Context, ref string) (string, error) {
	if reg == nil {
		return nil
	}
	return func(ctx context.Context, ref string) (string, error) {
		provider, err := reg.Active()
		if err != nil {
			return "", err
		}

		// The sensor resolver receives the ref already stripped of the "secret:" prefix.
		// It may still contain a scope suffix: "key_name" or "key_name:scope".
		parts := strings.SplitN(ref, ":", 2)
		key := parts[0]
		scope := "global"
		if len(parts) == 2 && parts[1] != "" {
			scope = parts[1]
		}

		return provider.Get(ctx, key, scope)
	}
}
