package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// --- Workspace mock ---

type mockWorkspaceRepo struct {
	workspaces map[string]*types.WorkspaceInfo
}

func (m *mockWorkspaceRepo) WorkspaceExists(_ context.Context, name string) (bool, error) {
	_, ok := m.workspaces[name]
	return ok, nil
}

func (m *mockWorkspaceRepo) ListWorkspaces(_ context.Context) ([]*types.WorkspaceInfo, error) {
	var result []*types.WorkspaceInfo
	for _, ws := range m.workspaces {
		result = append(result, ws)
	}
	return result, nil
}

func (m *mockWorkspaceRepo) GetWorkspace(_ context.Context, name string) (*types.WorkspaceInfo, error) {
	ws, ok := m.workspaces[name]
	if !ok {
		return nil, &notFoundErr{name}
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) CreateWorkspace(_ context.Context, ws *types.WorkspaceInfo) error {
	if m.workspaces == nil {
		m.workspaces = make(map[string]*types.WorkspaceInfo)
	}
	m.workspaces[ws.Name] = ws
	return nil
}

func (m *mockWorkspaceRepo) DeleteWorkspace(_ context.Context, name string) error {
	delete(m.workspaces, name)
	return nil
}

type notFoundErr struct{ id string }

func (e *notFoundErr) Error() string { return "not found: " + e.id }

// --- Provider mock ---

type mockProviderRepo struct {
	providers map[string]*repo.Provider
	counter   int
}

func (m *mockProviderRepo) Create(_ context.Context, p *repo.Provider) (string, error) {
	m.counter++
	p.ID = "prov-" + itoa(m.counter)
	if m.providers == nil {
		m.providers = make(map[string]*repo.Provider)
	}
	m.providers[p.ID] = p
	return p.ID, nil
}

func (m *mockProviderRepo) Get(_ context.Context, id string) (*repo.Provider, error) {
	p, ok := m.providers[id]
	if !ok {
		return nil, &notFoundErr{id}
	}
	return p, nil
}

func (m *mockProviderRepo) GetByName(_ context.Context, name string) (*repo.Provider, error) {
	for _, p := range m.providers {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, &notFoundErr{name}
}

func (m *mockProviderRepo) GetDefault(_ context.Context) (*repo.Provider, error) {
	for _, p := range m.providers {
		if p.IsDefault {
			return p, nil
		}
	}
	return nil, &notFoundErr{"default"}
}

func (m *mockProviderRepo) List(_ context.Context) ([]*repo.Provider, error) {
	var result []*repo.Provider
	for _, p := range m.providers {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockProviderRepo) Update(_ context.Context, id string, p *repo.Provider) error {
	m.providers[id] = p
	return nil
}

func (m *mockProviderRepo) SetDefault(_ context.Context, id string) error {
	for _, p := range m.providers {
		p.IsDefault = false
	}
	if p, ok := m.providers[id]; ok {
		p.IsDefault = true
	}
	return nil
}

func (m *mockProviderRepo) Delete(_ context.Context, id string) error {
	delete(m.providers, id)
	return nil
}

// --- Secret mock ---

type mockSecretRepo struct{}

func (m *mockSecretRepo) Get(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}
func (m *mockSecretRepo) Set(_ context.Context, _, _, _ string) error { return nil }
func (m *mockSecretRepo) Delete(_ context.Context, _, _ string) error { return nil }
func (m *mockSecretRepo) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *mockSecretRepo) SetWithAccess(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockSecretRepo) ListEntries(_ context.Context, _ string) ([]repo.SecretEntry, error) {
	return nil, nil
}
func (m *mockSecretRepo) GetAccessScope(_ context.Context, _, _ string) (string, error) {
	return "global", nil
}
func (m *mockSecretRepo) UpdateAccessScope(_ context.Context, _, _, _ string) error { return nil }

// --- Persona mock ---

type mockPersonaRepo struct {
	personas map[string]*repo.Persona
	counter  int
}

func (m *mockPersonaRepo) Create(_ context.Context, p *repo.Persona) (string, error) {
	m.counter++
	p.ID = "per-" + itoa(m.counter)
	if m.personas == nil {
		m.personas = make(map[string]*repo.Persona)
	}
	m.personas[p.ID] = p
	return p.ID, nil
}

func (m *mockPersonaRepo) Get(_ context.Context, id string) (*repo.Persona, error) {
	p, ok := m.personas[id]
	if !ok {
		return nil, &notFoundErr{id}
	}
	return p, nil
}

func (m *mockPersonaRepo) GetByName(_ context.Context, name string) (*repo.Persona, error) {
	for _, p := range m.personas {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, &notFoundErr{name}
}

func (m *mockPersonaRepo) List(_ context.Context) ([]*repo.Persona, error) {
	var result []*repo.Persona
	for _, p := range m.personas {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockPersonaRepo) Update(_ context.Context, id string, p *repo.Persona) error {
	m.personas[id] = p
	return nil
}

func (m *mockPersonaRepo) Delete(_ context.Context, id string) error {
	delete(m.personas, id)
	return nil
}

func itoa(n int) string {
	return string(rune('0'+n%10)) + ""
}

// --- Tests ---

func TestWorkspaceAPI_CRUD(t *testing.T) {
	mock := &mockWorkspaceRepo{
		workspaces: map[string]*types.WorkspaceInfo{
			"ws-1": {ID: "ws-1", Name: "ws-1", RootPath: "/tmp/ws1"},
		},
	}
	router := NewWorkspaceAPI(mock).Routes()

	// List
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get existing
	req = httptest.NewRequest(http.MethodGet, "/ws-1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}

	// Get not found
	req = httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get not found: expected 404, got %d", rec.Code)
	}

	// Create
	body, err := json.Marshal(map[string]string{"name": "new-ws", "root_path": "/tmp/new"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Create missing fields
	body, err = json.Marshal(map[string]string{"name": ""})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create missing: expected 400, got %d", rec.Code)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/new-ws", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}

func TestProviderAPI_CRUD(t *testing.T) {
	mock := &mockProviderRepo{providers: make(map[string]*repo.Provider)}
	router := NewProviderAPI(mock, &mockSecretRepo{}).Routes()

	// Create
	body, err := json.Marshal(map[string]string{"name": "test-provider", "kind": "openai", "base_url": "https://api.openai.com"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var created repo.Provider
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get
	req = httptest.NewRequest(http.MethodGet, "/"+created.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}

	// Set default
	req = httptest.NewRequest(http.MethodPost, "/"+created.ID+"/default", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set default: expected 200, got %d", rec.Code)
	}

	// Get default
	req = httptest.NewRequest(http.MethodGet, "/default", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get default: expected 200, got %d", rec.Code)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/"+created.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}

func TestPersonaAPI_CRUD(t *testing.T) {
	mock := &mockPersonaRepo{personas: make(map[string]*repo.Persona)}
	router := NewPersonaAPI(mock).Routes()

	// Create
	body, err := json.Marshal(map[string]string{"name": "Test Agent", "role": "developer"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var created repo.Persona
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created persona: %v", err)
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get
	req = httptest.NewRequest(http.MethodGet, "/"+created.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}

	// Update
	body, err = json.Marshal(map[string]string{"name": "Updated Agent", "role": "lead"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req = httptest.NewRequest(http.MethodPut, "/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rec.Code)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/"+created.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}

	// Get after delete → 404
	req = httptest.NewRequest(http.MethodGet, "/"+created.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d", rec.Code)
	}
}
