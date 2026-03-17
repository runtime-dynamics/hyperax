package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testIssuer(t *testing.T) *TokenIssuer {
	t.Helper()
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{
		DataDir: dir,
		TTL:     5 * time.Minute,
		Issuer:  "test-hyperax",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	return ti
}

// --- Key Management Tests ---

func TestNewTokenIssuer_GeneratesKey(t *testing.T) {
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	// Key file should exist.
	keyPath := filepath.Join(dir, keyFileName)
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if len(data) != ed25519.PrivateKeySize {
		t.Fatalf("key file size = %d, want %d", len(data), ed25519.PrivateKeySize)
	}

	// Public key should be populated.
	if ti.PublicKey() == nil {
		t.Fatal("public key is nil")
	}
}

func TestNewTokenIssuer_LoadsExistingKey(t *testing.T) {
	dir := t.TempDir()

	// Generate first issuer to persist key.
	ti1, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer (first): %v", err)
	}
	pub1 := ti1.PublicKey()

	// Create second issuer — should load same key.
	ti2, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer (second): %v", err)
	}
	pub2 := ti2.PublicKey()

	if !pub1.Equal(pub2) {
		t.Fatal("second issuer loaded different key")
	}
}

func TestTokenIssuer_RotateKey(t *testing.T) {
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	oldPub := ti.PublicKey()

	// Issue a token with the old key.
	claims := &Claims{PersonaID: "agent-1", ClearanceLevel: 1}
	oldToken, err := ti.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Rotate.
	if err := ti.RotateKey(dir); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	newPub := ti.PublicKey()

	if oldPub.Equal(newPub) {
		t.Fatal("key did not change after rotation")
	}

	// Old token should fail validation with new key.
	_, err = ti.Validate(oldToken)
	if err != ErrSignatureInvalid {
		t.Fatalf("old token after rotation: got err=%v, want ErrSignatureInvalid", err)
	}
}

func TestNewTokenIssuer_DefaultTTL(t *testing.T) {
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	if ti.ttl != DefaultTokenTTL {
		t.Fatalf("ttl = %v, want %v", ti.ttl, DefaultTokenTTL)
	}
}

func TestNewTokenIssuer_DefaultIssuer(t *testing.T) {
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{DataDir: dir}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	if ti.issuer != "hyperax" {
		t.Fatalf("issuer = %q, want %q", ti.issuer, "hyperax")
	}
}

// --- Token Issuance & Validation Tests ---

func TestIssueAndValidate(t *testing.T) {
	ti := testIssuer(t)

	claims := &Claims{
		PersonaID:      "postmaster",
		ClearanceLevel: 2,
		Scopes:         []string{"tools:read", "events:subscribe"},
		Delegations:    []string{"agent-1", "agent-2"},
	}

	token, err := ti.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Token should have 3 dot-separated parts.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}

	// Validate should succeed.
	got, err := ti.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if got.PersonaID != "postmaster" {
		t.Errorf("PersonaID = %q, want %q", got.PersonaID, "postmaster")
	}
	if got.ClearanceLevel != 2 {
		t.Errorf("ClearanceLevel = %d, want %d", got.ClearanceLevel, 2)
	}
	if got.Iss != "test-hyperax" {
		t.Errorf("Iss = %q, want %q", got.Iss, "test-hyperax")
	}
	if got.Sub != "postmaster" {
		t.Errorf("Sub = %q, want %q", got.Sub, "postmaster")
	}
	if len(got.Scopes) != 2 {
		t.Errorf("Scopes count = %d, want 2", len(got.Scopes))
	}
	if len(got.Delegations) != 2 {
		t.Errorf("Delegations count = %d, want 2", len(got.Delegations))
	}
	if got.Exp <= got.Iat {
		t.Error("Exp should be after Iat")
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	dir := t.TempDir()
	ti, err := NewTokenIssuer(TokenIssuerConfig{
		DataDir: dir,
		TTL:     1 * time.Millisecond, // Extremely short TTL.
	}, testLogger())
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	claims := &Claims{PersonaID: "agent-1", ClearanceLevel: 1}
	token, err := ti.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Wait for expiry.
	time.Sleep(5 * time.Millisecond)

	_, err = ti.Validate(token)
	if err != ErrTokenExpired {
		t.Fatalf("got err=%v, want ErrTokenExpired", err)
	}
}

