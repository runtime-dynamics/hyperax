package secrets

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name    string
	store   map[string]string // "scope/key" -> value
	healthy bool
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		name:    name,
		store:   make(map[string]string),
		healthy: true,
	}
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Get(_ context.Context, key, scope string) (string, error) {
	v, ok := m.store[scope+"/"+key]
	if !ok {
		return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}
	return v, nil
}

func (m *mockProvider) Set(_ context.Context, key, value, scope string) error {
	m.store[scope+"/"+key] = value
	return nil
}

func (m *mockProvider) Delete(_ context.Context, key, scope string) error {
	k := scope + "/" + key
	if _, ok := m.store[k]; !ok {
		return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}
	delete(m.store, k)
	return nil
}

func (m *mockProvider) List(_ context.Context, scope string) ([]string, error) {
	prefix := scope + "/"
	var keys []string
	for k := range m.store {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k[len(prefix):])
		}
	}
	return keys, nil
}

func (m *mockProvider) Rotate(_ context.Context, key, newValue, scope string) (string, error) {
	k := scope + "/" + key
	old, ok := m.store[k]
	if !ok {
		return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}
	m.store[k] = newValue
	return old, nil
}

func (m *mockProvider) SetWithAccess(_ context.Context, key, value, scope, accessScope string) error {
	m.store[scope+"/"+key] = value
	return nil
}

func (m *mockProvider) ListEntries(_ context.Context, scope string) ([]repo.SecretEntry, error) {
	prefix := scope + "/"
	var entries []repo.SecretEntry
	for k := range m.store {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			entries = append(entries, repo.SecretEntry{
				Key:         k[len(prefix):],
				Scope:       scope,
				AccessScope: "global",
			})
		}
	}
	return entries, nil
}

func (m *mockProvider) GetAccessScope(_ context.Context, _, _ string) (string, error) {
	return "global", nil
}

func (m *mockProvider) UpdateAccessScope(_ context.Context, key, scope, accessScope string) error {
	k := scope + "/" + key
	if _, ok := m.store[k]; !ok {
		return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}
	return nil
}

func (m *mockProvider) Health(_ context.Context) error {
	if !m.healthy {
		return fmt.Errorf("provider %s is unhealthy", m.name)
	}
	return nil
}

func TestRegistry_RegisterAndActive(t *testing.T) {
	reg := NewRegistry()
	p := newMockProvider("local")
	reg.Register(p)

	active, err := reg.Active()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active.Name() != "local" {
		t.Fatalf("expected active=local, got %s", active.Name())
	}
}

