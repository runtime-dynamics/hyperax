// Package auth provides JWT-based authentication for WebSocket connections.
//
// The JWT subsystem uses Ed25519 (EdDSA) for token signing and verification.
// Key pairs are generated on first startup and persisted in the configured
// data directory. Tokens carry ABAC-style claims (persona_id, clearance_level,
// scopes, delegations) and have a short default TTL of 5 minutes.
//
// Token issuance requires a valid MCP token, verified via the TokenValidator
// interface. This interface is implemented by the MCP token authentication
// system (internal/auth/session.go) once available.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/web/render"
)

// Default JWT configuration values.
const (
	// DefaultTokenTTL is the default lifetime for issued JWTs.
	DefaultTokenTTL = 5 * time.Minute

	// keyFileName is the Ed25519 private key file stored in data_dir.
	keyFileName = "jwt_ed25519.key"
)

// Errors returned by the JWT subsystem.
var (
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenMalformed     = errors.New("token malformed")
	ErrSignatureInvalid   = errors.New("signature verification failed")
	ErrAlgorithmMismatch  = errors.New("unexpected signing algorithm")
	ErrMissingToken       = errors.New("missing authentication token")
	ErrInvalidMCPToken    = errors.New("invalid MCP token")
	ErrTokenNotYetValid   = errors.New("token not yet valid")
)

// Claims represents the JWT payload for a Hyperax WebSocket connection.
// These claims are embedded in the JWT and extracted into the connection
// context upon WebSocket upgrade.
type Claims struct {
	// Sub is the subject — typically the persona ID.
	Sub string `json:"sub"`

	// PersonaID is the agent persona this token was issued for.
	PersonaID string `json:"persona_id"`

	// ClearanceLevel is the RBAC/ABAC clearance level (0=internal, 1=authorized, 2=external).
	ClearanceLevel int `json:"clearance_level"`

	// Scopes lists the permitted operation scopes (e.g. "tools:read", "events:subscribe").
	Scopes []string `json:"scopes,omitempty"`

	// Delegations lists persona IDs this token can act on behalf of.
	Delegations []string `json:"delegations,omitempty"`

	// Iat is the issued-at timestamp (Unix seconds).
	Iat int64 `json:"iat"`

	// Exp is the expiration timestamp (Unix seconds).
	Exp int64 `json:"exp"`

	// Nbf is the not-before timestamp (Unix seconds).
	Nbf int64 `json:"nbf"`

	// Iss is the issuer identifier.
	Iss string `json:"iss"`

	// Jti is the unique token identifier.
	Jti string `json:"jti,omitempty"`
}

// Valid checks whether the claims have not expired and are currently active.
func (c *Claims) Valid(now time.Time) error {
	unix := now.Unix()
	if unix >= c.Exp {
		return ErrTokenExpired
	}
	if c.Nbf > 0 && unix < c.Nbf {
		return ErrTokenNotYetValid
	}
	return nil
}

// HasScope returns true if the claims include the given scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// jwtHeader is the fixed JOSE header for Ed25519 signing.
type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

var edDSAHeader = jwtHeader{Alg: "EdDSA", Typ: "JWT"}

// TokenValidator verifies an MCP bearer token and returns the associated
// claims that should be embedded in the issued JWT. This interface is
// implemented by the MCP token authentication system.
type TokenValidator interface {
	// ValidateMCPToken checks the provided MCP token string and returns
	// the claims to embed in the JWT. Returns ErrInvalidMCPToken if the
	// token is invalid, revoked, or expired.
	ValidateMCPToken(token string) (*Claims, error)
}

// TokenIssuerConfig holds configuration for the JWT issuer.
type TokenIssuerConfig struct {
	// DataDir is the directory where the Ed25519 key pair is stored.
	DataDir string

	// TTL is the token lifetime. Defaults to DefaultTokenTTL if zero.
	TTL time.Duration

	// Issuer is the "iss" claim value. Defaults to "hyperax".
	Issuer string
}

