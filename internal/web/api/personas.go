package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/repo"
)

// PersonaAPI handles REST endpoints for agent personas.
type PersonaAPI struct {
	repo repo.PersonaRepo
}

func NewPersonaAPI(r repo.PersonaRepo) *PersonaAPI {
	return &PersonaAPI{repo: r}
}

func (a *PersonaAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.list)
	r.Post("/", a.create)
	r.Get("/{id}", a.get)
	r.Put("/{id}", a.update)
	r.Delete("/{id}", a.remove)
	return r
}

func (a *PersonaAPI) list(w http.ResponseWriter, r *http.Request) {
	personas, err := a.repo.List(r.Context())
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, personas)
}

func (a *PersonaAPI) get(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	persona, err := a.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "persona not found")
		return
	}
	respondJSON(w, r,http.StatusOK, persona)
}

func (a *PersonaAPI) create(w http.ResponseWriter, r *http.Request) {
	var p repo.Persona
	if err := decodeBody(r, &p); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		respondError(w, r,http.StatusBadRequest, "name is required")
		return
	}
	id, err := a.repo.Create(r.Context(), &p)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	p.ID = id
	respondJSON(w, r,http.StatusCreated, p)
}

func (a *PersonaAPI) update(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	var p repo.Persona
	if err := decodeBody(r, &p); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.repo.Update(r.Context(), id, &p); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	p.ID = id
	respondJSON(w, r,http.StatusOK, p)
}

func (a *PersonaAPI) remove(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	if err := a.repo.Delete(r.Context(), id); err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
