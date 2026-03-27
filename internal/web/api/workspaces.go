package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	r.Get("/browse", a.browse)
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
	if body.RootPath == "" {
		respondError(w, r, http.StatusBadRequest, "root_path is required")
		return
	}
	if body.Name == "" {
		body.Name = filepath.Base(body.RootPath)
	}

	abs, err := filepath.Abs(body.RootPath)
	if err != nil {
		abs = body.RootPath
	}
	hash := sha256.Sum256([]byte(body.Name + "\x00" + abs))
	wsID := "ws-" + hex.EncodeToString(hash[:8])

	ws := &types.WorkspaceInfo{
		ID:       wsID,
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

// browse lists directories at an absolute path so the UI can navigate the
// filesystem to select a workspace root. Query param: ?path=/some/dir
// Defaults to the user's home directory.
func (a *WorkspaceAPI) browse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			dir = "/"
		} else {
			dir = home
		}
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid path")
		return
	}

	info, err := os.Stat(abs)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "path not found")
		return
	}
	if !info.IsDir() {
		respondError(w, r, http.StatusBadRequest, "path is not a directory")
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "failed to read directory")
		return
	}

	type dirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		IsGit bool   `json:"is_git,omitempty"`
	}

	items := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(abs, e.Name())
		de := dirEntry{
			Name:  e.Name(),
			Path:  full,
			IsDir: e.IsDir(),
		}
		if e.IsDir() {
			if _, gitErr := os.Stat(filepath.Join(full, ".git")); gitErr == nil {
				de.IsGit = true
			}
		}
		items = append(items, de)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})

	respondJSON(w, r, http.StatusOK, map[string]any{
		"current_path": abs,
		"parent":       filepath.Dir(abs),
		"entries":      items,
		"count":        len(items),
	})
}

func (a *WorkspaceAPI) remove(w http.ResponseWriter, r *http.Request) {
	name := urlParam(r, "name")
	if err := a.repo.DeleteWorkspace(r.Context(), name); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