// TokenIssuer manages Ed25519 key pairs and issues/validates JWTs.
type TokenIssuer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	ttl        time.Duration
	issuer     string
	logger     *slog.Logger
	mu         sync.RWMutex // protects key rotation
}

// NewTokenIssuer creates a TokenIssuer, loading or generating the Ed25519
// key pair from the configured data directory.
func NewTokenIssuer(cfg TokenIssuerConfig, logger *slog.Logger) (*TokenIssuer, error) {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = DefaultTokenTTL
	}
	issuer := cfg.Issuer
	if issuer == "" {
		issuer = "hyperax"
	}

	ti := &TokenIssuer{
		ttl:    ttl,
		issuer: issuer,
		logger: logger,
	}

	if err := ti.loadOrGenerateKey(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("auth.NewTokenIssuer: %w", err)
	}

	return ti, nil
}

// loadOrGenerateKey reads the Ed25519 private key from disk, or generates
// a new key pair if none exists.
func (ti *TokenIssuer) loadOrGenerateKey(dataDir string) error {
	keyPath := filepath.Join(dataDir, keyFileName)

	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == ed25519.PrivateKeySize {
		ti.privateKey = ed25519.PrivateKey(data)
		ti.publicKey = ti.privateKey.Public().(ed25519.PublicKey)
		ti.logger.Info("jwt ed25519 key loaded", "path", keyPath)
		return nil
	}

	// Generate new key pair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("auth.TokenIssuer.loadOrGenerateKey: generate key: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("auth.TokenIssuer.loadOrGenerateKey: create key dir: %w", err)
	}

	if err := os.WriteFile(keyPath, priv, 0o600); err != nil {
		return fmt.Errorf("auth.TokenIssuer.loadOrGenerateKey: write key: %w", err)
	}

	ti.privateKey = priv
	ti.publicKey = pub
	ti.logger.Info("jwt ed25519 key generated", "path", keyPath)
	return nil
}

// RotateKey generates a new Ed25519 key pair and persists it, replacing
// the current key. Existing tokens signed with the old key will fail
// validation after rotation.
func (ti *TokenIssuer) RotateKey(dataDir string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("auth.TokenIssuer.RotateKey: generate key: %w", err)
	}

	keyPath := filepath.Join(dataDir, keyFileName)
	if err := os.WriteFile(keyPath, priv, 0o600); err != nil {
		return fmt.Errorf("auth.TokenIssuer.RotateKey: write key: %w", err)
	}

	ti.mu.Lock()
	ti.privateKey = priv
	ti.publicKey = pub
	ti.mu.Unlock()

	ti.logger.Info("jwt ed25519 key rotated", "path", keyPath)
	return nil
}

// Issue creates a signed JWT with the given claims. The Iat, Exp, Nbf,
// and Iss fields are set automatically based on the issuer configuration.
func (ti *TokenIssuer) Issue(claims *Claims) (string, error) {
	now := time.Now()

	claims.Iat = now.Unix()
	claims.Exp = now.Add(ti.ttl).Unix()
	claims.Nbf = now.Unix()
	claims.Iss = ti.issuer

	if claims.Sub == "" {
		claims.Sub = claims.PersonaID
	}

	return ti.sign(claims)
}

// Validate parses and verifies a JWT string, returning the embedded claims.
// Checks signature validity, algorithm, and temporal claims (exp, nbf).
func (ti *TokenIssuer) Validate(tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	// Decode and verify header.
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header decode: %v", ErrTokenMalformed, err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: header parse: %v", ErrTokenMalformed, err)
	}
	if header.Alg != "EdDSA" {
		return nil, fmt.Errorf("%w: got %s", ErrAlgorithmMismatch, header.Alg)
	}

	// Decode claims.
	claimsJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: claims decode: %v", ErrTokenMalformed, err)
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: claims parse: %v", ErrTokenMalformed, err)
	}

	// Decode signature.
	sig, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature decode: %v", ErrTokenMalformed, err)
	}

	// Verify signature over "header.claims".
	signingInput := parts[0] + "." + parts[1]

	ti.mu.RLock()
	pubKey := ti.publicKey
	ti.mu.RUnlock()

	if !ed25519.Verify(pubKey, []byte(signingInput), sig) {
		return nil, ErrSignatureInvalid
	}

	// Verify temporal claims.
	if err := claims.Valid(time.Now()); err != nil {
		return nil, err
	}

	return &claims, nil
}

