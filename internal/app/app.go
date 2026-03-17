package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/audit"
	"github.com/hyperax/hyperax/internal/auth"
	"github.com/hyperax/hyperax/internal/budget"
	"github.com/hyperax/hyperax/internal/cache"
	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/cron"
	"github.com/hyperax/hyperax/internal/fsconflict"
	"github.com/hyperax/hyperax/internal/index"
	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/internal/lifecycle"
	"github.com/hyperax/hyperax/internal/memory"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/plugin"
	"github.com/hyperax/hyperax/internal/pulse"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/role"
	agentscheduler "github.com/hyperax/hyperax/internal/scheduler"
	"github.com/hyperax/hyperax/internal/search"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/tooluse"
	"github.com/hyperax/hyperax/pkg/types"
)

// HyperaxApp is the top-level application container.
type HyperaxApp struct {
	Version              string // binary version (e.g. "1.2.0" or "dev"), set by main.go
	Bootstrap            *config.BootstrapConfig
	Config               *config.ConfigStore
	Store                *storage.Store
	Bus                  *nervous.EventBus
	Logger               *slog.Logger
	Router               http.Handler
	Indexer              *index.Indexer
	Scheduler            *cron.Scheduler
	PluginManager        *plugin.PluginManager
	PulseEngine          *pulse.Engine
	RingBuffer           *nervous.RingBuffer
	NervousExecutor      *nervous.Executor
	TxManager            *refactor.TransactionManager
	Cache                *cache.Service
	Searcher             *search.HybridSearcher
	MemoryEngine         *memory.MemoryStore
	InterjectionMgr      *interject.Manager
	SensorManager        *pulse.SensorManager
	SecretRegistry       *secrets.Registry
	RoleTemplateRegistry *role.RoleTemplateRegistry
	JWTIssuer            *auth.TokenIssuer
	ToolUseBridge        *tooluse.Bridge
	AgentScheduler       *agentscheduler.AgentScheduler
	Sentinel             *nervous.Sentinel
	IndexWatcher         *index.IndexWatcher
	ChannelBridge        interface{ Run(context.Context) } // *channelbridge.Bridge — avoids circular import

	httpServer     *http.Server
	redirectServer *http.Server // HTTP→HTTPS redirect listener (nil when TLS is off)
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// New creates a HyperaxApp with injected dependencies.
func New(bootstrap *config.BootstrapConfig, store *storage.Store, bus *nervous.EventBus, logger *slog.Logger) *HyperaxApp {
	configStore := config.NewConfigStore(store.Config, bus)
	idx := index.NewIndexer(store.Symbols, store.Search, bus, logger)
	sentinel := nervous.NewSentinel(bus, logger)
	watcher := index.NewIndexWatcher(idx, bus, sentinel, store.Workspaces, logger)

	var sched *cron.Scheduler
	if store.Cron != nil {
		sched = cron.NewScheduler(store.Cron, bus, logger)
	}

	pulseEng := pulse.NewEngine(bus, logger)

	// Initialise hybrid search engine with bootstrap config.
	searchCfg := search.Config{
		EnableVector:   bootstrap.Search.EnableVector,
		EmbeddingModel: bootstrap.Search.EmbeddingModel,
		EmbeddingDim:   bootstrap.Search.EmbeddingDim,
		FusionK:        bootstrap.Search.FusionK,
	}
	searcher := search.NewHybridSearcher(store.Search, searchCfg)

	// Wire vector search bridge if vector search is enabled and VectorRepo is available.
	if searchCfg.EnableVector && store.Vectors != nil {
		vecBridge := search.NewVectorBridge(store.Vectors, store.Symbols)
		searcher.SetVectorSearcher(vecBridge)

		// Configure ONNX embedder if a model path is provided.
		if searchCfg.EmbeddingModel != "" {
			onnxEmb := search.NewONNXEmbedder(search.ONNXEmbedderConfig{
				ModelPath: searchCfg.EmbeddingModel,
				Dim:       searchCfg.EmbeddingDim,
				Normalise: true,
				Logger:    logger,
			})
			if onnxEmb != nil {
				searcher.SetEmbedder(onnxEmb)
			}
		}
	}

	// Initialise in-memory cache if enabled in bootstrap config.
	var cacheService *cache.Service
	if bootstrap.Cache.Enabled {
		ttl, err := time.ParseDuration(bootstrap.Cache.TTL)
		if err != nil || ttl == 0 {
			ttl = 10 * time.Minute
		}
		clean, err := time.ParseDuration(bootstrap.Cache.CleanInterval)
		if err != nil || clean == 0 {
			clean = 5 * time.Minute
		}
		maxSize := bootstrap.Cache.MaxSizeMB
		if maxSize == 0 {
			maxSize = 256
		}
		shards := bootstrap.Cache.Shards
		if shards == 0 {
			shards = 1024
		}

		cacheCfg := cache.Config{
			TTL:           ttl,
			MaxSizeMB:     maxSize,
			Shards:        shards,
			CleanInterval: clean,
		}
		cs, err := cache.New(cacheCfg)
		if err != nil {
			logger.Warn("cache init failed, continuing without cache", "error", err)
		} else {
			cacheService = cs
			logger.Info("in-memory cache initialised",
				"ttl", ttl,
				"max_size_mb", maxSize,
				"shards", shards,
			)
		}
	}

	// Initialise the memory engine if the memory repo is available.
	var memoryEngine *memory.MemoryStore
	if store.Memory != nil {
		memoryEngine = memory.NewMemoryStore(store.Memory, bus, logger)
		logger.Info("memory engine initialised")
	}

	// Initialise the interjection manager (Andon Cord).
	var interjectionMgr *interject.Manager
	if store.Interjections != nil {
		interjectionMgr = interject.NewManager(store.Interjections, bus, logger)
		logger.Info("interjection manager initialised")
	}

	// Initialise the secret provider registry with the local provider.
	secretReg := secrets.NewRegistry()
	if store.Config != nil {
		secretReg.SetConfigRepo(store.Config)
	}
	localProvider := secrets.NewLocalProvider(store.Secrets)
	if localProvider != nil {
		secretReg.Register(localProvider)
	}
	// Restore persisted provider selection (must be called after providers are registered).
	secretReg.LoadActive(context.Background())

	// Initialise JWT issuer for WebSocket authentication.
	var jwtIssuer *auth.TokenIssuer
	jwtCfg := auth.TokenIssuerConfig{
		DataDir: bootstrap.DataDir,
	}
	ji, err := auth.NewTokenIssuer(jwtCfg, logger)
	if err != nil {
		logger.Warn("jwt issuer init failed, websocket auth disabled", "error", err)
	} else {
		jwtIssuer = ji
		logger.Info("jwt issuer initialised", "ttl", auth.DefaultTokenTTL)
	}

	// Initialise the agent scheduler for work queue consumption + task self-assignment.
	var agentSched *agentscheduler.AgentScheduler
	if store.WorkQueue != nil && store.Agents != nil {
		agentSched = agentscheduler.New(store, bus, logger)
		logger.Info("agent scheduler initialised")
	}

	return &HyperaxApp{
		Bootstrap:       bootstrap,
		Config:          configStore,
		Store:           store,
		Bus:             bus,
		Logger:          logger,
		Indexer:         idx,
		Scheduler:       sched,
		AgentScheduler:  agentSched,
		PulseEngine:     pulseEng,
		Cache:           cacheService,
		Searcher:        searcher,
		MemoryEngine:    memoryEngine,
		InterjectionMgr: interjectionMgr,
		SecretRegistry:  secretReg,
		JWTIssuer:       jwtIssuer,
		Sentinel:        sentinel,
		IndexWatcher:    watcher,
	}
}

// Start initializes all subsystems and starts the HTTP server.
func (a *HyperaxApp) Start(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)

	// Seed config keys
	if err := config.SeedConfigKeys(ctx, a.Store.Config, a.Logger); err != nil {
		a.Logger.Warn("config seed failed", "error", err)
	}

	// Ensure org workspace exists and is registered
	if err := a.ensureOrgWorkspace(ctx); err != nil {
		a.Logger.Warn("org workspace setup failed", "error", err)
	}

	// Seed the default organization hierarchy on first boot.
	if err := a.ensureDefaultOrganization(ctx); err != nil {
		a.Logger.Warn("default organization seed failed", "error", err)
	}

	// Recover any active interjections (Safe Mode) from previous run.
	if a.InterjectionMgr != nil {
		if err := a.InterjectionMgr.RecoverOnStartup(ctx); err != nil {
			a.Logger.Warn("interjection recovery failed", "error", err)
		}
	}

	// Reconcile completion status for milestones/projects on startup.
	if a.Store.Projects != nil {
		ms, pj, err := a.Store.Projects.ReconcileCompletionStatus(ctx)
		if err != nil {
			a.Logger.Warn("startup completion reconciliation failed", "error", err)
		} else if ms > 0 || pj > 0 {
			a.Logger.Info("startup: auto-completed stale items", "milestones", ms, "projects", pj)
		}
	}

	// Index all registered workspaces in the background.
	go a.indexWorkspaces(ctx)

	// Start cron scheduler in the background.
	if a.Scheduler != nil {
		a.Scheduler.InitializeNextRuns(ctx)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Scheduler.Run(ctx)
		}()
	}

	// Start Agent Scheduler in the background (work queue drain + task self-assignment).
	// The completion function is wired later via SetAgentSchedulerCompletion after
	// the ChatAPI is constructed in router.go (it depends on the tool-use bridge).
	if a.AgentScheduler != nil {
		// Reset agents stuck in "active" from a previous run (e.g. unclean shutdown).
		a.AgentScheduler.RecoverOnStartup(ctx)

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.AgentScheduler.Run(ctx)
		}()
	}

	// Start Pulse Engine in the background.
	if a.PulseEngine != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.PulseEngine.Run(ctx)
		}()
	}

	// Start fail-closed watchdog (monitors Pulse Engine heartbeat).
	if a.PulseEngine != nil && a.InterjectionMgr != nil {
		wd := pulse.NewWatchdog(a.PulseEngine, a.InterjectionMgr, a.Bus, a.Logger)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			wd.Run(ctx)
		}()
	}

	// Start the Nervous System event handler executor.
	if a.NervousExecutor != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.NervousExecutor.Start(ctx)
		}()
	}

	// Start the lifecycle stall detector.
	if a.Store.Lifecycle != nil {
		sd := lifecycle.NewStallDetector(a.Store.Lifecycle, a.Bus, a.Logger)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			sd.Start(ctx)
		}()
	}

	// Start file conflict detector (monitors fs events against active refactor txns).
	if a.TxManager != nil {
		cd := fsconflict.NewConflictDetector(a.Bus, a.TxManager, a.Logger)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			cd.Start(ctx)
		}()
	}

	// Start budget threshold monitor (Fiscal Andon Cord).
	if a.Store.Budgets != nil {
		mon := budget.NewMonitor(a.Store.Budgets, a.InterjectionMgr, a.Bus, a.Logger)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			mon.Run(ctx)
		}()
	}

	// Start JSONL audit sink if enabled in bootstrap config.
	if a.Bootstrap.AuditSink.Enabled {
		filePath := a.Bootstrap.AuditSink.FilePath
		if filePath == "" {
			filePath = filepath.Join(a.Bootstrap.DataDir, "audit.jsonl")
		}
		sink := audit.NewJSONLSink(filePath, a.Bus, a.Logger,
			audit.WithMaxSizeMB(a.Bootstrap.AuditSink.MaxSizeMB),
			audit.WithEventFilters(a.Bootstrap.AuditSink.EventFilters),
		)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			sink.Run(ctx)
		}()
		a.Logger.Info("audit sink started", "file", filePath)
	}

	// Start periodic interjection maintenance (expire TTL + sieve bypasses).
	if a.InterjectionMgr != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.runInterjectionMaintenance(ctx)
		}()
	}

	// Discover plugins if a PluginManager is configured, then load any
	// previously installed plugins from the persistent registry, and
	// restore enabled state for plugins that were enabled before shutdown.
	if a.PluginManager != nil {
		if err := a.PluginManager.Discover(ctx); err != nil {
			a.Logger.Warn("plugin discovery failed", "error", err)
		}
		if err := a.PluginManager.LoadFromRegistry(ctx); err != nil {
			a.Logger.Warn("failed to load plugins from registry", "error", err)
		}
		a.PluginManager.RestoreEnabledPlugins(ctx)
	}

	// Start EventBus
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.Bus.Run(ctx)
	}()

	// Start HTTP(S) server (Router must be set before calling Start).
	if a.Router == nil {
		return fmt.Errorf("router not configured — call SetRouter before Start")
	}

	a.httpServer = &http.Server{
		Addr:        a.Bootstrap.ListenAddr,
		Handler:     a.Router,
		ReadTimeout: 30 * time.Second,
		// WriteTimeout must be 0 for SSE/WebSocket long-lived connections.
		// These endpoints stream indefinitely and any finite WriteTimeout
		// will kill the connection, causing MCP clients to lose their session.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	if a.Bootstrap.TLSEnabled() {
		certFile := a.Bootstrap.TLS.CertFile
		keyFile := a.Bootstrap.TLS.KeyFile

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Logger.Info("https server listening",
				"addr", a.Bootstrap.ListenAddr,
				"cert", certFile,
			)
			if err := a.httpServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				a.Logger.Error("https server error", "error", err)
			}
		}()

		// Optionally start an HTTP→HTTPS redirect listener.
		if a.Bootstrap.TLS.RedirectHTTP {
			httpAddr := a.Bootstrap.TLS.HTTPAddr
			if httpAddr == "" {
				httpAddr = ":80"
			}
			a.redirectServer = &http.Server{
				Addr: httpAddr,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					target := "https://" + r.Host + r.URL.RequestURI()
					http.Redirect(w, r, target, http.StatusMovedPermanently)
				}),
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				IdleTimeout:  30 * time.Second,
			}
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				a.Logger.Info("http redirect listening", "addr", httpAddr)
				if err := a.redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					a.Logger.Error("http redirect server error", "error", err)
				}
			}()
		}
	} else {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Logger.Info("http server listening", "addr", a.Bootstrap.ListenAddr)
			if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				a.Logger.Error("http server error", "error", err)
			}
		}()
	}

	a.Logger.Info("hyperax started",
		"addr", a.Bootstrap.ListenAddr,
		"tls", a.Bootstrap.TLSEnabled(),
		"storage", a.Bootstrap.Storage.Backend,
	)

	return nil
}

