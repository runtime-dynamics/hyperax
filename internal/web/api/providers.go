package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/internal/repo"
)

// ProviderAPI handles REST endpoints for LLM providers.
type ProviderAPI struct {
	repo    repo.ProviderRepo
	secrets repo.SecretRepo
}

func NewProviderAPI(r repo.ProviderRepo, secrets repo.SecretRepo) *ProviderAPI {
	return &ProviderAPI{repo: r, secrets: secrets}
}

func (a *ProviderAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.list)
	r.Post("/", a.create)
	r.Get("/default", a.getDefault)
	r.Get("/{id}", a.get)
	r.Put("/{id}", a.update)
	r.Delete("/{id}", a.remove)
	r.Post("/{id}/default", a.setDefault)
	r.Post("/{id}/test", a.testConnection)
	return r
}

func (a *ProviderAPI) list(w http.ResponseWriter, r *http.Request) {
	providers, err := a.repo.List(r.Context())
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, providers)
}

func (a *ProviderAPI) get(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	p, err := a.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "provider not found")
		return
	}
	respondJSON(w, r,http.StatusOK, p)
}

func (a *ProviderAPI) getDefault(w http.ResponseWriter, r *http.Request) {
	p, err := a.repo.GetDefault(r.Context())
	if err != nil {
		respondError(w, r,http.StatusNotFound, "no default provider set")
		return
	}
	respondJSON(w, r,http.StatusOK, p)
}

// createBody extends the provider with an optional raw API key.
// When api_key is provided, it's auto-stored in the secrets store.
type createBody struct {
	repo.Provider
	APIKey string `json:"api_key"`
}

func (a *ProviderAPI) create(w http.ResponseWriter, r *http.Request) {
	var body createBody
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.Kind == "" {
		respondError(w, r,http.StatusBadRequest, "name and kind are required")
		return
	}

	// Auto-fill base URL for managed providers.
	if body.BaseURL == "" {
		if url := managedBaseURL(body.Kind); url != "" {
			body.BaseURL = url
		}
	}

	p := &body.Provider

	// If a raw API key was provided, store it in secrets automatically.
	if body.APIKey != "" && a.secrets != nil {
		secretName := fmt.Sprintf("provider-%s-key", p.Kind)
		if p.Name != "" {
			secretName = fmt.Sprintf("provider-%s-key", sanitizeSecretName(p.Name))
		}
		if err := a.secrets.Set(r.Context(), secretName, body.APIKey, "global"); err != nil {
			respondError(w, r,http.StatusInternalServerError, fmt.Sprintf("store API key: %v", err))
			return
		}
		p.SecretKeyRef = secretName
	}

	id, err := a.repo.Create(r.Context(), p)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	p.ID = id
	respondJSON(w, r,http.StatusCreated, p)
}

// updateBody extends the provider with an optional raw API key for updates.
// IsEnabled is a *bool to distinguish "not sent" (nil) from "sent as false".
type updateBody struct {
	repo.Provider
	APIKey    string `json:"api_key"`
	IsEnabledPtr *bool `json:"is_enabled"`
}

func (a *ProviderAPI) update(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	var body updateBody
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing provider to merge fields.
	existing, err := a.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "provider not found")
		return
	}

	// Apply updates only for non-empty fields, preserving existing values.
	if body.Name != "" {
		existing.Name = body.Name
	}
	if body.Kind != "" {
		existing.Kind = body.Kind
	}
	if body.BaseURL != "" {
		existing.BaseURL = body.BaseURL
	}
	if body.Models != "" {
		existing.Models = body.Models
	}
	if body.Metadata != "" {
		existing.Metadata = body.Metadata
	}
	if body.IsEnabledPtr != nil {
		existing.IsEnabled = *body.IsEnabledPtr
	}

	// If a new API key was provided, store/update it in secrets.
	if body.APIKey != "" && a.secrets != nil {
		secretName := existing.SecretKeyRef
		if secretName == "" {
			secretName = fmt.Sprintf("provider-%s-key", sanitizeSecretName(existing.Name))
		}
		if err := a.secrets.Set(r.Context(), secretName, body.APIKey, "global"); err != nil {
			respondError(w, r,http.StatusInternalServerError, fmt.Sprintf("store API key: %v", err))
			return
		}
		existing.SecretKeyRef = secretName
	}

	if err := a.repo.Update(r.Context(), id, existing); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	existing.ID = id
	respondJSON(w, r,http.StatusOK, existing)
}

func (a *ProviderAPI) remove(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	if err := a.repo.Delete(r.Context(), id); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ProviderAPI) setDefault(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	if err := a.repo.SetDefault(r.Context(), id); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, map[string]string{"status": "ok", "default_id": id})
}

// testConnection validates that a provider's credentials and endpoint work
// by attempting model discovery.
func (a *ProviderAPI) testConnection(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	p, err := a.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "provider not found")
		return
	}

	// Resolve the API key.
	apiKey := ""
	if p.SecretKeyRef != "" && a.secrets != nil {
		apiKey, err = a.secrets.Get(r.Context(), p.SecretKeyRef, "global")
		if err != nil {
			respondJSON(w, r,http.StatusOK, map[string]any{
				"success": false,
				"error":   fmt.Sprintf("Cannot resolve API key %q: %v", p.SecretKeyRef, err),
			})
			return
		}
	}

	// Attempt model discovery as the connection test.
	models, err := provider.DiscoverModels(r.Context(), p.Kind, p.BaseURL, apiKey)
	if err != nil {
		respondJSON(w, r,http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Update the provider's model list on successful discovery.
	modelsJSON, _ := json.Marshal(models)
	p.Models = string(modelsJSON)
	_ = a.repo.Update(r.Context(), id, p)

	resp := map[string]any{
		"success":     true,
		"model_count": len(models),
		"models":      models,
	}
	if p.Kind == "custom" && len(models) == 0 {
		resp["message"] = "Auto-discovery found no models. You can add models manually in the provider settings."
	}
	respondJSON(w, r, http.StatusOK, resp)
}

// managedBaseURL returns the fixed base URL for known provider kinds.
func managedBaseURL(kind string) string {
	switch kind {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	case "google":
		return "https://generativelanguage.googleapis.com"
	default:
		return ""
	}
}

// sanitizeSecretName creates a safe key name from a provider name.
func sanitizeSecretName(name string) string {
	result := make([]byte, 0, len(name))
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '-')
		}
	}
	if len(result) == 0 {
		return "provider-key"
	}
	return string(result)
}
