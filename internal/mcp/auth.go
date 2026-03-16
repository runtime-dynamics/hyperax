package mcp

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// authContextKey is the context key for AuthContext.
const authContextKey contextKey = "auth"

// AuthFromContext extracts the AuthContext from a request context.
// Returns a zero-value AuthContext with Authenticated=false if not present.
func AuthFromContext(ctx context.Context) types.AuthContext {
	if v, ok := ctx.Value(authContextKey).(types.AuthContext); ok {
		return v
	}
	return types.AuthContext{}
}

// withAuthContext returns a new context carrying the given AuthContext.
func withAuthContext(ctx context.Context, ac types.AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, ac)
}

// AuthContextKey returns the context key used for AuthContext storage.
// Exported for testing; handlers that need to inject AuthContext in tests
// can use context.WithValue(ctx, mcp.AuthContextKey(), authContext).
func AuthContextKey() contextKey {
	return authContextKey
}

// MaxClearanceLevel is the highest ABAC clearance level in the system.
// Localhost connections receive this level automatically.
// See pkg/types/auth.go for tier definitions:
//   0=Observer, 1=Operator, 2=Admin, 3=ChiefOfStaff.
const MaxClearanceLevel = 3

// Authenticator validates MCP bearer tokens and produces AuthContext values.
// When tokenRepo is nil or auth is not required, all requests pass through
// as unauthenticated (clearance 0).
//
// Localhost connections (127.0.0.1, ::1) are automatically granted max clearance
// to allow local tools like Claude Code full access without token configuration.
// GuardBypassResolver looks up whether a persona has guard bypass enabled.
// Returns true if the persona should bypass guard evaluation.
type GuardBypassResolver func(ctx context.Context, personaID string) bool

type Authenticator struct {
	tokenRepo           repo.MCPTokenRepo
	configRepo          repo.ConfigRepo
	bus                 *nervous.EventBus
	guardBypassResolver GuardBypassResolver
	logger              *slog.Logger
}

// NewAuthenticator creates an Authenticator. Pass nil tokenRepo to disable auth.
// Pass nil bus to disable token audit trail events.
func NewAuthenticator(tokenRepo repo.MCPTokenRepo, configRepo repo.ConfigRepo, logger *slog.Logger) *Authenticator {
	return &Authenticator{
		tokenRepo:  tokenRepo,
		configRepo: configRepo,
		logger:     logger,
	}
}

// SetEventBus configures the EventBus for publishing token audit trail events.
// When set, the authenticator publishes token.validated events on successful
// token validation.
func (a *Authenticator) SetEventBus(bus *nervous.EventBus) {
	a.bus = bus
}

// SetGuardBypassResolver configures a callback that resolves whether a persona
// has guard_bypass enabled. This avoids importing the persona repo directly and
// follows the same function injection pattern used elsewhere in the codebase.
func (a *Authenticator) SetGuardBypassResolver(resolver GuardBypassResolver) {
	a.guardBypassResolver = resolver
}

// Authenticate extracts and validates a Bearer token from the HTTP request.
// On success, it returns the enriched context with AuthContext injected.
// On failure when auth is required, it returns (nil, nil) — the caller should
// respond with JSON-RPC error code -32001.
//
// Localhost connections are automatically granted max clearance (level 2)
// regardless of token presence, allowing local tools like Claude Code full
// access to all MCP tools.
//
// Auth is required when the config key "auth.required" is "true".
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (context.Context, *types.AuthContext) {
	// Localhost bypass: grant max clearance for local connections.
	if isLocalhost(r) {
		ac := types.AuthContext{
			PersonaID:      "localhost",
			ClearanceLevel: MaxClearanceLevel,
			Scopes:         []string{"*"},
			Authenticated:  true,
		}
		return withAuthContext(ctx, ac), &ac
	}

	// Extract bearer token from Authorization header.
	token := extractBearerToken(r)

	// If no token repo configured, skip auth entirely.
	if a.tokenRepo == nil {
		ac := types.AuthContext{Authenticated: false}
		return withAuthContext(ctx, ac), &ac
	}

	// No token provided — check if auth is required.
	if token == "" {
		if a.isAuthRequired(ctx) {
			return nil, nil
		}
		ac := types.AuthContext{Authenticated: false}
		return withAuthContext(ctx, ac), &ac
	}

	// Validate the provided token.
	mcpToken, err := a.tokenRepo.ValidateToken(ctx, token)
	if err != nil {
		a.logger.Warn("mcp token validation failed", "error", err)
		return nil, nil
	}

	ac := types.AuthContext{
		PersonaID:      mcpToken.AgentID,
		TokenID:        mcpToken.ID,
		ClearanceLevel: mcpToken.ClearanceLevel,
		Scopes:         mcpToken.Scopes,
		Authenticated:  true,
	}

	// Resolve guard_bypass from the agent record if a resolver is configured.
	if a.guardBypassResolver != nil && mcpToken.AgentID != "" {
		ac.GuardBypass = a.guardBypassResolver(ctx, mcpToken.AgentID)
	}

	// Publish token.validated audit event.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventTokenValidated,
			"authenticator",
			"global",
			map[string]any{
				"token_id":        mcpToken.ID,
				"agent_id":        mcpToken.AgentID,
				"clearance_level": mcpToken.ClearanceLevel,
			},
		))
	}

	return withAuthContext(ctx, ac), &ac
}

// isAuthRequired checks if authentication is required via the "auth.required"
// config key (global scope). Defaults to false.
func (a *Authenticator) isAuthRequired(ctx context.Context) bool {
	if a.configRepo == nil {
		return false
	}
	scope := types.ConfigScope{Type: "global", ID: ""}
	val, err := a.configRepo.GetValue(ctx, "auth.required", scope)
	if err != nil {
		return false
	}
	return val == "true"
}

// extractBearerToken extracts the token from "Authorization: Bearer <token>".
// The "Bearer" prefix is matched case-insensitively per RFC 7235 Section 2.1,
// which specifies that authentication schemes are case-insensitive.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) < 7 {
		return ""
	}
	if !strings.EqualFold(auth[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[7:])
}

// isLocalhost reports whether the request originates from a loopback address
// (127.0.0.0/8 or ::1). This is used to grant automatic max clearance to
// local tools like Claude Code without requiring token authentication.
func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr without port — unlikely but handle gracefully.
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