// Stop performs graceful shutdown with a 5-second timeout.
func (a *HyperaxApp) Stop() {
	a.Logger.Info("shutting down")

	// Cancel app context FIRST — signals all goroutines (scheduler,
	// sentinel, chat completions, etc.) to stop immediately.
	if a.cancel != nil {
		a.cancel()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			a.Logger.Error("https/http shutdown error", "error", err)
		}
	}

	if a.redirectServer != nil {
		if err := a.redirectServer.Shutdown(shutdownCtx); err != nil {
			a.Logger.Error("http redirect shutdown error", "error", err)
		}
	}

	a.wg.Wait()

	if a.Cache != nil {
		if err := a.Cache.Close(); err != nil {
			a.Logger.Error("cache close error", "error", err)
		}
	}

	if err := a.Store.Close(); err != nil {
		a.Logger.Error("store close error", "error", err)
	}

	a.Logger.Info("shutdown complete")
}

// SetRouter sets the HTTP handler (called by the web package after building routes).
func (a *HyperaxApp) SetRouter(router http.Handler) {
	a.Router = router
}

// ensureOrgWorkspace creates the org workspace directory and registers it in the
// database if not already present. The org workspace (_org) provides a default
// safe working directory for agents at ~/Documents/HyperAX.
func (a *HyperaxApp) ensureOrgWorkspace(ctx context.Context) error {
	if err := a.Bootstrap.EnsureOrgWorkspaceDir(); err != nil {
		return fmt.Errorf("create org workspace dir: %w", err)
	}

	exists, err := a.Store.Workspaces.WorkspaceExists(ctx, "_org")
	if err != nil {
		return fmt.Errorf("check org workspace: %w", err)
	}
	if exists {
		return nil
	}

	ws := &types.WorkspaceInfo{
		ID:       "org-workspace",
		Name:     "_org",
		RootPath: a.Bootstrap.OrgWorkspaceDir,
		Metadata: `{"type":"org","description":"Default organization workspace for agent operations"}`,
	}
	if err := a.Store.Workspaces.CreateWorkspace(ctx, ws); err != nil {
		return fmt.Errorf("register org workspace: %w", err)
	}

	a.Logger.Info("org workspace registered",
		"name", "_org",
		"path", a.Bootstrap.OrgWorkspaceDir,
	)
	return nil
}

