package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockTokenRepo for handler tests.
type mockTokenRepo struct {
	tokens []*types.MCPToken
}

func (m *mockTokenRepo) Create(_ context.Context, token *types.MCPToken) error {
	m.tokens = append(m.tokens, token)
	return nil
}

func (m *mockTokenRepo) ValidateToken(_ context.Context, plaintext string) (*types.MCPToken, error) {
	return nil, fmt.Errorf("not implemented in mock")
}

func (m *mockTokenRepo) Revoke(_ context.Context, tokenID string) error {
	for _, t := range m.tokens {
		if t.ID == tokenID && t.RevokedAt == nil {
			now := time.Now()
			t.RevokedAt = &now
			return nil
		}
	}
	return fmt.Errorf("token %q not found or already revoked", tokenID)
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
	return nil, fmt.Errorf("token %q not found", tokenID)
}

// mockAgentRepo for handler tests.
type mockAgentRepo struct {
	agents map[string]*repo.Agent
}

func (m *mockAgentRepo) Create(_ context.Context, a *repo.Agent) (string, error) {
	m.agents[a.ID] = a
	return a.ID, nil
}

func (m *mockAgentRepo) Get(_ context.Context, id string) (*repo.Agent, error) {
	if a, ok := m.agents[id]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent %q not found", id)
}

func (m *mockAgentRepo) GetByName(_ context.Context, _ string) (*repo.Agent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockAgentRepo) List(_ context.Context) ([]*repo.Agent, error) {
	return nil, nil
}

func (m *mockAgentRepo) ListByPersona(_ context.Context, _ string) ([]*repo.Agent, error) {
	return nil, nil
}

func (m *mockAgentRepo) Update(_ context.Context, _ string, _ *repo.Agent) error {
	return nil
}

func (m *mockAgentRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockAgentRepo) SetAgentError(_ context.Context, agentID, reason string) error {
	a, ok := m.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}
	a.Status = repo.AgentStatusError
	a.StatusReason = reason
	return nil
}

func newTestAuthHandler() (*ConfigHandler, *mockTokenRepo) {
	tokenRepo := &mockTokenRepo{}
	agentRepo := &mockAgentRepo{
		agents: map[string]*repo.Agent{
			"p1": {ID: "p1", Name: "test-agent", ClearanceLevel: 2},
			"p2": {ID: "p2", Name: "low-agent", ClearanceLevel: 0},
		},
	}
	logger := slog.Default()
	h := NewConfigHandler(nil, nil)
	h.SetAuthDeps(tokenRepo, agentRepo, logger)
	return h, tokenRepo
}

func TestCreateMCPToken(t *testing.T) {
	h, tokenRepo := newTestAuthHandler()
	ctx := context.Background()

	params := mustMarshalJSON(t, map[string]any{
		"agent_id":      "p1",
		"label":           "CI token",
		"clearance_level": 1,
		"scopes":          []string{"read", "write"},
		"expires_in":      "7d",
	})

	result, err := h.createMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("createMCPToken: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	// Parse the result to check fields.
	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if resp["plaintext_token"] == nil || resp["plaintext_token"] == "" {
		t.Error("expected plaintext_token in response")
	}
	if resp["token_id"] == nil || resp["token_id"] == "" {
		t.Error("expected token_id in response")
	}
	if resp["warning"] == nil {
		t.Error("expected warning in response")
	}

	// Verify token was stored.
	if len(tokenRepo.tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokenRepo.tokens))
	}
}

func TestCreateMCPToken_ClearanceExceedsPersona(t *testing.T) {
	h, _ := newTestAuthHandler()
	ctx := context.Background()

	params := mustMarshalJSON(t, map[string]any{
		"agent_id":      "p2", // clearance 0
		"clearance_level": 1,    // exceeds persona clearance
	})

	result, err := h.createMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("createMCPToken: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for clearance exceeding persona level")
	}
}

