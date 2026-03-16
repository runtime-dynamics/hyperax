package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// WorkspaceAPI handles REST endpoints for workspaces.
type WorkspaceAPI struct {
	repo repo.WorkspaceRepo
}

func NewWorkspaceAPI(r repo.WorkspaceRepo) *WorkspaceAPI {
	return &WorkspaceAPI{repo: r}
}

func (a *WorkspaceAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.list)
	r.Post("/", a.create)
	r.Get("/{name}", a.get)
	r.Delete("/{name}", a.remove)
	return r
}

func (a *WorkspaceAPI) list(w http.ResponseWriter, r *http.Request) {
	workspaces, err := a.repo.ListWorkspaces(r.Context())
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, workspaces)
}

func (a *WorkspaceAPI) get(w http.ResponseWriter, r *http.Request) {
	name := urlParam(r, "name")
	ws, err := a.repo.GetWorkspace(r.Context(), name)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found")
		return
	}
	respondJSON(w, r, http.StatusOK, ws)
}

func (a *WorkspaceAPI) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		RootPath string `json:"root_path"`
		Metadata string `json:"metadata"`
	}
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.RootPath == "" {
		respondError(w, r, http.StatusBadRequest, "name and root_path are required")
		return
	}

	ws := &types.WorkspaceInfo{
		Name:     body.Name,
		RootPath: body.RootPath,
		Metadata: body.Metadata,
	}
	if err := a.repo.CreateWorkspace(r.Context(), ws); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, ws)
}

func (a *WorkspaceAPI) remove(w http.ResponseWriter, r *http.Request) {
	name := urlParam(r, "name")
	if err := a.repo.DeleteWorkspace(r.Context(), name); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
