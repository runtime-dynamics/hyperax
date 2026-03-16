package mcp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestAuthenticator_PublishesTokenValidatedEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
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
	auth.SetEventBus(bus)

	// Subscribe to token events.
	sub := bus.Subscribe("test-sub", func(e types.NervousEvent) bool {
		return e.Type == types.EventTokenValidated
	})
	defer bus.Unsubscribe("test-sub")

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")

	ctx, ac := auth.Authenticate(req.Context(), req)
	if ctx == nil || !ac.Authenticated {
		t.Fatal("expected successful authentication")
	}

	// Check that the event was published.
	select {
	case event := <-sub.Ch:
		if event.Type != types.EventTokenValidated {
			t.Errorf("expected token.validated event, got %s", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected token.validated event but none received")
	}
}

func TestAuthenticator_NoEventBus_NoPublish(t *testing.T) {
	tokenRepo := &mockTokenRepo{
		tokens: []*types.MCPToken{
			{
				ID:             "tok1",
				AgentID:        "p1",
				TokenHash:      "hash:my-secret-token",
				ClearanceLevel: 1,
			},
		},
	}
	logger := slog.Default()
	auth := NewAuthenticator(tokenRepo, nil, logger)
	// No SetEventBus call.

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")

	// Should not panic with nil bus.
	ctx, ac := auth.Authenticate(req.Context(), req)
	if ctx == nil || !ac.Authenticated {
		t.Fatal("expected successful authentication even without EventBus")
	}
}

func TestExtractBearerToken_CaseInsensitive(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"bearer mytoken", "mytoken"},
		{"BEARER mytoken", "mytoken"},
		{"BeArEr mytoken", "mytoken"},
		{"Bearer mytoken", "mytoken"},
	}

	for _, tt := range tests {
		req := &http.Request{Header: http.Header{}}
		req.Header.Set("Authorization", tt.header)
		got := extractBearerToken(req)
		if got != tt.want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}