func TestCreateMCPToken_PersonaNotFound(t *testing.T) {
	h, _ := newTestAuthHandler()
	ctx := context.Background()

	params := mustMarshalJSON(t, map[string]any{
		"agent_id": "nonexistent",
	})

	result, err := h.createMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("createMCPToken: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent persona")
	}
}

func TestCreateMCPToken_CrossPersonaClearanceCheck(t *testing.T) {
	h, _ := newTestAuthHandler()

	// Simulate an authenticated caller with clearance 1 trying to create
	// a token for a different persona — should be denied (needs >= 2).
	ac := types.AuthContext{
		PersonaID:      "p2",
		ClearanceLevel: 1,
		Authenticated:  true,
	}
	ctx := context.WithValue(context.Background(), mcp.AuthContextKey(), ac)

	params := mustMarshalJSON(t, map[string]any{
		"agent_id": "p1",
	})

	result, err := h.createMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("createMCPToken: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for insufficient clearance to create for other persona")
	}
}

func TestRevokeMCPToken(t *testing.T) {
	h, tokenRepo := newTestAuthHandler()
	ctx := context.Background()

	// Pre-create a token.
	tokenRepo.tokens = append(tokenRepo.tokens, &types.MCPToken{
		ID:        "tok-to-revoke",
		AgentID:   "p1",
		TokenHash: "hash",
		Scopes:    []string{},
	})

	params := mustMarshalJSON(t, map[string]any{
		"token_id": "tok-to-revoke",
	})

	result, err := h.revokeMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("revokeMCPToken: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	// Verify token was revoked.
	if tokenRepo.tokens[0].RevokedAt == nil {
		t.Error("expected token to be revoked")
	}
}

func TestListMCPTokens(t *testing.T) {
	h, tokenRepo := newTestAuthHandler()
	ctx := context.Background()

	tokenRepo.tokens = append(tokenRepo.tokens,
		&types.MCPToken{ID: "tok1", AgentID: "p1", Label: "first", Scopes: []string{}},
		&types.MCPToken{ID: "tok2", AgentID: "p1", Label: "second", Scopes: []string{}},
		&types.MCPToken{ID: "tok3", AgentID: "p2", Label: "other", Scopes: []string{}},
	)

	params := mustMarshalJSON(t, map[string]any{
		"agent_id": "p1",
	})

	result, err := h.listMCPTokens(ctx, params)
	if err != nil {
		t.Fatalf("listMCPTokens: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	count := resp["count"].(float64)
	if count != 2 {
		t.Errorf("expected 2 tokens for p1, got %v", count)
	}
}

func TestRotateMCPToken(t *testing.T) {
	h, tokenRepo := newTestAuthHandler()
	ctx := context.Background()

	tokenRepo.tokens = append(tokenRepo.tokens, &types.MCPToken{
		ID:             "tok-old",
		AgentID:        "p1",
		TokenHash:      "old-hash",
		Label:          "rotate me",
		ClearanceLevel: 1,
		Scopes:         []string{"read"},
	})

	params := mustMarshalJSON(t, map[string]any{
		"token_id": "tok-old",
	})

	result, err := h.rotateMCPToken(ctx, params)
	if err != nil {
		t.Fatalf("rotateMCPToken: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["old_token_id"] != "tok-old" {
		t.Errorf("expected old_token_id tok-old, got %v", resp["old_token_id"])
	}
	if resp["plaintext_token"] == nil || resp["plaintext_token"] == "" {
		t.Error("expected new plaintext_token")
	}

	// Old token should be revoked.
	if tokenRepo.tokens[0].RevokedAt == nil {
		t.Error("expected old token to be revoked")
	}

	// New token should be added.
	if len(tokenRepo.tokens) != 2 {
		t.Errorf("expected 2 tokens (old revoked + new), got %d", len(tokenRepo.tokens))
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := parseDuration(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("parseDuration(%q) error = %v, want error %v", tt.input, err, tt.err)
			continue
		}
		if !tt.err && got != tt.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