func TestRegistry_SetActive(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	if err := reg.SetActive("vault"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.ActiveName() != "vault" {
		t.Fatalf("expected active=vault, got %s", reg.ActiveName())
	}
}

func TestRegistry_SetActive_NotRegistered(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	err := reg.SetActive("nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(names))
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	if p := reg.Get("local"); p == nil {
		t.Fatal("expected local provider")
	}
	if p := reg.Get("nonexistent"); p != nil {
		t.Fatal("expected nil for nonexistent provider")
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate register")
		}
	}()
	reg.Register(newMockProvider("local"))
}

// fakeConfigRepo implements repo.ConfigRepo for testing persistence.
type fakeConfigRepo struct {
	data map[string]string
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{data: make(map[string]string)}
}

func (r *fakeConfigRepo) GetValue(_ context.Context, key string, _ types.ConfigScope) (string, error) {
	v, ok := r.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (r *fakeConfigRepo) SetValue(_ context.Context, key, value string, _ types.ConfigScope, _ string) error {
	r.data[key] = value
	return nil
}

func (r *fakeConfigRepo) GetKeyMeta(_ context.Context, _ string) (*types.ConfigKeyMeta, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *fakeConfigRepo) ListKeys(_ context.Context) ([]types.ConfigKeyMeta, error) {
	return nil, nil
}

func (r *fakeConfigRepo) ListValues(_ context.Context, _ types.ConfigScope) ([]types.ConfigValue, error) {
	return nil, nil
}

func (r *fakeConfigRepo) GetHistory(_ context.Context, _ string, _ int) ([]types.ConfigChange, error) {
	return nil, nil
}

func (r *fakeConfigRepo) UpsertKeyMeta(_ context.Context, _ *types.ConfigKeyMeta) error {
	return nil
}

func TestRegistry_SetActive_Persists(t *testing.T) {
	cr := newFakeConfigRepo()
	reg := NewRegistry()
	reg.SetConfigRepo(cr)
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	if err := reg.SetActive("vault"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	// Verify the selection was persisted.
	val, err := cr.GetValue(context.Background(), configKeySecretsProvider, types.ConfigScope{})
	if err != nil {
		t.Fatalf("config read: %v", err)
	}
	if val != "vault" {
		t.Fatalf("expected persisted value=vault, got %s", val)
	}
}

func TestRegistry_LoadActive_RestoresProvider(t *testing.T) {
	cr := newFakeConfigRepo()
	cr.data[configKeySecretsProvider] = "vault"

	reg := NewRegistry()
	reg.SetConfigRepo(cr)
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	reg.LoadActive(context.Background())

	if reg.ActiveName() != "vault" {
		t.Fatalf("expected active=vault after LoadActive, got %s", reg.ActiveName())
	}
}

func TestRegistry_LoadActive_IgnoresUnregistered(t *testing.T) {
	cr := newFakeConfigRepo()
	cr.data[configKeySecretsProvider] = "nonexistent"

	reg := NewRegistry()
	reg.SetConfigRepo(cr)
	reg.Register(newMockProvider("local"))

	reg.LoadActive(context.Background())

	// Should stay at default since "nonexistent" is not registered.
	if reg.ActiveName() != "local" {
		t.Fatalf("expected active=local (default), got %s", reg.ActiveName())
	}
}

func TestRegistry_Unregister_PersistsActiveFallback(t *testing.T) {
	cr := newFakeConfigRepo()
	reg := NewRegistry()
	reg.SetConfigRepo(cr)
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	// Set vault as active (this persists "vault" in config).
	if err := reg.SetActive("vault"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if reg.ActiveName() != "vault" {
		t.Fatalf("expected active=vault, got %s", reg.ActiveName())
	}

	// Unregister vault — should fall back to "local" and persist the fallback.
	if !reg.Unregister("vault") {
		t.Fatal("Unregister returned false for registered provider")
	}
	if reg.ActiveName() != "local" {
		t.Fatalf("expected active=local after unregister, got %s", reg.ActiveName())
	}

	// Verify the fallback was persisted to config.
	val, err := cr.GetValue(context.Background(), configKeySecretsProvider, types.ConfigScope{})
	if err != nil {
		t.Fatalf("config read: %v", err)
	}
	if val != "local" {
		t.Fatalf("expected persisted value=local, got %s", val)
	}
}

func TestRegistry_Unregister_InactiveDoesNotPersist(t *testing.T) {
	cr := newFakeConfigRepo()
	reg := NewRegistry()
	reg.SetConfigRepo(cr)
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	// Active is "local" (default). Unregister vault — should NOT touch config.
	if !reg.Unregister("vault") {
		t.Fatal("Unregister returned false for registered provider")
	}
	if reg.ActiveName() != "local" {
		t.Fatalf("expected active=local, got %s", reg.ActiveName())
	}

	// Config should NOT have been written (no SetValue call for fallback).
	if _, err := cr.GetValue(context.Background(), configKeySecretsProvider, types.ConfigScope{}); err == nil {
		t.Fatal("expected no config entry when unregistering inactive provider")
	}
}

func TestRegistry_IsActive(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))
	reg.Register(newMockProvider("vault"))

	if !reg.IsActive("local") {
		t.Error("expected IsActive(local)=true")
	}
	if reg.IsActive("vault") {
		t.Error("expected IsActive(vault)=false")
	}

	if err := reg.SetActive("vault"); err != nil {
		t.Fatal(err)
	}
	if reg.IsActive("local") {
		t.Error("expected IsActive(local)=false after switching")
	}
	if !reg.IsActive("vault") {
		t.Error("expected IsActive(vault)=true after switching")
	}
}

func TestRegistry_LoadActive_NilConfigRepo(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	// Should not panic and keep default.
	reg.LoadActive(context.Background())

	if reg.ActiveName() != "local" {
		t.Fatalf("expected active=local, got %s", reg.ActiveName())
	}
}
