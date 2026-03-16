[← Back to Docs Index](./README.md)

# Coding Guidelines & Architectural Patterns

## Table of Contents

- [Hyperax Adaptations](#hyperax-adaptations)
- [1. Entry Point & Application Lifecycle](#1-entry-point-application-lifecycle)
- [2. Logging & Observability](#2-logging-observability)
- [3. Router & Middleware](#3-router-middleware)
- [4. Authorization — ABAC System](#4-authorization-abac-system)
- [5. Response Conventions](#5-response-conventions)
- [6. WebSocket System](#6-websocket-system)
- [7. Queue System](#7-queue-system)
- [8. Cache Layer](#8-cache-layer)
- [9. Configuration](#9-configuration)
- [10. Data Layer & Repository Pattern](#10-data-layer-repository-pattern)
- [11. Prometheus Metrics — 4 Golden Signals](#11-prometheus-metrics-4-golden-signals)
- [12. Frontend Architecture](#12-frontend-architecture)
- [13. API Client Layer — The Envelope Peeler](#13-api-client-layer-the-envelope-peeler)
- [14. Data Fetching — TanStack Query](#14-data-fetching-tanstack-query)
- [15. ABAC Mirroring — usePermissions](#15-abac-mirroring-usepermissions)
- [16. WebSocket Integration — useSocket](#16-websocket-integration-usesocket)
- [17. Type Safety — Go to Zod Pipeline](#17-type-safety-go-to-zod-pipeline)
- [18. Zod-First Forms](#18-zod-first-forms)
- [19. Routing & Layouts](#19-routing-layouts)
- [20. State Management Rules](#20-state-management-rules)
- [21. Dev Workflow — air + build.sh](#21-dev-workflow-air-buildsh)
- [22. Front-to-Back Alignment](#22-front-to-back-alignment)
- [23. Architectural Evolution Roadmap](#23-architectural-evolution-roadmap)
- [24. Source Files Reference](#24-source-files-reference)

> **Provenance & Authority**
>
> This document represents a comprehensive Go coding standard built from 10+ years of production Go experience.
> It serves as the **authoritative reference** for all architectural patterns and coding conventions.
> [GoArchitecture.md](GoArchitecture.md) implements these patterns specifically for the Hyperax domain.
>
> **Relationship to GoArchitecture.md**: This document defines the *general patterns* (e.g., "use repository pattern",
> "use structured logging"). GoArchitecture.md shows how those patterns are *instantiated* in Hyperax
> (e.g., `SymbolRepo`, `SearchRepo`, zerolog with workspace context).

## Hyperax Adaptations

The following Hyperax-specific choices refine or override patterns in this guide:

| Pattern | This Guide Says | Hyperax Uses | Rationale |
|---|---|---|---|
| Router | General (any stdlib-compatible) | **chi** (not gin) | Lightweight, stdlib `http.Handler` compatible, middleware composability |
| Logger | Structured logging (general) | **zerolog** | Zero-allocation, JSON-native, fastest structured logger for Go |
| Authorization | ABAC (Section 4) | **RBAC** | Simpler model sufficient for Hyperax's tier-based feature gating |
| Forms | Zod-first (Section 18) | **Zod-first** (confirmed) | No change — Go → Zod pipeline retained |
| Metrics | Prometheus (Section 11) | **Prometheus + OTel** | OTel for distributed traces, Prometheus for metrics scraping |

> This document captures the architectural patterns for both the Go backend and React frontend,
> the target "world-class" refinements for each, and the implementation roadmap.
> All code snippets are extracted verbatim from source files unless marked as **Target**.

---

### Mandatory Rules

#### SDK-First Integration Rule

If an official SDK exists for a provider, service, or capability, it **MUST** be used. Raw HTTP calls are prohibited when an SDK is available.

- Before implementing any integration with an external service, verify whether an official SDK exists.
- If an official SDK exists, use it. No exceptions.
- If existing code uses raw HTTP where an SDK is available, it **MUST** be migrated immediately.
- This applies to all LLM providers (Google, OpenAI, Anthropic, AWS Bedrock, etc.) and any other external service integrations.
- Raw HTTP is only permitted when no official SDK exists for the required functionality (e.g., Ollama, self-hosted endpoints with no SDK).

**Official SDKs for LLM providers:**

| Provider | SDK | Module |
|----------|-----|--------|
| Google Gemini | go-genai | `google.golang.org/genai` |
| OpenAI | openai-go | `github.com/openai/openai-go` |
| Anthropic | anthropic-sdk-go | `github.com/anthropics/anthropic-sdk-go` |
| AWS Bedrock | aws-sdk-go-v2 | `github.com/aws/aws-sdk-go-v2/service/bedrockruntime` |

#### No Legacy / Fallback Code Rule

We do **NOT** under any circumstances (unless EXPLICITLY required) leave fallback, legacy, or leftover code. Any upgrades or changes must follow this process:

1. **Document** — Record what is being replaced and why.
2. **Clean** — Fully remove the old implementation (functions, types, constants, helpers, imports).
3. **Rebuild** — Implement the replacement from scratch using the new approach.

**NO FALLBACK. NO LEGACY. NO LEFTOVERS.**

- When upgrading or replacing any functionality, delete the old implementation entirely.
- Do not keep old code "just in case" — if the new implementation fails to compile, fix it.
- This includes helper functions, types, constants, and any supporting code that was only used by the old implementation.
- Dead code paths cause confusion about what's actually running, mask bugs, and create maintenance burden.

---

### Backend (Go)

1. [Entry Point & Application Lifecycle](#1-entry-point--application-lifecycle)
2. [Logging & Observability](#2-logging--observability)
3. [Router & Middleware](#3-router--middleware)
4. [Authorization — ABAC System](#4-authorization--abac-system)
5. [Response Conventions](#5-response-conventions)
6. [WebSocket System](#6-websocket-system)
7. [Queue System](#7-queue-system)
8. [Cache Layer](#8-cache-layer)
9. [Configuration](#9-configuration)
10. [Data Layer & Repository Pattern](#10-data-layer--repository-pattern)

### Frontend (React)

11. [Frontend Architecture](#11-frontend-architecture)
12. [API Client Layer — The Envelope Peeler](#12-api-client-layer--the-envelope-peeler)
13. [Data Fetching — TanStack Query](#13-data-fetching--tanstack-query)
14. [ABAC Mirroring — usePermissions](#14-abac-mirroring--usepermissions)
15. [WebSocket Integration — useSocket](#15-websocket-integration--usesocket)
16. [Type Safety — Go to Zod Pipeline](#16-type-safety--go-to-zod-pipeline)
17. [Zod-First Forms](#17-zod-first-forms)
18. [Routing & Layouts](#18-routing--layouts)
19. [State Management Rules](#19-state-management-rules)

### Full-Stack

20. [Dev Workflow — air + build.sh](#20-dev-workflow--air--buildsh)
21. [Front-to-Back Alignment](#21-front-to-back-alignment)
22. [Architectural Evolution Roadmap](#22-architectural-evolution-roadmap)
23. [Source Files Reference](#23-source-files-reference)

---

# Backend (Go)

## 1. Entry Point & Application Lifecycle

### Current Pattern

`cmd/main.go` follows a sequential initialization pattern. Each subsystem exposes a package-level `Init*()` function called once at startup:

```go
func main() {
	setLogger()
	config.InitConfig()
	config.InitSecrets()
	cache.InitCache()
	appContext := context.Background()
	err := data.InitStorageClient(appContext)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize storage")
	}
	sm := wsock.InitializeSocketManager()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	pprof.Register(router)
	router.Use(SocketMiddleware(sm))
	// ... request logger middleware ...

	web.Start(router)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		listenPort := ":8080"
		if os.Getenv("PORT") != "" {
			listenPort = ":" + os.Getenv("PORT")
		}
		if err := router.Run(listenPort); err != nil {
			log.Fatal().Err(err).Msg("Failed to start server")
		}
	}()
	<-sigCh
	appContext.Done()
	log.Info().Msg("Shutting down server")
}
```

**Issues:**
- `Init*()` functions call `log.Fatal()` on failure — sub-packages control the process lifecycle
- Shutdown does not drain in-flight requests
- Signal handling is manual with a bare channel
- Dependencies are wired implicitly through package-level globals

### Target: Error Bubbling + Graceful Shutdown + DI

```go
// Target main.go
func main() {
	setLogger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	db, err := data.Connect(cfg.DBConnString)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to DB")
	}

	c, err := cache.New(cfg.CacheConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize cache")
	}

	sm := wsock.NewSocketManager(ctx, cfg)
	app := web.NewServer(cfg, db, c, sm)

	srv := &http.Server{Addr: ":" + cfg.ListenPort, Handler: app.Router()}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("listen error")
		}
	}()

	log.Info().Msgf("Server started on :%s", cfg.ListenPort)
	<-ctx.Done()
	log.Info().Msg("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("forced shutdown")
	}
}
```

**Key changes:** Constructors return `(T, error)`, `main()` is the sole decision-maker, `http.Server.Shutdown()` drains in-flight requests, `signal.NotifyContext` replaces manual channel management.

---

## 2. Logging & Observability

### Current Pattern: Structured Logging

`github.com/rs/zerolog` with a global level toggled by `APP_DEBUG`:

```go
func setLogger() {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	if os.Getenv("APP_DEBUG") != "" {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		return
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
```

### Logging Conventions

| Aspect | Convention |
|--------|-----------|
| **Library** | zerolog — structured, zero-allocation JSON logging |
| **Time format** | RFC 3339 with nanosecond precision |
| **Debug toggle** | Any non-empty `APP_DEBUG` value enables debug |
| **Default level** | `InfoLevel` in production |
| **Usage** | Import `github.com/rs/zerolog/log` directly — no wrapper |

Use structured fields (`.Str()`, `.Int()`, `.Err()`, `.Interface()`, `.Dur()`, `.Bool()`) for machine-parseable output.

### Rule: No `log.Fatal()` Outside `main()`

Sub-packages **must not** call `log.Fatal()` or `log.Panic()`. Return errors and let `main()` decide.

### Target: Distributed Tracing with OpenTelemetry

Structured logging tells you *what* happened. Distributed tracing tells you *why* — was the 3-second latency from the DB query, the queue publish, or the WebSocket dispatch?

**The Problem:** With WebSockets, Pub/Sub queues, and async execution chains, a single user action can span multiple goroutines and services. Correlating logs across these boundaries is manual and error-prone.

**The Solution:** Add OpenTelemetry (OTel) middleware that generates a `trace_id` per request and propagates it through the entire chain.

```go
// Target: OTel middleware for Gin
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func (s *Server) registerMiddleware() {
	s.router.Use(otelgin.Middleware("app-service"))
	s.router.Use(s.requestLogger())
	s.router.Use(s.socketMiddleware())
}
```

**Trace propagation through the stack:**

```
HTTP Request (trace_id generated)
  → Gin middleware (otelgin)
    → AuthMiddlewareWithSession (span: "auth")
      → DB query (span: "mysql.query")
      → Cache lookup (span: "bigcache.get")
    → Handler (span: "handler.GetAgents")
      → Queue publish (span: "pubsub.publish")
        → WebSocket dispatch (span: "ws.send")
```

**Frontend integration:** Pass the trace ID to the frontend via a response header so browser-side errors can be correlated:

```go
// Target: attach trace ID to response
func traceHeaderMiddleware() gin.HandlerFunc {
	return func(gc *gin.Context) {
		span := trace.SpanFromContext(gc.Request.Context())
		if span.SpanContext().HasTraceID() {
			gc.Header("X-Trace-ID", span.SpanContext().TraceID().String())
		}
		gc.Next()
	}
}
```

**Key OTel dependencies:**

```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin
go.opentelemetry.io/contrib/instrumentation/database/sql/otelsql
```

**Benefit:** When an engineer says "the WebSocket execution failed," you look at a single trace that shows the HTTP request, the DB lookup, the queue publish, and the WebSocket dispatch — all in one timeline with latency breakdowns.

---

## 3. Router & Middleware

### Current Pattern

Two global middleware functions in `main()`, route orchestration in `router.go`, domain groups in `routes.go`.

**SocketMiddleware** — injects `SocketManager` into Gin's context:

```go
func SocketMiddleware(sm *wsock.SocketManager) gin.HandlerFunc {
	return func(gc *gin.Context) {
		gc.Set("socketManager", sm)
		gc.Next()
	}
}
```

**Route orchestration** (`router.go`):

```go
func Start(r *gin.Engine) *gin.Engine {
	HandleRoutes(r, "static")
	r.GET("/workers", EventHandler)
	return r
}
```

**Domain groups** (`routes.go`) — `RegisterRoutes()` creates Gin router groups per domain (`/api/actions`, `/api/agents`, etc.), each with its own route function.

### Target: Server Struct

```go
// Target — chi-based server
type Server struct {
	db     *sql.DB
	cfg    *config.AppConfig
	sm     *wsock.SocketManager
	cache  *bigcache.BigCache
	router chi.Router
}

func NewServer(cfg *config.AppConfig, db *sql.DB, cache *bigcache.BigCache, sm *wsock.SocketManager) *Server {
	s := &Server{db: db, cfg: cfg, sm: sm, cache: cache, router: chi.NewRouter()}
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

func (s *Server) Router() http.Handler { return s.router }
```

### Convention: Adding New Routes

1. Create `internal/web/handlers/<domain>/` with handler functions using `func(w http.ResponseWriter, r *http.Request)` signatures
2. Add `<Domain>Routes(chi.Router)` in `routes.go`
3. Add one line to `registerRoutes()`: `s.router.Route("/api/<domain>", <Domain>Routes)`
4. Use chi middleware for auth: `r.With(AuthMiddleware(action, level)).Get("/path", handler)`
5. Extract URL parameters with `chi.URLParam(r, "id")`, not from context values

---

## 4. Authorization — ABAC System

> **Hyperax Note**: Hyperax uses **RBAC** (Role-Based Access Control) rather than ABAC. The ABAC patterns
> below remain valid as a reference architecture, but the Hyperax implementation simplifies to role-based
> checks. See [GoArchitecture.md Section 7](GoArchitecture.md#7-storage-layer) for the actual implementation.

### Current Pattern

Three hierarchical levels: Organization, Team, Resource. Middleware in `api/web/routes.go`, logic in `api/data/abac.go`, model in `api/abac/abac.go`.

```go
const (
	NoneLevel     = iota  // 0 — no permission check
	OrgLevel              // 1 — check org permissions
	TeamLevel             // 2 — check org read + team permissions
	ResourceLevel         // 3 — check org read + team read + resource permissions
)
```

> **Known issue:** These constants are duplicated in `api/web/routes.go` and `api/data/abac.go`.

```go
func AuthMiddlewareWithSession(action string, level int, fn func(gc *gin.Context, s *data.Session)) gin.HandlerFunc {
	return func(gc *gin.Context) {
		isAuth, session := auth.IsAuthenticatedSession(gc)
		if !isAuth {
			helpers.RenderError(gc, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !data.CheckABACPermissions(session, action, level) {
			helpers.RenderError(gc, http.StatusForbidden, "forbidden")
			return
		}
		fn(gc, session)
	}
}
```

**Permission model:** Six permissions (`create`, `read`, `update`, `delete`, `special`, `protected`), five roles (admin, editor, viewer, special, protected), `CanDo(action)` method.

**Session storage:** Three maps — `OrgABAC`, `TeamABAC`, `ResourcePerms` — populated at login from `organization_users` and `team_members` tables.

### Target: Session in Context

```go
type contextKey string

const sessionKey contextKey = "authenticated_session"

// AuthMiddleware returns chi-compatible middleware that validates authentication
// and RBAC permissions, injecting the session into request context.
func AuthMiddleware(action string, level int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAuth, session := auth.IsAuthenticated(r)
			if !isAuth {
				RenderError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !rbac.Check(session, action, level) {
				RenderError(w, http.StatusForbidden, "forbidden")
				return
			}
			ctx := context.WithValue(r.Context(), sessionKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetSession(r *http.Request) *data.Session {
	return r.Context().Value(sessionKey).(*data.Session)
}
```

Handlers use standard `func(w http.ResponseWriter, r *http.Request)` signatures — session accessed via `web.GetSession(r)`.

---

## 5. Response Conventions

All API handlers use three response helpers from `internal/web/response.go`:

```go
func RenderError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func RenderSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "success"})
}

func RenderContent(w http.ResponseWriter, content any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"result": content})
}
```

| Function | Status | JSON Shape |
|----------|--------|------------|
| `RenderError(w, code, msg)` | Caller-specified | `{"error": "msg"}` |
| `RenderSuccess(w)` | 200 | `{"message": "success"}` |
| `RenderContent(w, data)` | 200 | `{"result": <data>}` |

**Rule:** All handlers **must** use these helpers. Do not call `json.NewEncoder(w).Encode()` with ad-hoc shapes.
---

## 6. WebSocket System

Connects remote agents to the server for bidirectional command execution. Spans `api/web/websocket_handler.go` and `api/wsock/`.

**Connection flow:** Agent connects to `GET /workers` with `Authorization: id:key` → WebSocket upgrade → agent lookup + key validation → `sm.InitClient()` → read loop → `sm.RemoveClient()` on disconnect.

**Message protocol:**

```go
type SocketMessage struct {
	MsgType  string `json:"MsgType"`
	Action   string `json:"Action"`
	ActionId string `json:"ActionId"`
	Message  string `json:"Message"`
}
```

| MsgType | Behavior |
|---------|----------|
| `execute-success` | Records success, triggers notifications and chained actions |
| `execute-failure` | Records failure, triggers notifications and chained actions |
| `heartbeat` | Updates last-connection timestamp |
| `message` | Logged only |

**Execution lifecycle:** Queue message → `MessageProcessor` parses `runner:action:actionID` → dispatch to connected client → client replies → `HandleExecutionComplete` → status recording + notification + action chaining.

### Target: JWT-Based WebSocket Auth

Replace the static `Authorization: id:key` header with a short-lived JWT issued by a dedicated endpoint. This enables key rotation without agent reconnection and eliminates a DB lookup on every WebSocket upgrade.

**Token endpoint:**

```go
// POST /api/auth/token
// Authenticated via session cookie (browser) or API key (agent CLI)
func (s *Server) HandleTokenRequest(w http.ResponseWriter, r *http.Request) {
	session := GetSession(r)

	claims := jwt.MapClaims{
		"sub":   session.AgentID,
		"org":   session.OrgID,
		"team":  session.TeamID,
		"perms": session.RBACMap(),
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		RenderError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	RenderContent(w, map[string]any{"token": signed, "expires_in": 300})
}
```

**Upgraded WebSocket connection flow:**

```
Current:  GET /workers + Authorization: id:key → DB lookup → upgrade
Target:   POST /api/auth/token → JWT → GET /workers?token=<jwt> → verify signature → upgrade
```

**Why this matters:**

| Concern | `id:key` (Current) | JWT (Target) |
|---------|---------------------|--------------|
| Key rotation | Requires agent reconnection | Rotate signing key; agents re-fetch token |
| Auth cost per upgrade | DB query to validate key | Signature verification only (CPU, no I/O) |
| RBAC on upgrade | Separate permission check call | Claims embedded in token |
| Token lifetime | Static (never expires) | 5-minute expiry, auto-refresh |
| Revocation | Delete key from DB | Short TTL + optional deny-list |

**Convention:**
- Token TTL should be short (5 minutes) — agents refresh before expiry
- RBAC claims are advisory for the upgrade handler; the backend still enforces on each action dispatch
- Use `HS256` with a server-side secret; no need for asymmetric keys in a single-service architecture
- The `/api/auth/token` endpoint is authenticated via existing `AuthMiddleware`

---

## 7. Queue System

Pluggable `Queue` interface with provider selection at startup:

```go
type Queue interface {
	Enqueue(ctx context.Context, id string, payload string, ttl int64) error
	ConnectResponse()
}
```

**Providers:** Google Pub/Sub (production), AWS SQS (functional), Redis (stub).

**Target:** Factory function returning `(Queue, error)` instead of `log.Fatal`. Queue injected into `SocketManager`.

---

## 8. Cache Layer

### Current Pattern

In-memory sharded cache using `github.com/allegro/bigcache/v3`. 2-minute TTL, 1-minute cleanup, 8 GB hard limit, 1024 shards.

**Domain accessor pattern:** Each entity gets a file with prefixed keys (`session-`, `agent-`, etc.) wrapping core `getData`/`saveData`/`deleteData` operations.

**Integration:** Read-through — check cache, fall back to DB, warm cache. Writes update both DB and cache.

### Target: Injected CacheService + Singleflight Stampede Protection

Replace the package-level singleton with a constructor. Add `golang.org/x/sync/singleflight` to prevent cache stampedes.

**The Problem:** When a cache key expires under high traffic, 100 concurrent requests for the same agent ID all miss the cache and hit the database simultaneously. This is a "thundering herd" or cache stampede.

**The Solution:** `singleflight.Group` ensures that only one goroutine fetches from the DB. The other 99 wait and share the result.

```go
// Target: CacheService with singleflight
import "golang.org/x/sync/singleflight"

type CacheService struct {
	store *bigcache.BigCache
	sf    singleflight.Group
}

func NewCacheService(cfg bigcache.Config) (*CacheService, error) {
	store, err := bigcache.New(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("cache init: %w", err)
	}
	return &CacheService{store: store}, nil
}

// GetOrFetch checks cache, deduplicates concurrent DB calls, then warms cache
func (c *CacheService) GetOrFetch(key string, fetchFn func() (interface{}, error)) (interface{}, error) {
	// 1. Check cache
	if cached, err := c.store.Get(key); err == nil {
		var result interface{}
		if err := json.Unmarshal(cached, &result); err == nil {
			return result, nil
		}
	}

	// 2. Singleflight: only one goroutine hits the DB
	v, err, _ := c.sf.Do(key, func() (interface{}, error) {
		result, err := fetchFn()
		if err != nil {
			return nil, err
		}

		// 3. Warm cache for subsequent requests
		data, _ := json.Marshal(result)
		c.store.Set(key, data)

		return result, nil
	})

	return v, err
}
```

**Usage in the data layer:**

```go
// Target: Agent.GetByID with singleflight protection
func (r *MySQLAgentRepo) GetByID(ctx context.Context, id string) (*Agent, error) {
	v, err := r.cache.GetOrFetch("agent-"+id, func() (interface{}, error) {
		a := &Agent{}
		row := r.db.QueryRowContext(ctx, `SELECT ... FROM agents WHERE id = ?`, id)
		err := row.Scan(&a.Id, &a.Name, ...)
		return a, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*Agent), nil
}
```

**Impact:** Under a traffic spike with 100 concurrent requests for the same expired key, only 1 DB query executes instead of 100. The other 99 goroutines wait ~1ms and share the result.

---

## 9. Configuration

Hyperax uses a two-tier configuration model. **Bootstrap** config (minimal YAML) provides only the fields needed to connect to the database. **Runtime** config lives in the database as scoped key-value pairs, managed via the dashboard.

### Bootstrap YAML (`hyperax.yaml`)

The bootstrap file contains at most 4 fields: `listen_addr`, `data_dir`, `storage.backend`, `storage.dsn`. No feature flags, no subsystem tuning, no agent configuration. If the file does not exist, sensible defaults apply.

### Runtime Config Store (Database)

- **`config_keys`** table defines the schema: key name, allowed scope, value type, default, critical flag, description
- **`config_values`** table stores actual values as scoped key-value pairs

**Scope resolution:** `agent → workspace → global → default`

### Implementation Rules

1. **Never read config from YAML at runtime** — Only `LoadBootstrap()` reads the YAML file, and only at startup. All runtime config access goes through `ConfigStore.Resolve()`.
2. **Emit events on change** — Every `ConfigStore.Set()` call emits a `config.changed` event on the Nervous System Transport Stream.
3. **Critical keys require confirmation** — REST handlers must check `IsCritical()` and return a confirmation prompt to the dashboard before persisting.
4. **Task-boundary application** — Agents apply config changes at task boundaries, not mid-task. When `config.changed` arrives for an agent-scoped key (e.g., `agent.model`), the agent finishes its current task and reads the new value before starting the next one.
5. **Type safety** — `ConfigStore.Resolve()` returns `string`. Callers parse to the target type using the `value_type` metadata from `config_keys`. Helper functions: `ResolveInt()`, `ResolveBool()`, `ResolveDuration()`.

**Target:** `ConfigStore` injected as a dependency. No singletons, no global state.

---

## 10. Data Layer & Repository Pattern

### Storage Layer Functional Specification

This section defines the implementation mandates for Hyperax's storage layer. It enforces Clean Architecture boundaries, the Repository Pattern, and `singleflight`-protected caching.

> **Cross-references:** [Architecture.md § 5.2](Architecture.md#52-storage-layer-architecture) describes the high-level design. [GoArchitecture.md § 7](GoArchitecture.md#7-storage-layer) provides the complete Go code.

### 10.1 Domain Model Boundary

All domain types live in `pkg/types/` as plain Go structs (POGOs). These are the **only** types that cross package boundaries.

```go
// pkg/types/symbol.go — Domain Model Boundary
type Symbol struct {
    ID          string    `json:"id"`
    WorkspaceID string    `json:"workspace_id"`
    Name        string    `json:"name"`
    Kind        string    `json:"kind"`
    FilePath    string    `json:"file_path"`
    Signature   string    `json:"signature"`
    Range       LineRange `json:"range"`
}

type LineRange struct {
    Start int `json:"start"`
    End   int `json:"end"`
}
```

**Rule:** Database-specific types (`queries.*` from sqlc, `sql.NullString`, `pgtype.*`, MySQL driver types) never appear outside `internal/storage/`. Each backend (SQLite, PostgreSQL, MySQL/MariaDB) translates between domain types and database types at the repository boundary.

### 10.2 Granular Repository Interfaces

Repository interfaces live in `internal/repo/`, one file per domain. Each interface is small, focused, and independently mockable.

```go
// internal/repo/symbol_repo.go
type SymbolRepo interface {
    GetByID(ctx context.Context, id string) (*types.Symbol, error)
    Search(ctx context.Context, query search.Query) ([]*types.Symbol, error)
    Upsert(ctx context.Context, sym *types.Symbol) error
    DeleteByFile(ctx context.Context, fileID string) error
}

// internal/repo/interjection_repo.go
type InterjectionRepo interface {
    PullCord(ctx context.Context, signal *types.InterjectionSignal) error
    GetActive(ctx context.Context, scope types.Scope) ([]*types.Interjection, error)
    Resolve(ctx context.Context, id string, resolution *types.Resolution) error
}
```

**Rule:** Handler code depends on **individual** repository interfaces, not a monolithic `Store`. A CommHub handler that needs symbols imports `repo.SymbolRepo`. A pipeline handler imports `repo.PipelineRepo`. No handler ever imports the entire storage package.

### 10.3 Cached Decorator "Shield" Pattern

Cached repositories implement the same interface as their inner implementation, wrapping it with `singleflight`-protected cache-aside logic:

```go
// internal/storage/cached_symbol_repo.go
type CachedSymbolRepo struct {
    inner repo.SymbolRepo
    cache *cache.Service
}

func (r *CachedSymbolRepo) GetByID(ctx context.Context, id string) (*types.Symbol, error) {
    key := "sym:" + id
    val, err := r.cache.GetOrFetch(key, func() (any, error) {
        return r.inner.GetByID(ctx, id)
    })
    if err != nil {
        return nil, err
    }
    return val.(*types.Symbol), err
}
```

The `cache.Service.GetOrFetch` method uses `singleflight.Group` internally. Under a traffic spike with 100 concurrent requests for the same expired key, only 1 DB query executes — the other 99 goroutines wait ~1ms and share the result.

Write operations invalidate relevant cache entries (write-through):

```go
func (r *CachedSymbolRepo) Upsert(ctx context.Context, sym *types.Symbol) error {
    err := r.inner.Upsert(ctx, sym)
    if err == nil {
        r.cache.Invalidate("sym:" + sym.ID)
    }
    return err
}
```

### 10.4 Failure-Aware Composition Root

The `Store` struct in `internal/storage/store.go` is a composition root — not a God object. It wires concrete implementations at construction time, connecting to the Graceful Degradation Matrix:

```go
// internal/storage/store.go
type Store struct {
    Symbols       repo.SymbolRepo
    Search        repo.SearchRepo
    Interjections repo.InterjectionRepo
    Projects      repo.ProjectRepo
    Pipelines     repo.PipelineRepo
    // ... one field per domain
}

func NewStore(cfg config.StorageConfig, c *cache.Service) (*Store, error) {
    var base repo.SymbolRepo
    switch cfg.Driver {
    case "postgres":
        base = postgres.NewSymbolRepo(cfg.DSN)
    case "mysql":
        base = mysql.NewSymbolRepo(cfg.DSN)
    default:
        base = sqlite.NewSymbolRepo(cfg.DSN)
    }

    // Search degradation — wired at construction, not runtime
    var searchImpl repo.SearchRepo
    switch {
    case hasVectorExt && hasONNX:
        searchImpl = hybrid.NewSearchRepo(fts5, vector, rrf)  // Level 3: Hybrid
    case hasFTS5:
        searchImpl = fts5.NewSearchRepo(db)                    // Level 2: FTS5
    default:
        searchImpl = like.NewSearchRepo(db)                    // Level 1: LIKE
    }

    return &Store{
        Symbols: cache.NewCachedSymbolRepo(base, c),
        Search:  searchImpl,
    }, nil
}
```

Degradation decisions are made **once at startup**, not scattered across handler code. Each tier implements the same `SearchRepo` interface — consumers never know which level they're using.

### 10.5 Implementation Mandates

These are non-negotiable rules for all storage layer code:

| # | Mandate | Rationale |
|---|---------|-----------|
| 1 | **Context pass-through** — Every repository method accepts `context.Context` as its first parameter. Never use `context.Background()` inside a repository. | Enables cancellation propagation, deadline enforcement, and trace correlation across the full call chain. |
| 2 | **No `sql.NullString` leakage** — Database nullable types (`sql.NullString`, `sql.NullInt64`, `pgtype.*`) must be translated to Go zero values or `*T` pointers at the repository boundary. Domain types in `pkg/types/` never contain database-specific nullable wrappers. | Prevents database dialect concerns from infecting domain logic. Consumers should never need to check `.Valid` on a domain struct field. |
| 3 | **Atomic migrations** — Each migration file contains exactly one logical change. Never combine table creation with data backfill in a single migration. Use `golang-migrate` with embedded SQL files (`go:embed`). Migrations run automatically at startup via `Store.Migrate()`. | Enables safe rollback of individual changes. Prevents partial migration states that require manual intervention. |
| 4 | **Trace correlation** — Repository implementations must propagate the `trace_id` from context to any log entries or error wraps. Use `fmt.Errorf("repo.SymbolRepo.GetByID: %w", err)` for error wrapping with the full method path. | Enables end-to-end tracing from MCP tool call → handler → repository → SQL query. Critical for diagnosing performance issues in production. |

### 10.6 Developer Onboarding Note

> **If you find yourself writing a SQL query inside a CommHub handler, you have violated the architecture.**
>
> The correct flow is: `Handler → repo.Interface → internal/storage/sqlite/ or postgres/ → sqlc-generated queries`. Every layer has a single responsibility. Handlers orchestrate. Repositories abstract. Storage implementations execute.

---

## 11. Prometheus Metrics — 4 Golden Signals

### Overview

The backend exposes a `/metrics` endpoint serving Prometheus-format metrics. This covers the [4 Golden Signals](https://sre.google/sre-book/monitoring-distributed-systems/#xref_monitoring_golden-signals) (latency, traffic, errors, saturation) plus application-specific gauges for WebSocket connections, queue depth, and cache performance.

Together with zerolog (logs), OpenTelemetry (traces), and Prometheus (metrics), the application covers the **three pillars of observability**.

### Dependency

```go
import "github.com/prometheus/client_golang/prometheus"
import "github.com/prometheus/client_golang/prometheus/promhttp"
```

### HTTP Golden Signals Middleware

A single chi-compatible middleware instruments all HTTP handlers:

```go
// internal/web/metrics/middleware.go
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Latency — histogram of request durations
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// Traffic — counter of total requests
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status"})

	// Errors — counter of 5xx responses
	httpErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_errors_total",
		Help: "Total number of HTTP 5xx responses.",
	}, []string{"method", "route"})
)

// Middleware returns a chi-compatible middleware that records the 4 golden signals.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := NewResponseWriter(w) // wraps http.ResponseWriter to capture status code
		next.ServeHTTP(ww, r)

		status := strconv.Itoa(ww.Status())
		route := chi.RouteContext(r.Context()).RoutePattern() // route template, not raw path
		method := r.Method
		duration := time.Since(start).Seconds()

		httpRequestDuration.WithLabelValues(method, route, status).Observe(duration)
		httpRequestsTotal.WithLabelValues(method, route, status).Inc()

		if ww.Status() >= 500 {
			httpErrorsTotal.WithLabelValues(method, route).Inc()
		}
	})
}
```

**Key design decisions:**
- Uses `chi.RouteContext(r.Context()).RoutePattern()` (e.g., `/api/agents/{id}`) not `r.URL.Path` — avoids unbounded cardinality from path parameters
- `promauto` handles registration — no manual `prometheus.MustRegister` boilerplate
- `DefBuckets` (5ms–10s) covers typical web handler latencies
- Uses a `ResponseWriter` wrapper to capture the status code written by downstream handlers
### Application Metrics

Beyond HTTP signals, instrument domain-specific gauges and counters:

```go
// api/metrics/application.go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Saturation — WebSocket connections
	WebsocketConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "websocket_connections_active",
		Help: "Number of active WebSocket connections.",
	})

	// Saturation — Queue depth
	QueueMessagesPending = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "queue_messages_pending",
		Help: "Number of pending messages in the queue.",
	}, []string{"provider"})

	// Agent status
	AgentsOnline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "agents_online",
		Help: "Number of agents with active heartbeat.",
	})

	// Execution tracking
	AgentExecutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_executions_total",
		Help: "Total agent executions by outcome.",
	}, []string{"status"}) // status: success, failure, timeout

	// Cache performance
	CacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_hits_total",
		Help: "Total BigCache hits.",
	})

	CacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_misses_total",
		Help: "Total BigCache misses.",
	})

	// Session cache
	SessionCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "session_cache_size",
		Help: "Number of sessions currently cached.",
	})
)
```

### Instrumentation Points

| Metric | Where to Instrument | How |
|--------|---------------------|-----|
| `websocket_connections_active` | `wsock.InitClient()` / `wsock.RemoveClient()` | `.Inc()` / `.Dec()` |
| `queue_messages_pending` | Queue provider `Receive()` loop | `.Set(float64(depth))` per poll |
| `agents_online` | Heartbeat sweep in `SocketManager` | `.Set(float64(count))` per sweep |
| `agent_executions_total` | `SocketManager` execution result handler | `.WithLabelValues("success").Inc()` |
| `cache_hits_total` / `cache_misses_total` | `cache.getData()` | `.Inc()` on hit or miss |
| `session_cache_size` | `data.Session.Upsert()` / `data.Session.Get()` | `.Inc()` / periodic `.Set()` |

### Exposing the /metrics Endpoint

```go
// In router setup (internal/web/router.go)
import "github.com/prometheus/client_golang/prometheus/promhttp"

func (s *Server) registerRoutes() {
	// Metrics middleware on all routes
	s.router.Use(metrics.Middleware)

	// Prometheus scrape endpoint — no auth required
	s.router.Handle("/metrics", promhttp.Handler())

	// ... existing routes
}
```
### Prometheus Scrape Config

```yaml
# prometheus.yml (or ServiceMonitor for k8s)
scrape_configs:
  - job_name: 'app-service'
    scrape_interval: 15s
    metrics_path: /metrics
    static_configs:
      - targets: ['app-service:8080']
```

### Useful PromQL Queries

| Signal | Query | Purpose |
|--------|-------|---------|
| **Latency (p99)** | `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))` | Catch slow endpoints |
| **Traffic** | `sum(rate(http_requests_total[5m])) by (route)` | Requests per second by route |
| **Error rate** | `sum(rate(http_errors_total[5m])) / sum(rate(http_requests_total[5m]))` | Percentage of 5xx responses |
| **Saturation** | `websocket_connections_active` | Current WebSocket load |
| **Cache hit rate** | `cache_hits_total / (cache_hits_total + cache_misses_total)` | Cache effectiveness |
| **Queue backlog** | `queue_messages_pending` | Queue health per provider |
| **Agent health** | `agents_online` | Heartbeat-alive agent count |
| **Execution success** | `rate(agent_executions_total{status="success"}[5m]) / rate(agent_executions_total[5m])` | Agent execution reliability |

### Alert Examples

```yaml
# alertmanager rules
groups:
  - name: app-service
    rules:
      - alert: HighErrorRate
        expr: sum(rate(http_errors_total[5m])) / sum(rate(http_requests_total[5m])) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Error rate above 5%"

      - alert: HighLatency
        expr: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m])) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "p99 latency above 2 seconds"

      - alert: QueueBacklog
        expr: queue_messages_pending > 100
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Queue backlog exceeding 100 messages"

      - alert: NoAgentsOnline
        expr: agents_online == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "No agents reporting heartbeat"
```

### Convention

- All metrics live in `api/metrics/` — never define `prometheus.Counter` in domain packages
- Use `promauto` for registration — keeps it declarative
- Label cardinality must stay bounded — use route templates, not raw paths
- `/metrics` endpoint requires no authentication — Prometheus needs unauthenticated access
- Application metrics are updated at the point of state change, not via periodic polling (except `agents_online` which uses the heartbeat sweep)

---

# Frontend (React)

## 12. Frontend Architecture

### Design Philosophy

The React frontend is a **thin client** that projects backend state. It never owns business logic — all authorization, validation, and data persistence happens in Go. The frontend's job is:

1. **Render** the data the backend provides
2. **Collect** user input and send it to the backend
3. **Mirror** the ABAC system for UI-level visibility (hide buttons the user can't use)
4. **Cache** server responses locally for responsiveness (TanStack Query)

### Project Structure

```
src/
├── api/
│   ├── client.ts              # Axios instance with envelope interceptors
│   └── types/                 # Generated Zod schemas (from Go structs)
│       ├── agent.ts
│       ├── action.ts
│       ├── session.ts
│       ├── organization.ts
│       ├── team.ts
│       └── notification.ts
├── components/
│   ├── ui/                    # Reusable primitives (Button, Modal, DataList)
│   └── domain/                # Domain-specific (AgentCard, ActionModal, OrgSwitcher)
├── hooks/
│   ├── usePermissions.ts      # ABAC mirroring hook
│   ├── useSocket.ts           # WebSocket lifecycle hook
│   └── useAuth.ts             # Session context hook
├── layouts/
│   ├── RootLayout.tsx         # Navbar, Footer, global auth check
│   ├── OrgLayout.tsx          # Org sidebar, org-level ABAC
│   └── TeamLayout.tsx         # Team tools, team-level ABAC
├── pages/                     # Route components
│   ├── agents/
│   ├── actions/
│   ├── organizations/
│   ├── teams/
│   └── user/
├── services/                  # TanStack Query hooks per entity
│   ├── agentService.ts
│   ├── actionService.ts
│   ├── orgService.ts
│   ├── teamService.ts
│   ├── notificationService.ts
│   └── userService.ts
├── config.ts                  # Frontend config singleton
├── App.tsx                    # Router + providers
└── main.tsx                   # Entry point
```

### Key Dependencies

| Package | Purpose | Replaces |
|---------|---------|----------|
| `react` + `react-dom` | UI rendering | Svelte |
| `@tanstack/react-query` | Server-state caching | Redux, SWR |
| `@tanstack/react-router` | Type-safe nested routing | React Router |
| `axios` | HTTP client with interceptors | fetch |
| `zod` | Runtime validation + TypeScript types | Manual interfaces |
| `react-hook-form` + `@hookform/resolvers` | Form management with Zod validation | Manual form state |
| `tailwindcss` | Utility-first CSS | Custom CSS |

### Rules

- **No Redux.** The backend owns state. TanStack Query handles server-state cache.
- **No complex filtering in React.** If you need it, add a Go endpoint. React is a "dumb projector."
- **No manual TypeScript interfaces for API types.** Use the Go → Zod pipeline (see [Section 17](#17-type-safety--go-to-zod-pipeline)).
- **Keep logic in Go.** If a component does permission checking beyond `can('action', level)`, move it to the backend.
- **All forms use Zod resolvers.** See [Section 18](#18-zod-first-forms).

---

## 13. API Client Layer — The Envelope Peeler

The Axios client is the frontend's middleware — it mirrors the Go `AuthMiddlewareWithSession` and `RenderError`/`RenderContent` helpers.

```typescript
// src/api/client.ts
import axios from 'axios';

const client = axios.create({
  baseURL: '/api',
  withCredentials: true, // Sends the session cookie
});

// Response interceptor — peels the envelope
client.interceptors.response.use(
  (response) => {
    // RenderContent wraps in {"result": ...} — unwrap it
    return response.data.result;
  },
  (error) => {
    const status = error.response?.status;

    // Mirror AuthMiddlewareWithSession: 401 → redirect to login
    if (status === 401) {
      window.location.href = '/goto/auth/login/google';
      return Promise.reject(error);
    }

    // Normalize RenderError: {"error": "msg"} → Error("msg")
    const message = error.response?.data?.error || 'An unexpected error occurred';
    return Promise.reject(new Error(message));
  }
);

export default client;
```

Components never see the `{"result": ...}` or `{"error": ...}` envelope — they get clean data or a catchable `Error`.

```
Go Backend                    Axios Interceptor              React Component
─────────────────────────────────────────────────────────────────────────────
RenderContent(gc, agents)  →  {"result": [...]}  →  peeled  →  Agent[]
RenderError(gc, 403, msg)  →  {"error": "msg"}   →  Error   →  catch(e)
401 Unauthorized           →  redirect to login   →  (never reaches component)
```

### Conventions

- **All API calls go through `client`** — never raw `fetch`
- **`withCredentials: true`** is mandatory — Go uses the session cookie
- **401 handling is centralized** — components don't handle auth redirects
- **Error messages come from Go** — the frontend displays `error.message` directly

---

## 14. Data Fetching — TanStack Query

Each entity gets a service file in `src/services/` mirroring the Go `api/data/` layout.

```typescript
// src/services/agentService.ts
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import client from '../api/client';
import type { Agent } from '../api/types/agent';

const STALE_TIME = 2 * 60 * 1000; // 2 minutes — matches BigCache TTL

export const useAgents = () => {
  return useQuery<Agent[]>({
    queryKey: ['agents'],
    queryFn: () => client.get('/agents'),
    staleTime: STALE_TIME,
  });
};

export const useAgent = (id: string) => {
  return useQuery<Agent>({
    queryKey: ['agents', id],
    queryFn: () => client.get(`/agents/${id}`),
    staleTime: STALE_TIME,
    enabled: !!id,
  });
};

export const useCreateAgent = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: Partial<Agent>) => client.post('/agents', data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] });
    },
  });
};
```

### Cache Alignment

| Layer | Cache | TTL | Invalidation |
|-------|-------|-----|-------------|
| **Frontend** (TanStack Query) | In-memory per query key | 2 min (`staleTime`) | `invalidateQueries` on mutations |
| **Backend** (BigCache) | In-memory per entity key | 2 min (`LifeWindow`) | Write-through on Upsert/Delete |
| **Backend** (Singleflight) | Dedup concurrent DB calls | Per-request | Automatic |
| **Database** (MySQL) | Source of truth | — | — |

---

## 15. ABAC Mirroring — usePermissions

The frontend mirrors the Go `CheckABACPermissions` logic to control UI visibility. The backend remains the enforcer.

```typescript
// src/hooks/usePermissions.ts
import { useAuth } from './useAuth';

const NoneLevel = 0;
const OrgLevel = 1;
const TeamLevel = 2;
const ResourceLevel = 3;

export { NoneLevel, OrgLevel, TeamLevel, ResourceLevel };

export const usePermissions = () => {
  const { session } = useAuth();

  const can = (action: string, level: number): boolean => {
    if (!session) return false;
    if (!session.ent_context) return true;
    if (level === NoneLevel && action === '') return true;

    const orgId = session.current_org;
    const teamId = session.current_team;

    switch (level) {
      case NoneLevel: return true;
      case OrgLevel:
        return canDo(session.org_abac?.[orgId], action);
      case TeamLevel:
        if (!canDo(session.org_abac?.[orgId], 'read')) return false;
        return canDo(session.team_abac?.[teamId], action);
      case ResourceLevel:
        if (!canDo(session.org_abac?.[orgId], 'read')) return false;
        if (!canDo(session.team_abac?.[teamId], 'read')) return false;
        return canDo(session.resource_abac?.[teamId], action);
      default: return false;
    }
  };

  return { can };
};
```

**Usage:** `{can('delete', ResourceLevel) && <DeleteButton />}` — visibility only, backend enforces.

---

## 16. WebSocket Integration — useSocket

```typescript
// src/hooks/useSocket.ts
export const useSocket = (agentId: string, agentKey: string) => {
  const wsRef = useRef<WebSocket | null>(null);
  const queryClient = useQueryClient();

  const connect = useCallback(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/workers`);

    ws.onmessage = (event) => {
      const msg: SocketMessage = JSON.parse(event.data);
      switch (msg.MsgType) {
        case 'execute-success':
        case 'execute-failure':
          queryClient.invalidateQueries({ queryKey: ['agents', agentId] });
          queryClient.invalidateQueries({ queryKey: ['actions', msg.ActionId] });
          break;
        case 'heartbeat':
          ws.send(JSON.stringify({ MsgType: 'heartbeat', Message: 'pong' }));
          break;
      }
    };

    ws.onclose = () => setTimeout(connect, 5000);
    wsRef.current = ws;
  }, [agentId, agentKey, queryClient]);

  useEffect(() => { connect(); return () => wsRef.current?.close(); }, [connect]);
};
```

Execution results invalidate TanStack Query caches so the UI stays in sync.

### Target: JWT-Authenticated WebSocket

When the backend moves to JWT-based WebSocket auth (see [Section 6](#6-websocket-system)), the hook acquires a short-lived token before connecting:

```typescript
// src/hooks/useSocket.ts — Target
export const useSocket = (agentId: string) => {
  const wsRef = useRef<WebSocket | null>(null);
  const queryClient = useQueryClient();

  const connect = useCallback(async () => {
    // Fetch short-lived JWT from the token endpoint
    const { token } = await client.post('/auth/token');

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/workers?token=${token}`
    );

    ws.onmessage = (event) => {
      const msg: SocketMessage = JSON.parse(event.data);
      switch (msg.MsgType) {
        case 'execute-success':
        case 'execute-failure':
          queryClient.invalidateQueries({ queryKey: ['agents', agentId] });
          queryClient.invalidateQueries({ queryKey: ['actions', msg.ActionId] });
          break;
        case 'heartbeat':
          ws.send(JSON.stringify({ MsgType: 'heartbeat', Message: 'pong' }));
          break;
      }
    };

    ws.onclose = () => setTimeout(connect, 5000); // Auto-reconnect fetches a fresh token
    wsRef.current = ws;
  }, [agentId, queryClient]);

  useEffect(() => { connect(); return () => wsRef.current?.close(); }, [connect]);
};
```

**Key changes from current:**
- `agentKey` parameter removed — the JWT carries identity and permissions
- Token is fetched on every `connect()` call, including reconnects — always fresh
- No static credentials stored on the client

---

## 17. Type Safety — Go to Zod Pipeline

### The Problem

Manually writing TypeScript interfaces to match Go structs creates type drift. When a Go struct changes, the frontend silently breaks at runtime.

### The Solution: Automated Go → Zod Generation

A build-time script parses Go struct definitions — including `json` tags and `validate` tags — and generates Zod schemas with both **types** and **validation rules** in one artifact.

### Pipeline

```
Go Structs + validate tags (api/data/*.go)
    ↓  [go2zod script]
Zod Schemas with validators (src/api/types/*.ts)
    ↓  [z.infer<typeof schema>]
TypeScript Types (used in services)
    ↓  [zodResolver(schema)]
Form Validation (used in react-hook-form)
```

### Generated Output Example

From a Go struct with validation tags:

```go
// api/data/agents.go
type Agent struct {
	Id             string    `json:"id" validate:"required,uuid"`
	Name           string    `json:"name" validate:"required,min=3,max=64"`
	Key            string    `json:"key"`
	OwnerID        DbString  `json:"owner"`
	CreatorID      string    `json:"creator"`
	TeamID         DbString  `json:"team"`
	OrgID          DbString  `json:"organization"`
	IsOnline       bool      `json:"is_online"`
	LastPing       time.Time `json:"last_ping"`
	Updated        time.Time `json:"updated"`
	HasQueue       bool      `json:"queued"`
	SuccessAction  string    `json:"success_action"`
	FailureAction  string    `json:"failure_action"`
	UseSecurityKey bool      `json:"use_security_key"`
	SecurityKey    string    `json:"-"`
}
```

The generated Zod schema includes validation rules:

```typescript
// src/api/types/agent.ts — AUTO-GENERATED, DO NOT EDIT
import { z } from 'zod';

export const AgentSchema = z.object({
  id: z.string().uuid(),                    // validate:"required,uuid"
  name: z.string().min(3).max(64),          // validate:"required,min=3,max=64"
  key: z.string(),
  owner: z.string().nullable(),             // DbString → nullable
  creator: z.string(),
  team: z.string().nullable(),
  organization: z.string().nullable(),
  is_online: z.boolean(),
  last_ping: z.string().datetime(),         // time.Time → datetime string
  updated: z.string().datetime(),
  queued: z.boolean(),
  success_action: z.string(),
  failure_action: z.string(),
  use_security_key: z.boolean(),
  // SecurityKey omitted: json:"-"
});

export type Agent = z.infer<typeof AgentSchema>;

// Partial schema for create/update forms (all fields optional except required ones)
export const AgentCreateSchema = AgentSchema.pick({
  name: true,
});

export const AgentListSchema = z.array(AgentSchema);
export type AgentList = z.infer<typeof AgentListSchema>;
```

### Type Mapping Rules

| Go Type | Zod Type | Notes |
|---------|----------|-------|
| `string` | `z.string()` | — |
| `int`, `int64`, `float64` | `z.number()` | — |
| `bool` | `z.boolean()` | — |
| `time.Time` | `z.string().datetime()` | JSON serializes as ISO string |
| `DbString` (NullString) | `z.string().nullable()` | Custom MarshalJSON emits `null` |
| `[]T` | `z.array(TSchema)` | — |
| `map[string]T` | `z.record(z.string(), TSchema)` | — |
| `json:"-"` | (omitted) | Never sent to frontend |

### Validation Tag Mapping

| Go `validate` Tag | Zod Method | Example |
|-------------------|-----------|---------|
| `required` | (field is not `.optional()`) | — |
| `min=N` (string) | `.min(N)` | `validate:"min=3"` → `.min(3)` |
| `max=N` (string) | `.max(N)` | `validate:"max=64"` → `.max(64)` |
| `min=N` (number) | `.min(N)` | `validate:"min=0"` → `.min(0)` |
| `max=N` (number) | `.max(N)` | `validate:"max=100"` → `.max(100)` |
| `email` | `.email()` | `validate:"email"` → `.email()` |
| `uuid` | `.uuid()` | `validate:"uuid"` → `.uuid()` |
| `url` | `.url()` | `validate:"url"` → `.url()` |
| `oneof=a b c` | `.enum(['a','b','c'])` | `validate:"oneof=sqs pubsub redis"` |
| `len=N` | `.length(N)` | `validate:"len=36"` → `.length(36)` |

**Single source of truth:** Changing `validate:"min=3"` to `validate:"min=5"` in Go automatically updates the Zod schema on the next build. The frontend form validation, TypeScript types, and backend validation all stay in sync.

### The go2zod Script

```bash
#!/bin/bash
# scripts/go2zod.sh — Run during build to regenerate frontend types
set -e

GO_DATA_DIR="api/data"
GO_ABAC_DIR="api/abac"
ZOD_OUT_DIR="src/api/types"

mkdir -p "$ZOD_OUT_DIR"

echo "Generating Zod schemas from Go structs..."

go run scripts/go2zod.go --input "$GO_DATA_DIR/agents.go" --output "$ZOD_OUT_DIR/agent.ts"
go run scripts/go2zod.go --input "$GO_DATA_DIR/actions.go" --output "$ZOD_OUT_DIR/action.ts"
go run scripts/go2zod.go --input "$GO_DATA_DIR/sessions.go" --output "$ZOD_OUT_DIR/session.ts"
go run scripts/go2zod.go --input "$GO_DATA_DIR/organizations.go" --output "$ZOD_OUT_DIR/organization.ts"
go run scripts/go2zod.go --input "$GO_DATA_DIR/teams.go" --output "$ZOD_OUT_DIR/team.ts"
go run scripts/go2zod.go --input "$GO_DATA_DIR/notifications.go" --output "$ZOD_OUT_DIR/notification.ts"
go run scripts/go2zod.go --input "$GO_ABAC_DIR/abac.go" --output "$ZOD_OUT_DIR/permission.ts"

echo "Zod schemas generated in $ZOD_OUT_DIR"
```

### The go2zod Go Tool

Uses `go/ast` to parse struct definitions, reads both `json` and `validate` tags, and emits Zod schemas:

```go
// scripts/go2zod.go — core logic
// 1. Parse the Go file with go/ast
// 2. Find all exported struct types
// 3. For each field:
//    a. Read json tag (skip json:"-")
//    b. Map Go type → Zod base type
//    c. Read validate tag → append Zod validators (.min(), .max(), .email(), etc.)
//    d. Handle DbString → z.string().nullable()
// 4. Emit: export const <Name>Schema = z.object({ ... })
// 5. Emit: export type <Name> = z.infer<typeof <Name>Schema>
```

### Convention

- Generated files have `— AUTO-GENERATED, DO NOT EDIT` header
- The script is called from `build.sh` before `vite build`
- Never manually edit files in `src/api/types/`
- If a Go struct changes, the next build catches mismatches at compile time

---

## 18. Zod-First Forms

### The Pattern

Since Zod schemas are auto-generated from Go structs (including validation rules), all React forms **must** use `react-hook-form` with the `@hookform/resolvers/zod` resolver. This closes the full loop:

```
Go struct + validate tags → Zod schema → Form resolver → API request → Go handler validates again
```

Four layers. One source of truth.

### Implementation

```typescript
// src/pages/agents/CreateAgentPage.tsx
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { AgentCreateSchema } from '../../api/types/agent';
import { useCreateAgent } from '../../services/agentService';
import type { z } from 'zod';

type CreateAgentForm = z.infer<typeof AgentCreateSchema>;

const CreateAgentPage = () => {
  const createAgent = useCreateAgent();

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<CreateAgentForm>({
    resolver: zodResolver(AgentCreateSchema), // Auto-generated schema with validation!
  });

  const onSubmit = (data: CreateAgentForm) => {
    createAgent.mutate(data);
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <input {...register('name')} placeholder="Agent name" />
      {errors.name && <span>{errors.name.message}</span>}
      {/* Error message auto-generated: "String must contain at least 3 character(s)" */}

      <button type="submit" disabled={createAgent.isPending}>
        Create Agent
      </button>
    </form>
  );
};
```

### What Happens When You Change a Go Validation Rule

1. **Developer** changes `validate:"min=3"` to `validate:"min=5"` on `Agent.Name` in Go
2. **`build.sh`** runs `go2zod.sh` → regenerates `src/api/types/agent.ts` with `.min(5)`
3. **`vite build`** compiles the frontend with the updated schema
4. **React form** now enforces `min=5` via the Zod resolver — no manual frontend change needed
5. **Go handler** also enforces `min=5` via the validate tag — defense in depth

### Rules

- **Every form uses `zodResolver`** with the auto-generated schema
- **No manual validation logic** in components — Zod handles it
- **Custom error messages** can be added in the Zod schema if the defaults aren't sufficient
- **Partial schemas** (`AgentCreateSchema`, `AgentUpdateSchema`) should be generated alongside the full schema for create/update forms that only accept a subset of fields

---

## 19. Routing & Layouts

### Layout Hierarchy

Nested layouts mirror the ABAC hierarchy:

| Route | Layout | Responsibility |
|-------|--------|---------------|
| `/` | `RootLayout` | Navbar, footer, global auth check via `AuthProvider` |
| `/org/:id/*` | `OrgLayout` | Org sidebar, org switcher, org-level ABAC gate |
| `/org/:id/team/:id/*` | `TeamLayout` | Team tools, team-level ABAC gate |
| `/agents`, `/actions`, etc. | (within team) | Resource-level ABAC per component |
| `/user/*` | `RootLayout` | Personal context (no org/team) |

### Layout-Level Auth Gates

Each layout checks the appropriate ABAC level before rendering children:

```tsx
const OrgLayout = () => {
  const { can } = usePermissions();
  if (!can('read', OrgLevel)) return <Forbidden />;

  return (
    <div className="flex">
      <OrgSidebar />
      <main><Outlet /></main>
    </div>
  );
};
```

---

## 20. State Management Rules

| State Type | Where It Lives | Example |
|------------|---------------|---------|
| **Server data** | TanStack Query | Agents, Actions, Orgs, Teams |
| **Auth/session** | `AuthContext` (React Context) | Current user, ABAC maps |
| **UI state** | `useState` in the component | Modal open/closed, form inputs |
| **Shared UI state** | `jotai` atoms (if needed) | Sidebar collapsed, theme |
| **URL state** | Router params/search | Current org ID, filters, pagination |

### Rules

1. **If it comes from `/api`, it goes in TanStack Query.** No Redux. No `useState` for server data.
2. **If it's purely visual, use `useState`.** A modal's open state doesn't need to be global.
3. **If multiple unrelated components need the same UI state, use a `jotai` atom.** Rare.
4. **Separate query keys for lists and items.** `['agents']` for the list, `['agents', id]` for one.
5. **Prefer URL state for anything bookmarkable.** Filters, pagination, sort order, org/team context.

---

# Full-Stack

## 21. Dev Workflow — air + build.sh

### Architecture

A single `air` process watches both Go and React source files. On any change, `build.sh` runs the full pipeline: generate types → build frontend → recompile Go → restart.

```
File change detected (*.go, *.ts, *.tsx)
    ↓
air triggers build.sh
    ↓
┌─────────────────────────────────┐
│ 1. scripts/go2zod.sh            │  Generate Zod types from Go structs
│ 2. npx vite build               │  Bundle React → static/
│ 3. go build -o ./tmp/main ./cmd │  Compile Go binary
└─────────────────────────────────┘
    ↓
air restarts ./tmp/main
    ↓
Gin serves static/ (React SPA) + /api/* (Go handlers) + /workers (WebSocket)
```

### build.sh

```bash
#!/bin/bash
set -e
echo "=== Generating types ==="
bash scripts/go2zod.sh
echo "=== Building frontend ==="
npx vite build
echo "=== Building backend ==="
go build -o ./tmp/main ./cmd
```

### .air.toml

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "bash build.sh"
  bin = "./tmp/main"
  delay = 500
  include_ext = ["go", "ts", "tsx", "css", "html"]
  exclude_dir = ["tmp", "node_modules", "static", ".git"]
  kill_delay = 500

[log]
  time = true

[misc]
  clean_on_exit = true
```

### vite.config.ts

```typescript
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  root: 'src',
  build: {
    outDir: '../static',
    emptyOutDir: false,  // Don't delete existing static assets (images, SVGs)
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
});
```

### Why This Works

| Concern | Solution |
|---------|----------|
| Go changes | air detects `.go` → rebuilds and restarts |
| React changes | air detects `.ts`/`.tsx` → vite build → new static → Go restarts |
| Type drift | go2zod runs before every build — types always match |
| No dev server | Gin serves everything — no CORS, no proxy |
| No caching issues | Full rebuild every time — always 100% up to date |
| Fast iteration | Go ~2s + Vite ~3s = ~5s total cycle |

---

## 22. Front-to-Back Alignment

### Symmetry Table

| Logic Step | Go Backend | React Frontend |
|------------|-----------|---------------|
| **Identity** | `AuthMiddlewareWithSession` | Axios 401 interceptor → redirect to login |
| **Authorization** | `CheckABACPermissions` | `usePermissions` hook (UI visibility only) |
| **Validation** | `validate` struct tags | Zod schemas (auto-generated from same tags) |
| **Form validation** | Go handler validates on receive | `zodResolver(Schema)` validates on submit |
| **Caching** | BigCache (2-min TTL) | TanStack Query (2-min staleTime) |
| **Stampede protection** | `singleflight.Do()` | (not applicable — single user) |
| **Success response** | `helpers.RenderContent(gc, data)` | Axios interceptor: `response.data.result` |
| **Error response** | `helpers.RenderError(gc, code, msg)` | Axios interceptor: `Error(response.data.error)` |
| **WebSocket auth** | JWT via `/api/auth/token` (ABAC in claims) | `useSocket` fetches token before connect |
| **WebSocket** | `wsock.SocketManager` | `useSocket` hook |
| **Config** | `config.Get()` → `*AppConfig` | `config.ts` → env vars from `.env` |
| **Entity pattern** | `data.Agent` struct + methods | `services/agentService.ts` + Zod schema |
| **State ownership** | MySQL (source of truth) | TanStack Query (cache of backend state) |
| **Observability** | OpenTelemetry traces + zerolog | `X-Trace-ID` header for error correlation |
| **Metrics** | Prometheus `/metrics` endpoint (4 golden signals) | Grafana dashboards / alerts |
| **Type safety** | Go structs + `json` + `validate` tags | Auto-generated Zod schemas |

### The Principle

> The frontend learns the logic once — it applies to both sides of the stack.
> A junior engineer who understands `AuthMiddlewareWithSession` immediately understands the Axios 401 interceptor.
> A junior engineer who understands BigCache's TTL immediately understands TanStack Query's `staleTime`.
> A junior engineer who adds `validate:"min=5"` to a Go struct never has to touch the frontend — the form updates automatically.

---

## 23. Architectural Evolution Roadmap

### Backend Priority Order

| # | Change | Scope | Prerequisite |
|---|--------|-------|-------------|
| 1 | **Error bubbling** — `Init*()` → constructors returning `(T, error)` | `config`, `cache`, `data`, `wsock` | None |
| 2 | **Graceful shutdown** — `http.Server.Shutdown()` + `signal.NotifyContext` | `cmd/main.go` | #1 |
| 3 | **ABAC session in context** — standardize handler signatures | `api/web/routes.go`, all handlers | None |
| 4 | **Repository interfaces** — per-entity interfaces for data layer | `api/data/` | None |
| 5 | **DI constructors** — `Server` struct, inject all dependencies | `cmd/main.go`, `api/web/` | #1–#4 |
| 6 | **Singleflight** — cache stampede protection | `api/cache/` | #5 (CacheService) |
| 7 | **OpenTelemetry** — distributed tracing | `cmd/main.go`, middleware | #5 (Server struct) |
| 8 | **Prometheus metrics** — 4 golden signals + application gauges | `api/metrics/`, `api/web/router.go` | #5 (Server struct) |
| 9 | **JWT WebSocket auth** — short-lived tokens with ABAC claims | `api/web/`, `api/wsock/` | #5 (Server struct) |
| 10 | **Validation tags** — add `validate` tags to Go structs | `api/data/*.go` | None |

### Frontend Priority Order

| # | Change | Scope | Prerequisite |
|---|--------|-------|-------------|
| 1 | **React scaffold** — Vite + React + TanStack Query + Router | `src/`, `vite.config.ts` | None |
| 2 | **API client** — Axios with envelope interceptors | `src/api/client.ts` | #1 |
| 3 | **Auth + ABAC** — AuthProvider, usePermissions | `src/hooks/` | #2 |
| 4 | **Go → Zod pipeline** — go2zod script with validate tag support | `scripts/`, `src/api/types/` | Backend #8 |
| 5 | **Service layer** — TanStack Query hooks per entity | `src/services/` | #2, #4 |
| 6 | **Zod-first forms** — react-hook-form + zodResolver | All form pages | #4 |
| 7 | **Layouts + routing** — RootLayout, OrgLayout, TeamLayout | `src/layouts/`, `src/pages/` | #3 |
| 8 | **WebSocket hook** — useSocket for agent status | `src/hooks/useSocket.ts` | #2 |

### Impact Summary

| Change | Developer Experience | Operational Reliability |
|--------|---------------------|------------------------|
| Error Bubbling | `go test ./...` without panics | No mystery crashes during init |
| Graceful Shutdown | Local dev stops cleanly | Zero dropped requests during K8s rolling updates |
| Context ABAC | Handlers look like standard Go/Gin | Middleware becomes plug-and-play |
| Repository Interfaces | Unit testing becomes real | — |
| DI Constructors | Dependencies are explicit | Provider swaps are one-line changes |
| Singleflight | — | No thundering herd on cache expiry |
| OpenTelemetry | Single trace per request across all subsystems | Production debugging is tractable |
| Prometheus Metrics | Dashboards and alerts out of the box | SLO tracking, capacity planning, proactive alerting |
| JWT WebSocket Auth | Key rotation without reconnection | No DB lookup on upgrade, ABAC enforced at the edge |
| Validation Tags | Single source of truth for all validation | Frontend/backend can never disagree |
| Go → Zod Pipeline | Type mismatches caught at build time | No silent API drift |
| Zod-First Forms | Zero manual form validation code | Form rules auto-sync with backend |
| air + build.sh | Single process, full-stack hot-reload | Always 100% up to date |

### Architectural Comparison

| Feature | Current Pattern | Target Refinement |
|---------|----------------|-------------------|
| State | Global singletons | Dependency injection |
| Testing | Integration-heavy | Unit-testable via interfaces/mocks |
| Errors | `log.Fatal` in sub-packages | Bubbled up to `main` |
| Concurrency | Manual signal handling | `signal.NotifyContext` |
| Cache | Read-through only | Read-through + singleflight |
| Config | Mutable mutex singleton | Immutable struct as dependency |
| Handlers | Custom `func(gc, session)` | Standard `func(gc *gin.Context)` |
| WebSocket auth | Static `id:key` header | Short-lived JWT with ABAC claims |
| Observability | Structured logging only | OpenTelemetry traces + Prometheus metrics |
| Validation | Backend-only, no struct tags | `validate` tags → Go + Zod (both sides) |
| Frontend | SvelteKit (separate dev server) | React (built to static, served by Gin) |
| Type safety | Manual TypeScript interfaces | Go → Zod auto-generation |
| Form validation | Manual per-component | Zod resolver (auto-generated) |
| Frontend state | Component-local | TanStack Query (server) + jotai (UI) |

---

## 24. Source Files Reference

### Backend

| File | Package | Key Contents |
|------|---------|-------------|
| `cmd/main.go` | `main` | Entry point, setLogger, SocketMiddleware, graceful shutdown |
| `api/web/router.go` | `web` | `Start()`, `HandleRoutes()`, `GetStaticFiles()` |
| `api/web/routes.go` | `web` | `RegisterRoutes()`, `AuthMiddlewareWithSession`, domain route functions |
| `api/web/websocket_handler.go` | `web` | `EventHandler`, `HandleClientMessages`, `GetIdent` |
| `api/web/goto.go` | `web` | `GotoRedirects`, `RedirectToGoogleLogin`, `LogoutHandler` |
| `api/wsock/wsock.go` | `wsock` | `SocketManager`, `ClientInfo`, heartbeat system, execution lifecycle |
| `api/wsock/manager.go` | `wsock` | `MessageProcessor`, `getRunnerAction` |
| `api/wsock/queues.go` | `wsock` | `Queue` interface, `InitializeQueue`, `Get()` singleton |
| `api/wsock/qproviders/pubsub.go` | `qproviders` | Google Pub/Sub queue provider (production) |
| `api/wsock/qproviders/sqs.go` | `qproviders` | AWS SQS queue provider |
| `api/wsock/qproviders/redis.go` | `qproviders` | Redis queue provider (stub) |
| `api/abac/abac.go` | `abac` | `Permission` struct, role mappings, `CanDo()` |
| `api/data/abac.go` | `data` | `CheckABACPermissions`, ABAC level constants |
| `api/data/data.go` | `data` | `GetDB()` singleton, `DbString`, `InitStorageClient` |
| `api/data/sessions.go` | `data` | `Session` struct, ABAC permission loading, cache integration |
| `api/data/agents.go` | `data` | `Agent` struct, status tracking, notification management |
| `api/metrics/metrics.go` | `metrics` | HTTP golden signals middleware (latency, traffic, errors) |
| `api/metrics/application.go` | `metrics` | Application gauges/counters (WebSocket, queue, cache, agents) |
| `api/cache/cache.go` | `cache` | `InitCache()`, core get/save/delete operations |
| `api/config/config.go` | `config` | `AppConfig`, `InitConfig()`, `InitSecrets()`, `Get()` |
| `api/helpers/http.go` | `helpers` | `RenderError`, `RenderSuccess`, `RenderContent` |
| `api/helpers/hashes.go` | `helpers` | `GetUUID`, `GenerateBCryptHash`, `ValidateBearerToken` |
| `api/helpers/slices.go` | `helpers` | `RemoveFromSlice`, `Difference`, `GetAddRem` (generics) |
| `api/helpers/strings.go` | `helpers` | `Contains`, `JsonOrEmpty` |
| `api/helpers/templates.go` | `helpers` | `StringFromTemplate`, `MergeMap` |

### Frontend (Target)

| File | Purpose |
|------|---------|
| `src/main.tsx` | React entry point |
| `src/App.tsx` | Router + providers (AuthProvider, QueryClientProvider) |
| `src/api/client.ts` | Axios instance with envelope interceptors |
| `src/api/types/*.ts` | Auto-generated Zod schemas from Go structs |
| `src/hooks/useAuth.ts` | AuthContext + AuthProvider |
| `src/hooks/usePermissions.ts` | ABAC mirroring hook |
| `src/hooks/useSocket.ts` | WebSocket lifecycle hook |
| `src/services/agentService.ts` | TanStack Query hooks for agents |
| `src/services/actionService.ts` | TanStack Query hooks for actions |
| `src/services/orgService.ts` | TanStack Query hooks for organizations |
| `src/services/teamService.ts` | TanStack Query hooks for teams |
| `src/layouts/RootLayout.tsx` | Global layout with auth gate |
| `src/layouts/OrgLayout.tsx` | Org-level layout with ABAC gate |
| `src/layouts/TeamLayout.tsx` | Team-level layout with ABAC gate |
| `src/config.ts` | Frontend configuration from env vars |

### Build & Tooling (Target)

| File | Purpose |
|------|---------|
| `vite.config.ts` | Vite build config (output to static/) |
| `build.sh` | Full-stack build script (go2zod → vite → go build) |
| `.air.toml` | air config for full-stack hot-reload |
| `scripts/go2zod.sh` | Type generation orchestrator |
| `scripts/go2zod.go` | Go AST → Zod schema converter (with validate tag support) |
---

[↑ Back to Top](#coding-guidelines-architectural-patterns) | [← Back to Docs Index](./README.md)
