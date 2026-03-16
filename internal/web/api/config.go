package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigAPI handles REST endpoints for runtime configuration.
type ConfigAPI struct {
	repo repo.ConfigRepo
}

func NewConfigAPI(r repo.ConfigRepo) *ConfigAPI {
	return &ConfigAPI{repo: r}
}

func (a *ConfigAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/keys", a.listKeys)
	r.Get("/values", a.listValues)
	r.Get("/keys/{key}", a.getKey)
	r.Put("/keys/{key}", a.setValue)
	r.Get("/keys/{key}/history", a.getHistory)
	return r
}

// parseScope constructs a ConfigScope from query params.
func parseScope(r *http.Request) types.ConfigScope {
	return types.ConfigScope{
		Type: queryStr(r, "scope", "global"),
		ID:   queryStr(r, "scope_id", ""),
	}
}

func (a *ConfigAPI) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.repo.ListKeys(r.Context())
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, keys)
}

func (a *ConfigAPI) listValues(w http.ResponseWriter, r *http.Request) {
	scope := parseScope(r)
	values, err := a.repo.ListValues(r.Context(), scope)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, values)
}

func (a *ConfigAPI) getKey(w http.ResponseWriter, r *http.Request) {
	key := urlParam(r, "key")
	meta, err := a.repo.GetKeyMeta(r.Context(), key)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "key not found")
		return
	}
	scope := parseScope(r)
	value, _ := a.repo.GetValue(r.Context(), key, scope)
	respondJSON(w, r,http.StatusOK, map[string]any{
		"meta":  meta,
		"value": value,
		"scope": scope,
	})
}

func (a *ConfigAPI) setValue(w http.ResponseWriter, r *http.Request) {
	key := urlParam(r, "key")
	var body struct {
		Value   string `json:"value"`
		Scope   string `json:"scope"`
		ScopeID string `json:"scope_id"`
		Actor   string `json:"actor"`
	}
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Value == "" {
		respondError(w, r,http.StatusBadRequest, "value is required")
		return
	}
	scopeType := body.Scope
	if scopeType == "" {
		scopeType = "global"
	}
	scope := types.ConfigScope{Type: scopeType, ID: body.ScopeID}
	actor := body.Actor
	if actor == "" {
		actor = "api"
	}

	if err := a.repo.SetValue(r.Context(), key, body.Value, scope, actor); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, map[string]string{"status": "ok"})
}

func (a *ConfigAPI) getHistory(w http.ResponseWriter, r *http.Request) {
	key := urlParam(r, "key")
	limit := queryInt(r, "limit", 20)
	history, err := a.repo.GetHistory(r.Context(), key, limit)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, history)
}
