package sqlite

import (
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

func TestProviderRepo_Create(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	p := &repo.Provider{
		Name:         "test-anthropic",
		Kind:         "anthropic",
		BaseURL:      "https://api.anthropic.com",
		SecretKeyRef: "anthropic_api_key",
		IsDefault:    false,
		IsEnabled:    true,
		Models:       `["claude-sonnet-4-20250514","claude-opus-4-20250514"]`,
		Metadata:     `{"version":"2024-01"}`,
	}

	id, err := r.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}
}

func TestProviderRepo_CreateKeyless(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	p := &repo.Provider{
		Name:      "local-ollama",
		Kind:      "ollama",
		BaseURL:   "http://localhost:11434",
		IsEnabled: true,
		Models:    `["llama3.2","mistral"]`,
	}

	id, err := r.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// SecretKeyRef should be empty for keyless providers.
	if got.SecretKeyRef != "" {
		t.Errorf("secret_key_ref = %q, want empty", got.SecretKeyRef)
	}
}

func TestProviderRepo_Get(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	p := &repo.Provider{
		Name:         "openai-prod",
		Kind:         "openai",
		BaseURL:      "https://api.openai.com/v1",
		SecretKeyRef: "openai_key",
		IsDefault:    true,
		IsEnabled:    true,
		Models:       `["gpt-4o","o3"]`,
		Metadata:     `{}`,
	}

	id, err := r.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "openai-prod" {
		t.Errorf("name = %q, want %q", got.Name, "openai-prod")
	}
	if got.Kind != "openai" {
		t.Errorf("kind = %q, want %q", got.Kind, "openai")
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want %q", got.BaseURL, "https://api.openai.com/v1")
	}
	if got.SecretKeyRef != "openai_key" {
		t.Errorf("secret_key_ref = %q, want %q", got.SecretKeyRef, "openai_key")
	}
	if !got.IsDefault {
		t.Error("expected is_default = true")
	}
	if !got.IsEnabled {
		t.Error("expected is_enabled = true")
	}
	if got.Models != `["gpt-4o","o3"]` {
		t.Errorf("models = %q", got.Models)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("expected non-zero updated_at")
	}
}

func TestProviderRepo_GetNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	_, err := r.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestProviderRepo_GetByName(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	_, err := r.Create(ctx, &repo.Provider{
		Name:      "my-ollama",
		Kind:      "ollama",
		BaseURL:   "http://localhost:11434",
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.GetByName(ctx, "my-ollama")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}

	if got.Kind != "ollama" {
		t.Errorf("kind = %q, want %q", got.Kind, "ollama")
	}
}

func TestProviderRepo_GetByNameNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	_, err := r.GetByName(ctx, "does-not-exist")
	if err == nil {
		t.Error("expected error for nonexistent name")
	}
}

func TestProviderRepo_List(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	// Empty initially.
	list, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}

	// Add two providers.
	_, err = r.Create(ctx, &repo.Provider{Name: "beta-provider", Kind: "openai", BaseURL: "https://beta.openai.com", IsEnabled: true})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}
	_, err = r.Create(ctx, &repo.Provider{Name: "alpha-provider", Kind: "anthropic", BaseURL: "https://api.anthropic.com", IsEnabled: true})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}

	list, err = r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}

	// Should be ordered by name.
	if list[0].Name != "alpha-provider" {
		t.Errorf("first = %q, want %q", list[0].Name, "alpha-provider")
	}
	if list[1].Name != "beta-provider" {
		t.Errorf("second = %q, want %q", list[1].Name, "beta-provider")
	}
}

