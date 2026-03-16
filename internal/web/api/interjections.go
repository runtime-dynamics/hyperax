package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/pkg/types"
)

// InterjectionAPI handles REST endpoints for the Andon Cord system.
type InterjectionAPI struct {
	mgr *interject.Manager
}

func NewInterjectionAPI(mgr *interject.Manager) *InterjectionAPI {
	return &InterjectionAPI{mgr: mgr}
}

func (a *InterjectionAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/active", a.listActive)
	r.Get("/active/{scope}", a.listActiveByScope)
	r.Get("/{id}", a.get)
	r.Post("/halt", a.halt)
	r.Post("/{id}/resolve", a.resolve)
	r.Get("/history/{scope}", a.history)
	r.Get("/safemode", a.safeModeStatus)
	return r
}

func (a *InterjectionAPI) listActive(w http.ResponseWriter, r *http.Request) {
	active, err := a.mgr.GetAllActive(r.Context())
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, active)
}

func (a *InterjectionAPI) listActiveByScope(w http.ResponseWriter, r *http.Request) {
	scope := urlParam(r, "scope")
	active, err := a.mgr.GetActive(r.Context(), scope)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, active)
}

func (a *InterjectionAPI) get(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	ij, err := a.mgr.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "interjection not found")
		return
	}
	respondJSON(w, r,http.StatusOK, ij)
}

func (a *InterjectionAPI) halt(w http.ResponseWriter, r *http.Request) {
	var ij types.Interjection
	if err := decodeBody(r, &ij); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if ij.Scope == "" || ij.Severity == "" || ij.Source == "" || ij.Reason == "" {
		respondError(w, r,http.StatusBadRequest, "scope, severity, source, and reason are required")
		return
	}
	id, err := a.mgr.Halt(r.Context(), &ij)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusCreated, map[string]string{"id": id})
}

func (a *InterjectionAPI) resolve(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	var action types.ResolutionAction
	if err := decodeBody(r, &action); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	action.InterjectionID = id
	if err := a.mgr.Resolve(r.Context(), &action); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, map[string]string{"status": "resolved"})
}

func (a *InterjectionAPI) history(w http.ResponseWriter, r *http.Request) {
	scope := urlParam(r, "scope")
	limit := queryInt(r, "limit", 50)
	history, err := a.mgr.GetHistory(r.Context(), scope, limit)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, history)
}

func (a *InterjectionAPI) safeModeStatus(w http.ResponseWriter, r *http.Request) {
	states := a.mgr.SafeMode().GetAllStates()
	halted := a.mgr.SafeMode().HaltedScopes()
	respondJSON(w, r,http.StatusOK, map[string]any{
		"halted_scopes": halted,
		"states":        states,
	})
}