func TestValidate_MalformedToken(t *testing.T) {
	ti := testIssuer(t)

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"single_part", "abc"},
		{"two_parts", "abc.def"},
		{"four_parts", "a.b.c.d"},
		{"invalid_base64_header", "!!!.def.ghi"},
		{"invalid_base64_claims", base64URLEncode([]byte(`{"alg":"EdDSA","typ":"JWT"}`)) + ".!!!.ghi"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ti.Validate(tc.token)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestValidate_WrongAlgorithm(t *testing.T) {
	ti := testIssuer(t)

	// Forge a token with alg=HS256.
	header := base64URLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := base64URLEncode([]byte(`{"sub":"test","exp":9999999999}`))
	sig := base64URLEncode([]byte("fakesig"))
	token := header + "." + claims + "." + sig

	_, err := ti.Validate(token)
	if err == nil || !strings.Contains(err.Error(), "unexpected signing algorithm") {
		t.Fatalf("got err=%v, want ErrAlgorithmMismatch", err)
	}
}

func TestValidate_TamperedClaims(t *testing.T) {
	ti := testIssuer(t)

	claims := &Claims{PersonaID: "agent-1", ClearanceLevel: 1}
	token, err := ti.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Tamper with the claims part.
	parts := strings.Split(token, ".")
	tamperedClaims := base64URLEncode([]byte(`{"sub":"agent-1","persona_id":"agent-1","clearance_level":99,"iat":1,"exp":9999999999,"nbf":1,"iss":"test-hyperax"}`))
	tampered := parts[0] + "." + tamperedClaims + "." + parts[2]

	_, err = ti.Validate(tampered)
	if err != ErrSignatureInvalid {
		t.Fatalf("got err=%v, want ErrSignatureInvalid", err)
	}
}

// --- Claims Tests ---

func TestClaims_Valid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		claims  Claims
		wantErr error
	}{
		{
			name:    "valid",
			claims:  Claims{Iat: now.Unix(), Exp: now.Add(5 * time.Minute).Unix(), Nbf: now.Unix()},
			wantErr: nil,
		},
		{
			name:    "expired",
			claims:  Claims{Iat: now.Add(-10 * time.Minute).Unix(), Exp: now.Add(-5 * time.Minute).Unix()},
			wantErr: ErrTokenExpired,
		},
		{
			name:    "not_yet_valid",
			claims:  Claims{Iat: now.Unix(), Exp: now.Add(5 * time.Minute).Unix(), Nbf: now.Add(1 * time.Minute).Unix()},
			wantErr: ErrTokenNotYetValid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.claims.Valid(now)
			if err != tc.wantErr {
				t.Fatalf("Valid() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestClaims_HasScope(t *testing.T) {
	c := Claims{Scopes: []string{"tools:read", "events:subscribe"}}

	if !c.HasScope("tools:read") {
		t.Error("should have tools:read")
	}
	if c.HasScope("tools:write") {
		t.Error("should not have tools:write")
	}

	// Wildcard scope.
	cWild := Claims{Scopes: []string{"*"}}
	if !cWild.HasScope("anything") {
		t.Error("wildcard should match anything")
	}
}

// --- ExtractJWT Tests ---

func TestExtractJWT_AuthorizationHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	r.Header.Set("Authorization", "Bearer my-jwt-token")

	token, err := ExtractJWT(r)
	if err != nil {
		t.Fatalf("ExtractJWT: %v", err)
	}
	if token != "my-jwt-token" {
		t.Fatalf("token = %q, want %q", token, "my-jwt-token")
	}
}

func TestExtractJWT_QueryParam(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/events?token=query-jwt", nil)

	token, err := ExtractJWT(r)
	if err != nil {
		t.Fatalf("ExtractJWT: %v", err)
	}
	if token != "query-jwt" {
		t.Fatalf("token = %q, want %q", token, "query-jwt")
	}
}

func TestExtractJWT_HeaderTakesPrecedence(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/events?token=query-jwt", nil)
	r.Header.Set("Authorization", "Bearer header-jwt")

	token, err := ExtractJWT(r)
	if err != nil {
		t.Fatalf("ExtractJWT: %v", err)
	}
	if token != "header-jwt" {
		t.Fatalf("token = %q, want %q (header should take precedence)", token, "header-jwt")
	}
}

func TestExtractJWT_Missing(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/events", nil)

	_, err := ExtractJWT(r)
	if err != ErrMissingToken {
		t.Fatalf("got err=%v, want ErrMissingToken", err)
	}
}

func TestExtractJWT_AuthorizationWithoutBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err := ExtractJWT(r)
	if err != ErrMissingToken {
		t.Fatalf("got err=%v, want ErrMissingToken for non-Bearer auth", err)
	}
}

// --- Token Endpoint Tests ---

// mockValidator implements TokenValidator for testing.
type mockValidator struct {
	claims *Claims
	err    error
}

func (m *mockValidator) ValidateMCPToken(token string) (*Claims, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.claims, nil
}

func TestHandleTokenEndpoint_Success(t *testing.T) {
	ti := testIssuer(t)
	validator := &mockValidator{
		claims: &Claims{
			PersonaID:      "agent-1",
			ClearanceLevel: 1,
			Scopes:         []string{"tools:read"},
		},
	}

	handler := HandleTokenEndpoint(ti, validator, testLogger())

	body, err := json.Marshal(TokenRequest{MCPToken: "valid-mcp-token"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("response token is empty")
	}
	if resp.ExpiresIn != 300 {
		t.Fatalf("expires_in = %d, want 300", resp.ExpiresIn)
	}

	// Validate the returned token.
	claims, err := ti.Validate(resp.Token)
	if err != nil {
		t.Fatalf("validate returned token: %v", err)
	}
	if claims.PersonaID != "agent-1" {
		t.Fatalf("PersonaID = %q, want %q", claims.PersonaID, "agent-1")
	}
}

func TestHandleTokenEndpoint_InvalidMCPToken(t *testing.T) {
	ti := testIssuer(t)
	validator := &mockValidator{err: ErrInvalidMCPToken}

	handler := HandleTokenEndpoint(ti, validator, testLogger())

	body, err := json.Marshal(TokenRequest{MCPToken: "bad-token"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleTokenEndpoint_MissingMCPToken(t *testing.T) {
	ti := testIssuer(t)
	validator := &mockValidator{}

	handler := HandleTokenEndpoint(ti, validator, testLogger())

	body, err := json.Marshal(TokenRequest{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleTokenEndpoint_ValidatorNil(t *testing.T) {
	ti := testIssuer(t)

	handler := HandleTokenEndpoint(ti, nil, testLogger())

	body, err := json.Marshal(TokenRequest{MCPToken: "any"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleTokenEndpoint_WrongMethod(t *testing.T) {
	ti := testIssuer(t)

	handler := HandleTokenEndpoint(ti, nil, testLogger())

	r := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleTokenEndpoint_InvalidBody(t *testing.T) {
	ti := testIssuer(t)
	validator := &mockValidator{}

	handler := HandleTokenEndpoint(ti, validator, testLogger())

	r := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- Base64 URL Encoding Tests ---

func TestBase64URLRoundTrip(t *testing.T) {
	inputs := []string{
		"hello world",
		`{"alg":"EdDSA","typ":"JWT"}`,
		string(make([]byte, 256)),
	}
	for i, input := range inputs {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			encoded := base64URLEncode([]byte(input))
			decoded, err := base64URLDecode(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if string(decoded) != input {
				t.Fatalf("roundtrip mismatch")
			}
		})
	}
}
