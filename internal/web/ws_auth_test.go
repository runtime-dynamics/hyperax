package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hyperax/hyperax/internal/auth"
	"github.com/hyperax/hyperax/internal/nervous"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testJWTIssuer(t *testing.T) *auth.TokenIssuer {
	t.Helper()
	dir := t.TempDir()
	ti, err := auth.NewTokenIssuer(auth.TokenIssuerConfig{
		DataDir: dir,
		TTL:     5 * time.Minute,
		Issuer:  "test-hyperax",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	return ti
}

func testWSHub(t *testing.T, issuer *auth.TokenIssuer) *WSHub {
	t.Helper()
	bus := nervous.NewEventBus(256)
	hub := NewWSHub(bus, nil, testLogger())
	if issuer != nil {
		hub.SetJWTIssuer(issuer)
		hub.disableLoopbackExempt = true // tests run on localhost, force JWT checks
	}
	return hub
}

// TestWSUpgrade_NoAuth_JWTDisabled verifies that WebSocket upgrade succeeds
// when no JWT issuer is configured (backwards compatibility).
func TestWSUpgrade_NoAuth_JWTDisabled(t *testing.T) {
	hub := testWSHub(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			t.Logf("cleanup: failed to close websocket: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
}

// TestWSUpgrade_MissingJWT_Rejected verifies that WebSocket upgrade is rejected
// with 401 when JWT is required but not provided.
func TestWSUpgrade_MissingJWT_Rejected(t *testing.T) {
	issuer := testJWTIssuer(t)
	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail, got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response, got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestWSUpgrade_InvalidJWT_Rejected verifies that WebSocket upgrade is rejected
// with 401 when an invalid JWT is provided.
func TestWSUpgrade_InvalidJWT_Rejected(t *testing.T) {
	issuer := testJWTIssuer(t)
	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=invalid.jwt.token"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail, got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response, got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestWSUpgrade_ValidJWT_QueryParam verifies that WebSocket upgrade succeeds
// when a valid JWT is provided via the ?token query parameter.
func TestWSUpgrade_ValidJWT_QueryParam(t *testing.T) {
	issuer := testJWTIssuer(t)
	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	claims := &auth.Claims{
		PersonaID:      "agent-1",
		ClearanceLevel: 1,
		Scopes:         []string{"events:subscribe"},
	}
	token, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			t.Logf("cleanup: failed to close websocket: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
}

// TestWSUpgrade_ValidJWT_AuthHeader verifies that WebSocket upgrade succeeds
// when a valid JWT is provided via the Authorization: Bearer header.
func TestWSUpgrade_ValidJWT_AuthHeader(t *testing.T) {
	issuer := testJWTIssuer(t)
	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	claims := &auth.Claims{
		PersonaID:      "agent-2",
		ClearanceLevel: 2,
		Scopes:         []string{"*"},
	}
	token, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			t.Logf("cleanup: failed to close websocket: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
}

// TestWSUpgrade_ExpiredJWT_Rejected verifies that WebSocket upgrade is rejected
// when an expired JWT is provided.
func TestWSUpgrade_ExpiredJWT_Rejected(t *testing.T) {
	dir := t.TempDir()
	issuer, err := auth.NewTokenIssuer(auth.TokenIssuerConfig{
		DataDir: dir,
		TTL:     1 * time.Millisecond,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	claims := &auth.Claims{PersonaID: "agent-1", ClearanceLevel: 1}
	token, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Wait for expiry.
	time.Sleep(5 * time.Millisecond)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + token
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail, got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response, got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestWSUpgrade_RotatedKey_Rejected verifies that a JWT signed with the old
// key is rejected after key rotation.
func TestWSUpgrade_RotatedKey_Rejected(t *testing.T) {
	dir := t.TempDir()
	issuer, err := auth.NewTokenIssuer(auth.TokenIssuerConfig{
		DataDir: dir,
		TTL:     5 * time.Minute,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	claims := &auth.Claims{PersonaID: "agent-1", ClearanceLevel: 1}
	oldToken, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Rotate key.
	if err := issuer.RotateKey(dir); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}

	hub := testWSHub(t, issuer)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + oldToken
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail, got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response, got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
