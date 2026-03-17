package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// writeJSON is a test helper that encodes v as JSON to the response writer.
func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to write JSON response: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateModel
// ---------------------------------------------------------------------------

func TestValidateModel_Found(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4o-mini", "o3"}
	if !ValidateModel(models, "gpt-4o") {
		t.Fatal("expected model to be found")
	}
}

func TestValidateModel_NotFound(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4o-mini"}
	if ValidateModel(models, "gpt-5") {
		t.Fatal("expected model not to be found")
	}
}

func TestValidateModel_EmptyList(t *testing.T) {
	if ValidateModel([]string{}, "anything") {
		t.Fatal("expected false for empty model list")
	}
}

func TestValidateModel_CaseSensitive(t *testing.T) {
	models := []string{"GPT-4o"}
	if ValidateModel(models, "gpt-4o") {
		t.Fatal("expected case-sensitive mismatch")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Ollama
// ---------------------------------------------------------------------------

func TestDiscoverModels_Ollama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]interface{}{
			"models": []map[string]string{
				{"name": "llama3.2:latest"},
				{"name": "mistral:latest"},
			},
		})
	}))
	defer srv.Close()

	models, err := DiscoverModels(context.Background(), "ollama", srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	// Sorted order: llama3.2:latest, mistral:latest
	if models[0] != "llama3.2:latest" {
		t.Errorf("expected llama3.2:latest, got %s", models[0])
	}
	if models[1] != "mistral:latest" {
		t.Errorf("expected mistral:latest, got %s", models[1])
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — OpenAI
// ---------------------------------------------------------------------------

func TestDiscoverModels_OpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing or wrong Authorization header: %s", r.Header.Get("Authorization"))
		}
		writeJSON(t, w, map[string]interface{}{
			"data": []map[string]string{
				{"id": "gpt-4o"},
				{"id": "gpt-4o-mini"},
			},
		})
	}))
	defer srv.Close()

	models, err := DiscoverModels(context.Background(), "openai", srv.URL, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Anthropic
// ---------------------------------------------------------------------------

func TestDiscoverModels_Anthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Fatalf("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("missing anthropic-version header")
		}
		writeJSON(t, w, map[string]interface{}{
			"data": []map[string]string{
				{"id": "claude-sonnet-4-20250514"},
				{"id": "claude-opus-4-20250514"},
			},
		})
	}))
	defer srv.Close()

	models, err := DiscoverModels(context.Background(), "anthropic", srv.URL, "ant-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	// Sorted: claude-opus before claude-sonnet
	if models[0] != "claude-opus-4-20250514" {
		t.Errorf("expected claude-opus-4-20250514 first (sorted), got %s", models[0])
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Azure
// ---------------------------------------------------------------------------

func TestDiscoverModels_Azure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "2024-02-01" {
			t.Fatalf("missing api-version query param")
		}
		if r.Header.Get("api-key") != "azure-key" {
			t.Fatalf("missing api-key header")
		}
		writeJSON(t, w, map[string]interface{}{
			"data": []map[string]string{
				{"id": "gpt-4o"},
			},
		})
	}))
	defer srv.Close()

	models, err := DiscoverModels(context.Background(), "azure", srv.URL, "azure-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0] != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", models[0])
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Custom (OpenAI-compatible, best-effort)
// ---------------------------------------------------------------------------

func TestDiscoverModels_Custom_FallbackOnFailure(t *testing.T) {
	// When the endpoint is unreachable, custom discovery falls back to empty
	// without returning an error.
	models, err := DiscoverModels(context.Background(), "custom", "http://127.0.0.1:1", "")
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models on fallback, got %d", len(models))
	}
}

func TestDiscoverModels_Custom_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("expected /models path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintln(w, `{"data": [{"id": "custom-model-1"}, {"id": "custom-model-2"}]}`); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	models, err := DiscoverModels(context.Background(), "custom", ts.URL, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0] != "custom-model-1" || models[1] != "custom-model-2" {
		t.Errorf("unexpected models: %v", models)
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Unsupported kind
// ---------------------------------------------------------------------------

func TestDiscoverModels_UnsupportedKind(t *testing.T) {
	_, err := DiscoverModels(context.Background(), "unknown", "http://example.com", "")
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — HTTP error responses
// ---------------------------------------------------------------------------

func TestDiscoverModels_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error": "invalid api key"}`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer srv.Close()

	_, err := DiscoverModels(context.Background(), "openai", srv.URL, "bad-key")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Connection failure
// ---------------------------------------------------------------------------

func TestDiscoverModels_ConnectionFailure(t *testing.T) {
	_, err := DiscoverModels(context.Background(), "ollama", "http://127.0.0.1:1", "")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Empty model list from provider
// ---------------------------------------------------------------------------

func TestDiscoverModels_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"models": []map[string]string{},
		})
	}))
	defer srv.Close()

	models, err := DiscoverModels(context.Background(), "ollama", srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Malformed JSON
// ---------------------------------------------------------------------------

func TestDiscoverModels_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`not json`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer srv.Close()

	_, err := DiscoverModels(context.Background(), "openai", srv.URL, "key")
	if err == nil {
		t.Fatal("expected parse error for malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels — Trailing slash in baseURL
// ---------------------------------------------------------------------------

func TestDiscoverModels_TrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"data": []map[string]string{{"id": "model-1"}},
		})
	}))
	defer srv.Close()

	// Pass baseURL with trailing slash — should still work.
	models, err := DiscoverModels(context.Background(), "openai", srv.URL+"/", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
}