// PublicKey returns the current Ed25519 public key (for external verification).
func (ti *TokenIssuer) PublicKey() ed25519.PublicKey {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return ti.publicKey
}

// sign produces the compact JWS serialization: base64url(header).base64url(claims).base64url(sig).
func (ti *TokenIssuer) sign(claims *Claims) (string, error) {
	headerJSON, err := json.Marshal(edDSAHeader)
	if err != nil {
		return "", fmt.Errorf("auth.TokenIssuer.sign: marshal header: %w", err)
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth.TokenIssuer.sign: marshal claims: %w", err)
	}

	headerB64 := base64URLEncode(headerJSON)
	claimsB64 := base64URLEncode(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	ti.mu.RLock()
	privKey := ti.privateKey
	ti.mu.RUnlock()

	sig := ed25519.Sign(privKey, []byte(signingInput))
	sigB64 := base64URLEncode(sig)

	return signingInput + "." + sigB64, nil
}

// --- Base64 URL encoding helpers (no padding, per RFC 7515) ---

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// --- HTTP Token Endpoint ---

// TokenRequest is the request body for POST /auth/token.
type TokenRequest struct {
	// MCPToken is the valid MCP bearer token to exchange for a JWT.
	MCPToken string `json:"mcp_token"`
}

// TokenResponse is the response body for POST /auth/token.
type TokenResponse struct {
	// Token is the signed JWT.
	Token string `json:"token"`

	// ExpiresIn is the token lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
}

// HandleTokenEndpoint returns an http.HandlerFunc for POST /auth/token.
// It accepts a valid MCP token and returns a signed JWT with ABAC claims.
// If validator is nil, the endpoint returns 503 Service Unavailable
// (MCP token auth not yet configured).
func HandleTokenEndpoint(issuer *TokenIssuer, validator TokenValidator, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			render.Error(w, r, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if validator == nil {
			render.Error(w, r, "token authentication not configured", http.StatusServiceUnavailable)
			return
		}

		var req TokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			render.Error(w, r, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.MCPToken == "" {
			render.Error(w, r, "mcp_token is required", http.StatusBadRequest)
			return
		}

		claims, err := validator.ValidateMCPToken(req.MCPToken)
		if err != nil {
			logger.Warn("mcp token validation failed", "error", err)
			render.Error(w, r, "invalid or expired MCP token", http.StatusUnauthorized)
			return
		}

		token, err := issuer.Issue(claims)
		if err != nil {
			logger.Error("jwt issuance failed", "error", err)
			render.Error(w, r, "token issuance failed", http.StatusInternalServerError)
			return
		}

		render.JSON(w, r, TokenResponse{
			Token:     token,
			ExpiresIn: int(issuer.ttl.Seconds()),
		}, http.StatusOK)
	}
}

// --- WebSocket JWT Extraction ---

// ExtractJWT extracts a JWT from the request, checking (in order):
// 1. Authorization: Bearer <token> header
// 2. ?token=<jwt> query parameter
// Returns ErrMissingToken if no token is found.
func ExtractJWT(r *http.Request) (string, error) {
	// Check Authorization header first.
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimPrefix(auth, prefix), nil
		}
	}

	// Fall back to query parameter.
	if token := r.URL.Query().Get("token"); token != "" {
		return token, nil
	}

	return "", ErrMissingToken
}