func TestProviderRepo_Update(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	id, err := r.Create(ctx, &repo.Provider{
		Name:      "original",
		Kind:      "openai",
		BaseURL:   "https://api.openai.com/v1",
		IsEnabled: true,
		Models:    `["gpt-4o"]`,
		Metadata:  `{}`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated := &repo.Provider{
		Name:         "renamed-provider",
		Kind:         "azure",
		BaseURL:      "https://my.azure.openai.com",
		SecretKeyRef: "azure_key",
		IsDefault:    false,
		IsEnabled:    false,
		Models:       `["gpt-4o","gpt-4o-mini"]`,
		Metadata:     `{"deployment":"east-us"}`,
	}
	if err := r.Update(ctx, id, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "renamed-provider" {
		t.Errorf("name = %q, want %q", got.Name, "renamed-provider")
	}
	if got.Kind != "azure" {
		t.Errorf("kind = %q, want %q", got.Kind, "azure")
	}
	if got.BaseURL != "https://my.azure.openai.com" {
		t.Errorf("base_url = %q, want %q", got.BaseURL, "https://my.azure.openai.com")
	}
	if got.SecretKeyRef != "azure_key" {
		t.Errorf("secret_key_ref = %q, want %q", got.SecretKeyRef, "azure_key")
	}
	if got.IsEnabled {
		t.Error("expected is_enabled = false")
	}
	if got.Models != `["gpt-4o","gpt-4o-mini"]` {
		t.Errorf("models = %q", got.Models)
	}
	if got.Metadata != `{"deployment":"east-us"}` {
		t.Errorf("metadata = %q", got.Metadata)
	}
}

func TestProviderRepo_UpdateNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	err := r.Update(ctx, "nonexistent", &repo.Provider{Name: "x", Kind: "openai", BaseURL: "http://x"})
	if err == nil {
		t.Error("expected error for nonexistent provider update")
	}
}

func TestProviderRepo_Delete(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	id, err := r.Create(ctx, &repo.Provider{
		Name:      "doomed",
		Kind:      "custom",
		BaseURL:   "http://custom.local",
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should no longer be retrievable.
	_, err = r.Get(ctx, id)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestProviderRepo_DeleteNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	err := r.Delete(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider delete")
	}
}

func TestProviderRepo_GetDefault(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	// No default initially.
	_, err := r.GetDefault(ctx)
	if err == nil {
		t.Error("expected error when no default exists")
	}

	// Create a default provider.
	_, err = r.Create(ctx, &repo.Provider{
		Name:      "the-default",
		Kind:      "anthropic",
		BaseURL:   "https://api.anthropic.com",
		IsDefault: true,
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create default: %v", err)
	}

	got, err := r.GetDefault(ctx)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}

	if got.Name != "the-default" {
		t.Errorf("name = %q, want %q", got.Name, "the-default")
	}
	if !got.IsDefault {
		t.Error("expected is_default = true")
	}
}

func TestProviderRepo_SetDefault(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	// Create two providers, first as default.
	id1, err := r.Create(ctx, &repo.Provider{
		Name:      "provider-a",
		Kind:      "openai",
		BaseURL:   "https://api.openai.com/v1",
		IsDefault: true,
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}

	id2, err := r.Create(ctx, &repo.Provider{
		Name:      "provider-b",
		Kind:      "anthropic",
		BaseURL:   "https://api.anthropic.com",
		IsDefault: false,
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	// Switch default to provider-b.
	if err := r.SetDefault(ctx, id2); err != nil {
		t.Fatalf("set default: %v", err)
	}

	// provider-b should be default now.
	got, err := r.GetDefault(ctx)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if got.ID != id2 {
		t.Errorf("default id = %q, want %q", got.ID, id2)
	}

	// provider-a should no longer be default.
	a, err := r.Get(ctx, id1)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if a.IsDefault {
		t.Error("provider-a should no longer be default")
	}
}

func TestProviderRepo_SetDefaultNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	err := r.SetDefault(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider set default")
	}
}

func TestProviderRepo_UniqueNameConstraint(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ProviderRepo{db: db.db}

	_, err := r.Create(ctx, &repo.Provider{
		Name:      "duplicate-name",
		Kind:      "openai",
		BaseURL:   "https://api.openai.com/v1",
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}

	_, err = r.Create(ctx, &repo.Provider{
		Name:      "duplicate-name",
		Kind:      "anthropic",
		BaseURL:   "https://api.anthropic.com",
		IsEnabled: true,
	})
	if err == nil {
		t.Error("expected error for duplicate provider name")
	}
}

func TestProviderRepo_DeleteCascadePersonaRef(t *testing.T) {
	db, ctx := setupTestDB(t)
	providerRepo := &ProviderRepo{db: db.db}
	personaRepo := &PersonaRepo{db: db.db}

	// Create a provider.
	provID, err := providerRepo.Create(ctx, &repo.Provider{
		Name:      "cascade-test",
		Kind:      "openai",
		BaseURL:   "https://api.openai.com/v1",
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Create a persona linked to that provider.
	personaID, err := personaRepo.Create(ctx, &repo.Persona{
		Name:       "linked-persona",
		ProviderID: provID,
		IsActive:   true,
	})
	if err != nil {
		t.Fatalf("create persona: %v", err)
	}

	// Delete the provider — ON DELETE SET NULL should clear the persona's provider_id.
	if err := providerRepo.Delete(ctx, provID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}

	persona, err := personaRepo.Get(ctx, personaID)
	if err != nil {
		t.Fatalf("get persona: %v", err)
	}

	if persona.ProviderID != "" {
		t.Errorf("provider_id = %q after cascade, want empty", persona.ProviderID)
	}
}