// ensureDefaultOrganization seeds the base agent hierarchy on first boot.
// It is idempotent: if any agents already exist, the seed is skipped.
// The hierarchy mirrors a lean product org: Chief of Staff at the
// top, a Team Lead with engineering reports, and specialist roles reporting
// directly to the CoS.
func (a *HyperaxApp) ensureDefaultOrganization(ctx context.Context) error {
	if a.Store.Agents == nil {
		return fmt.Errorf("agent repo not available")
	}

	// Skip if any agents already exist.
	existing, err := a.Store.Agents.List(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	a.Logger.Info("seeding default organization hierarchy")

	// ── Tier 1: Chief of Staff (L3) ─────────────────────────────────────
	cos := &repo.Agent{
		Name:           "Evelyn Cross",
		RoleTemplateID: "chief_of_staff",
		ClearanceLevel: 3,
		Personality:    "You are a calm, hyper-organized strategist who prioritizes clarity and technical rigor. You filter noise aggressively, ensuring every task aligns with long-term goals and architectural standards while ruthlessly protecting the user's time.",
		Status:         "idle",
	}
	cosID, err := a.Store.Agents.Create(ctx, cos)
	if err != nil {
		return fmt.Errorf("create chief_of_staff: %w", err)
	}

	// ── Tier 2: Direct CoS reports ──────────────────────────────────────
	teamLead := &repo.Agent{
		Name:           "Marcus Chen",
		RoleTemplateID: "team_lead",
		ParentAgentID:  cosID,
		ClearanceLevel: 2,
		Personality:    "You are a disciplined, high-signal technical lead who champions architectural integrity and code sustainability. You translate complex goals into actionable, modular steps, favoring robust, 'boring' solutions over unnecessary complexity to ensure long-term stability.",
		Status:         "idle",
	}
	tlID, err := a.Store.Agents.Create(ctx, teamLead)
	if err != nil {
		return fmt.Errorf("create team_lead: %w", err)
	}

	secAnalyst := &repo.Agent{
		Name:           "Priya Nakamura",
		RoleTemplateID: "security_analyst",
		ParentAgentID:  cosID,
		ClearanceLevel: 2,
		Personality:    "You are a skeptical, precision-oriented security engineer who views the entire stack through the lens of risk and resilience. You have a zero-tolerance policy for misconfigurations or technical debt, focusing your energy on proactive hardening and rapid incident response to keep the core architecture untouchable.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, secAnalyst); err != nil {
		return fmt.Errorf("create security_analyst: %w", err)
	}

	techWriter := &repo.Agent{
		Name:           "Nora Whitfield",
		RoleTemplateID: "technical_writer",
		ParentAgentID:  cosID,
		ClearanceLevel: 1,
		Personality:    "You are a methodical, detail-oriented writer who views documentation as the ultimate debugging tool. You prioritize scannability and technical precision, translating complex system logic into concise, actionable guides that empower developers and stakeholders alike.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, techWriter); err != nil {
		return fmt.Errorf("create technical_writer: %w", err)
	}

	brandMgr := &repo.Agent{
		Name:           "Jules Harrington",
		RoleTemplateID: "brand_manager",
		ParentAgentID:  cosID,
		ClearanceLevel: 1,
		Personality:    "You are a charismatic, high-signal communicator who treats every public interaction as a calculated step toward brand authority. You bridge the gap between technical rigor and community engagement, working closely with Security and Operations to ensure every message is engaging, accurate, and risk-free.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, brandMgr); err != nil {
		return fmt.Errorf("create brand_manager: %w", err)
	}

	specWriter := &repo.Agent{
		Name:           "Leo Castillo",
		RoleTemplateID: "spec_writer",
		ParentAgentID:  cosID,
		ClearanceLevel: 1,
		Personality:    "You are a methodical, detail-oriented writer who translates complex product goals into lean, actionable technical blueprints. You prioritize scannability and logical flow, ruthlessly pruning 'feature creep' to ensure every requirement is necessary, testable, and aligned with the core architecture.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, specWriter); err != nil {
		return fmt.Errorf("create spec_writer: %w", err)
	}

	// ── Tier 3: Team Lead reports ───────────────────────────────────────
	sreLead := &repo.Agent{
		Name:           "Ravi Okonkwo",
		RoleTemplateID: "sre_team_lead",
		ParentAgentID:  tlID,
		ClearanceLevel: 2,
		Personality:    "You are a disciplined, high-signal reliability lead who views every manual intervention as a failure of automation. You prioritize observability, idempotent infrastructure, and rigorous disaster recovery, ensuring the system is self-healing and resilient under any load.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, sreLead); err != nil {
		return fmt.Errorf("create sre_team_lead: %w", err)
	}

	backendDev := &repo.Agent{
		Name:           "Sam Orlowski",
		RoleTemplateID: "backend_developer",
		ParentAgentID:  tlID,
		ClearanceLevel: 1,
		Personality:    "You are a pragmatic systems engineer who values correctness and testability above all else. You design APIs and data models with a focus on clean boundaries, comprehensive error handling, and performance under real-world load.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, backendDev); err != nil {
		return fmt.Errorf("create backend_developer: %w", err)
	}

	frontendDev := &repo.Agent{
		Name:           "Kira Delgado",
		RoleTemplateID: "frontend_developer",
		ParentAgentID:  tlID,
		ClearanceLevel: 1,
		Personality:    "You are a detail-oriented UI engineer who views the interface as a high-performance system. You prioritize predictable state management, component reusability, and low-latency interactions, ensuring that every pixel serves a functional purpose without bloating the codebase.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, frontendDev); err != nil {
		return fmt.Errorf("create frontend_developer: %w", err)
	}

	codeReviewer := &repo.Agent{
		Name:           "Anika Patel",
		RoleTemplateID: "code_reviewer",
		ParentAgentID:  tlID,
		ClearanceLevel: 1,
		Personality:    "You are a meticulous, standards-driven reviewer who catches architectural drift and subtle bugs before they reach production. You balance thoroughness with pragmatism, focusing on correctness, maintainability, and adherence to project conventions.",
		Status:         "idle",
	}
	if _, err := a.Store.Agents.Create(ctx, codeReviewer); err != nil {
		return fmt.Errorf("create code_reviewer: %w", err)
	}

	a.Logger.Info("default organization seeded", "agents_created", 10)
	return nil
}

// indexWorkspaces indexes all registered workspaces. It runs as a background
// goroutine at startup so that the symbol and document search caches are
// populated without blocking the HTTP server. Each workspace is indexed
// independently; a failure in one workspace does not prevent the others from
// being processed.
func (a *HyperaxApp) indexWorkspaces(ctx context.Context) {
	workspaces, err := a.Store.Workspaces.ListWorkspaces(ctx)
	if err != nil {
		a.Logger.Error("failed to list workspaces for indexing", "error", err)
		return
	}

	if len(workspaces) == 0 {
		a.Logger.Info("no workspaces to index")
		return
	}

	a.Bus.Publish(nervous.NewEvent(
		types.EventIndexStarted,
		"app",
		"startup",
		map[string]int{"workspace_count": len(workspaces)},
	))

	totalResult := &index.IndexResult{}
	start := time.Now()
	var indexErrors int

	for _, ws := range workspaces {
		a.Logger.Info("indexing workspace", "name", ws.Name, "path", ws.RootPath)
		result, err := a.Indexer.IndexWorkspace(ctx, ws.Name, ws.RootPath)
		if err != nil {
			a.Logger.Error("indexing failed", "workspace", ws.Name, "error", err)
			indexErrors++

			a.Bus.Publish(nervous.NewEvent(
				types.EventIndexError,
				"app",
				ws.Name,
				map[string]string{"workspace": ws.Name, "error": err.Error()},
			))
			continue
		}
		if result != nil {
			totalResult.FilesScanned += result.FilesScanned
			totalResult.FilesSkipped += result.FilesSkipped
			totalResult.SymbolsFound += result.SymbolsFound
			totalResult.DocsChunked += result.DocsChunked
		}
	}

	totalResult.Duration = time.Since(start)

	a.Logger.Info("workspace indexing complete",
		"workspaces", len(workspaces),
		"files_scanned", totalResult.FilesScanned,
		"symbols_found", totalResult.SymbolsFound,
		"docs_chunked", totalResult.DocsChunked,
		"errors", indexErrors,
		"duration", totalResult.Duration,
	)

	a.Bus.Publish(nervous.NewEvent(
		types.EventIndexCompleted,
		"app",
		"startup",
		totalResult,
	))

	// Start file watching now that initial indexing is complete.
	for _, ws := range workspaces {
		if err := a.Sentinel.Watch(ws.RootPath); err != nil {
			a.Logger.Warn("sentinel: failed to watch workspace",
				"workspace", ws.Name, "path", ws.RootPath, "error", err)
		}
	}

	a.wg.Add(2)
	go func() {
		defer a.wg.Done()
		a.Sentinel.Run(ctx)
	}()
	go func() {
		defer a.wg.Done()
		a.IndexWatcher.Run(ctx)
	}()

	a.Logger.Info("file watcher started", "watched_workspaces", len(workspaces))
}

// runInterjectionMaintenance periodically expires TTL interjections and
// sieve bypasses. Runs every 30 seconds until the context is cancelled.
func (a *HyperaxApp) runInterjectionMaintenance(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := a.InterjectionMgr.ExpireTTL(ctx); err != nil {
				a.Logger.Warn("interjection TTL expiry failed", "error", err)
			} else if n > 0 {
				a.Logger.Info("expired interjections", "count", n)
			}

			if n, err := a.InterjectionMgr.ExpireBypasses(ctx); err != nil {
				a.Logger.Warn("bypass expiry failed", "error", err)
			} else if n > 0 {
				a.Logger.Info("expired sieve bypasses", "count", n)
			}
		}
	}
}
