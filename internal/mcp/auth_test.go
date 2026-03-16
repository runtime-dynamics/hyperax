package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// mockTokenRepo implements repo.MCPTokenRepo for testing.
type mockTokenRepo struct {
	tokens []*types.MCPToken
}

func (m *mockTokenRepo) Create(_ context.Context, token *types.MCPToken) error {
	m.tokens = append(m.tokens, token)
	return nil
}

func (m *mockTokenRepo) ValidateToken(_ context.Context, plaintext string) (*types.MCPToken, error) {
	for _, t := range m.tokens {
		if t.TokenHash == "hash:"+plaintext && t.IsValid() {
			return t, nil
		}
	}
	return nil, fmt.Errorf("invalid or expired token")
}

func (m *mockTokenRepo) Revoke(_ context.Context, tokenID string) error {
	for _, t := range m.tokens {
		if t.ID == tokenID {
			now := time.Now()
			t.RevokedAt = &now
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockTokenRepo) ListByAgent(_ context.Context, agentID string) ([]*types.MCPToken, error) {
	var out []*types.MCPToken
	for _, t := range m.tokens {
		if t.AgentID == agentID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *mockTokenRepo) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockTokenRepo) GetByID(_ context.Context, tokenID string) (*types.MCPToken, error) {
	for _, t := range m.tokens {
		if t.ID == tokenID {
			return t, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// mockConfigRepo implements repo.ConfigRepo for testing.
type mockConfigRepo struct {
	values map[string]string
}

func (m *mockConfigRepo) GetValue(_ context.Context, key string, _ types.ConfigScope) (string, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockConfigRepo) SetValue(_ context.Context, _, _ string, _ types.ConfigScope, _ string) error {
	return nil
}

func (m *mockConfigRepo) GetKeyMeta(_ context.Context, _ string) (*types.ConfigKeyMeta, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockConfigRepo) ListKeys(_ context.Context) ([]types.ConfigKeyMeta, error) {
	return nil, nil
}

func (m *mockConfigRepo) ListValues(_ context.Context, _ types.ConfigScope) ([]types.ConfigValue, error) {
	return nil, nil
}

func (m *mockConfigRepo) GetHistory(_ context.Context, _ string, _ int) ([]types.ConfigChange, error) {
	return nil, nil
}

func (m *mockConfigRepo) UpsertKeyMeta(_ context.Context, _ *types.ConfigKeyMeta) error {
	return nil
}

func TestAuthenticator_NoTokenRepo(t *testing.T) {
	logger := slog.Default()
	auth := NewAuthenticator(nil, nil, logger)

	req := httptest.NewRequest("POST", "/mcp", nil)
	ctx, ac := auth.Authenticate(req.Context(), req)
	if ctx == nil {
		t.Fatal("expected non-nil context when token repo is nil")
	}
	if ac.Authenticated {
		t.Error("expected unauthenticated when no token repo")
	}
}

func TestAuthenticator_ValidToken(t *testing.T) {
	tokenRepo := &mockTokenRepo{
		tokens: []*types.MCPToken{
			{
				ID:             "tok1",
				AgentID:        "p1",
				TokenHash:      "hash:my-secret-token",
				ClearanceLevel: 2,
				Scopes:         []string{"admin"},
			},
		},
	}
	logger := slog.Default()
	auth := NewAuthenticator(tokenRepo, nil, logger)

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")

	ctx, ac := auth.Authenticate(req.Context(), req)
	if ctx == nil {
		t.Fatal("expected non-nil context for valid token")
	}
	if !ac.Authenticated {
		t.Error("expected authenticated")
	}
	if ac.PersonaID != "p1" {
		t.Errorf("expected persona p1, got %s", ac.PersonaID)
	}
	if ac.ClearanceLevel != 2 {
		t.Errorf("expected clearance 2, got %d", ac.ClearanceLevel)
	}
}

func TestAuthenticator_InvalidToken(t *testing.T) {
	tokenRepo := &mockTokenRepo{
		tokens: []*types.MCPToken{
			{
				ID:        "tok1",
				AgentID:   "p1",
				TokenHash: "hash:correct-token",
			},
		},
	}
	logger := slog.Default()
	auth := NewAuthenticator(tokenRepo, nil, logger)

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	ctx, _ := auth.Authenticate(req.Context(), req)
	if ctx != nil {
		t.Error("expected nil context for invalid token")
	}
}

func TestAuthenticator_NoTokenAuthNotRequired(t *testing.T) {
	tokenRepo := &mockTokenRepo{}
	configRepo := &mockConfigRepo{values: map[string]string{}}
	logger := slog.Default()
	auth := NewAuthenticator(tokenRepo, configRepo, logger)

	req := httptest.NewRequest("POST", "/mcp", nil)
	// No Authorization header, auth.required not set.
	ctx, ac := auth.Authenticate(req.Context(), req)
	if ctx == nil {
		t.Fatal("expected non-nil context when auth not required")
	}
	if ac.Authenticated {
		t.Error("expected unauthenticated")
	}
}

func TestAuthenticator_NoTokenAuthRequired(t *testing.T) {
	tokenRepo := &mockTokenRepo{}
	configRepo := &mockConfigRepo{values: map[string]string{"auth.required": "true"}}
	logger := slog.Default()
	auth := NewAuthenticator(tokenRepo, configRepo, logger)

	req := httptest.NewRequest("POST", "/mcp", nil)
	// No Authorization header, but auth.required = true.
	ctx, _ := auth.Authenticate(req.Context(), req)
	if ctx != nil {
		t.Error("expected nil context when auth required and no token")
	}
}

func TestAuthFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	ac := AuthFromContext(ctx)
	if ac.Authenticated {
		t.Error("expected unauthenticated for empty context")
	}
	if ac.ClearanceLevel != 0 {
		t.Errorf("expected clearance 0, got %d", ac.ClearanceLevel)
	}
}

func TestAuthFromContext_WithAuth(t *testing.T) {
	ac := types.AuthContext{
		PersonaID:      "p1",
		TokenID:        "tok1",
		ClearanceLevel: 2,
		Scopes:         []string{"admin"},
		Authenticated:  true,
	}
	ctx := withAuthContext(context.Background(), ac)
	got := AuthFromContext(ctx)
	if !got.Authenticated {
		t.Error("expected authenticated")
	}
	if got.PersonaID != "p1" {
		t.Errorf("expected persona p1, got %s", got.PersonaID)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"Bearer  padded ", "padded"},
		{"bearer abc123", "abc123"},  // case-insensitive per RFC 7235
		{"BEARER abc123", "abc123"},  // all caps
		{"BeArEr abc123", "abc123"},  // mixed case
		{"Basic abc123", ""},         // wrong scheme
		{"", ""},                     // empty
		{"Bearer", ""},              // no token (too short)
		{"bear", ""},                // too short
	}

	for _, tt := range tests {
		req := &http.Request{Header: http.Header{}}
		if tt.header != "" {
			req.Header.Set("Authorization", tt.header)
		}
		got := extractBearerToken(req)
		if got != tt.want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}
