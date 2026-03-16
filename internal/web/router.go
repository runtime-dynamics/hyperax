package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hyperax/hyperax/catalog"
	"github.com/hyperax/hyperax/internal/agentmail"
	"github.com/hyperax/hyperax/internal/app"
	"github.com/hyperax/hyperax/internal/audit"
	"github.com/hyperax/hyperax/internal/auth"
	"github.com/hyperax/hyperax/internal/channelbridge"
	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/cron"
	"github.com/hyperax/hyperax/internal/delegation"
	"github.com/hyperax/hyperax/internal/federation"
	"github.com/hyperax/hyperax/internal/guard"
	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/hints/providers"
	"github.com/hyperax/hyperax/internal/lifecycle"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/mcp/handlers"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/observability"
	"github.com/hyperax/hyperax/internal/plugin"
	"github.com/hyperax/hyperax/internal/pulse"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/internal/telemetry"
	"github.com/hyperax/hyperax/internal/tooluse"
	"github.com/hyperax/hyperax/internal/web/api"
	"github.com/hyperax/hyperax/internal/web/render"
	"github.com/hyperax/hyperax/internal/workflow"
	"github.com/hyperax/hyperax/pkg/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// BuildRouter creates the chi router with all routes wired up.
// uiFS is the embedded React SPA filesystem (ui/dist contents).
// sqlDB is the underlying sql.DB for handlers that need direct queries.
func BuildRouter(application *app.HyperaxApp, uiFS fs.FS, sqlDB *sql.DB) http.Handler {
	startTime := time.Now()
	render.SetLogger(application.Logger)
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(observability.PrometheusMiddleware)

	// Create MCP server and register all handlers
	mcpServer := mcp.NewServer(application.Logger)
	wsHandler := handlers.NewWorkspaceHandler(application.Store, application.Indexer, application.IndexWatcher)
	mcpServer.RegisterHandler(wsHandler)
	mcpServer.RegisterHandler(handlers.NewProjectHandler(application.Store))
	mcpServer.RegisterHandler(handlers.NewDocHandler(application.Store, application.Indexer))
	pipelineHandler := handlers.NewPipelineHandler(application.Store, application.Bus, application.Logger)
	mcpServer.RegisterHandler(pipelineHandler)
	mcpServer.RegisterHandler(handlers.NewAuditHandler(application.Store))
	codeHandler := handlers.NewCodeHandler(application.Store, sqlDB)
	codeHandler.SetContextDeps(application.Bus, application.Logger)
	mcpServer.RegisterHandler(codeHandler)
	configHandler := handlers.NewConfigHandler(application.Config, application.Store.Config)
	configHandler.SetProviderDeps(application.Store)
	configHandler.SetEventBus(application.Bus)
	if application.Store.MCPTokens != nil {
		configHandler.SetAuthDeps(application.Store.MCPTokens, application.Store.Agents, application.Logger)
	}
	mcpServer.RegisterHandler(configHandler)
	agentHandler := handlers.NewAgentHandler(application.Store)
	agentHandler.SetLifecycleDeps(application.Bus)
	agentHandler.TemplateRegistry().LoadOverrides(context.Background())
	application.RoleTemplateRegistry = agentHandler.TemplateRegistry()
	mcpServer.RegisterHandler(agentHandler)
	// Observability handler (consolidated: telemetry, budget, logs, metrics)
	obsHandler := handlers.NewObservabilityHandler(application.Store, application.Logger)
	mcpServer.RegisterHandler(obsHandler)

	// Phase 2 handlers: Memory, Secret (Lifecycle + Delegation absorbed into agent)
	mcpServer.RegisterHandler(handlers.NewMemoryHandler(application.MemoryEngine))
	agentHandler.SetMemoryDeps(application.MemoryEngine)
	mcpServer.RegisterHandler(handlers.NewSecretHandler(application.Store, application.SecretRegistry))

	// On-Behalf-Of Delegation (wired into consolidated agent handler)
	if application.Store.Delegations != nil {
		dlgSvc := delegation.NewService(
			application.Store.Delegations,
			application.SecretRegistry,
			application.Bus,
			application.Logger,
		)
		agentHandler.SetDelegationDeps(dlgSvc)
	}


	// Refactoring toolkit with transaction manager
	txMgr := refactor.NewTransactionManager(application.Logger)
	application.TxManager = txMgr
	mcpServer.RegisterHandler(handlers.NewRefactorHandler(application.Store, txMgr, application.Logger))

	// CommHub (message routing with Context Sieve + proactive memory recall)
	hub := commhub.NewCommHub(application.Bus, application.Logger)
	if application.Store.Memory != nil {
		hub.SetRecallFunc(commhub.BuildRecallFunc(application.Store.Memory, application.Logger))
	}
	var commLogger *commhub.CommLogger
	if application.Store.CommHub != nil {
		commLogger = commhub.NewCommLogger(application.Store.CommHub, application.Logger)
		// Wire overflow persistence: dropped messages are stored in DB.
		hub.SetOverflowPersister(func(ctx context.Context, entry *types.OverflowEntry) error {
			return application.Store.CommHub.PersistOverflow(ctx, entry)
		})
		hub.SetOverflowDrainer(func(ctx context.Context, agentID string, limit int) ([]*types.OverflowEntry, error) {
			return application.Store.CommHub.DrainOverflow(ctx, agentID, limit)
		})
	}
	commHandler := handlers.NewCommHandler(handlers.CommHandlerDeps{
		Hub:     hub,
		CommLog: commLogger,
		Store:   application.Store,
		Logger:  application.Logger,
	})

	// Wire lifecycle onboarding into CommHub so OnboardAgent() routes
	// all step progression messages through the Context Sieve.
	if application.Store.Lifecycle != nil && application.Store.Agents != nil {
		onboardDeps := lifecycle.OnboardingDeps{
			LifecycleRepo: application.Store.Lifecycle,
			AgentRepo:     application.Store.Agents,
			CommHubRepo:   application.Store.CommHub,
			MemoryRepo:    application.Store.Memory,
			ProjectRepo:   application.Store.Projects,
			Hub:           hub,
			Bus:           application.Bus,
			Logger:        application.Logger,
		}
		onboarder := lifecycle.NewOnboarder(onboardDeps)
		hub.SetOnboardFunc(func(ctx context.Context, agentID, personaID, parentAgentID, workspaceID string) (map[string]any, error) {
			result, err := onboarder.Onboard(ctx, agentID, personaID, parentAgentID, workspaceID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"agent_id":        result.AgentID,
				"persona_id":      result.PersonaID,
				"inbox_created":   result.InboxCreated,
				"relationships":   result.Relationships,
				"permissions":     result.Permissions,
				"memories_loaded": result.MemoriesLoaded,
				"tasks_assigned":  result.TasksAssigned,
			}, nil
		})
	}

	// Wire rehydration into CommHub so zombie agents are brought back to life
	// before message delivery. Uses the Rehydrator from the lifecycle package.
	if application.Store.Lifecycle != nil && application.Store.Checkpoints != nil && application.TxManager != nil {
		rehydrator := lifecycle.NewRehydrator(
			application.Store.Lifecycle,
			application.Store.Checkpoints,
			application.TxManager,
			hub,
			application.Bus,
			application.Logger,
		)

		// Zombie detector: checks if the agent's last heartbeat is stale.
		hub.SetZombieDetector(func(ctx context.Context, agentID string) bool {
			state, err := application.Store.Lifecycle.GetState(ctx, agentID)
			if err != nil {
				return false // Unknown agent — not a zombie.
			}
			// An agent in the error state with an expired heartbeat is a zombie.
			return state == string(lifecycle.StateError)
		})

		// Rehydration function: transitions agent to rehydrating, then rehydrates.
		hub.SetRehydrationFunc(func(ctx context.Context, agentID string) (map[string]any, error) {
			// Transition from error to rehydrating first.
			transition := &repo.LifecycleTransition{
				AgentID:   agentID,
				FromState: string(lifecycle.StateError),
				ToState:   string(lifecycle.StateRehydrating),
				Reason:    "pre-delivery rehydration triggered by CommHub",
			}
			if err := application.Store.Lifecycle.LogTransition(ctx, transition); err != nil {
				return nil, fmt.Errorf("web.BuildRouter.rehydrationFunc: transition to rehydrating: %w", err)
			}

			result, err := rehydrator.Rehydrate(ctx, agentID)
			if err != nil {
				return nil, fmt.Errorf("web.BuildRouter.rehydrationFunc: rehydrate: %w", err)
			}
			return map[string]any{
				"agent_id":             result.AgentID,
				"checkpoint_restored":  result.CheckpointRestored,
				"refactor_rolled_back": result.RefactorRolledBack,
				"messages_replayed":    result.MessagesReplayed,
				"fell_back_to_onboard": result.FellBackToOnboard,
			}, nil
		})
	}

	// Cron scheduler (wired into consolidated pipeline handler)
	if application.Scheduler != nil {
		pipelineHandler.SetCronDeps(application.Scheduler)
	}

	// Context generation is now part of the consolidated "code" tool.

	// Hints Engine (wired into consolidated agent handler)
	hintEngine := hints.NewEngine(application.Logger)
	hintEngine.RegisterProvider(providers.NewStandardsProvider(application.Store.Config))
	hintEngine.RegisterProvider(providers.NewErrorsProvider(application.Store.Metrics))
	hintEngine.RegisterProvider(providers.NewPatternsProvider(application.Store.Symbols, application.Store.Search))
	hintEngine.RegisterProvider(providers.NewDocsProvider(application.Store.Search))
	hintEngine.RegisterProvider(providers.NewGitProvider(application.Store.Git))
	hintEngine.RegisterProvider(providers.NewMemoryProvider(application.Store.Memory))
	agentHandler.SetHintsDeps(hintEngine)

	// Pulse Engine wiring (agent order sender routes through CommHub)
	if application.PulseEngine != nil {
		application.PulseEngine.SetAgentOrderSender(func(ctx context.Context, targetAgent, cadenceName, cadenceID string, payload any) error {
			payloadJSON, _ := json.Marshal(map[string]any{
				"type":         "agent_order",
				"cadence_id":   cadenceID,
				"cadence_name": cadenceName,
				"payload":      payload,
			})
			env := &types.MessageEnvelope{
				ID:          "cadence-" + cadenceID,
				From:        "system:pulse",
				To:          targetAgent,
				Trust:       types.TrustInternal,
				ContentType: "application/json",
				Content:     string(payloadJSON),
				Metadata: map[string]string{
					"cadence_id":   cadenceID,
					"cadence_name": cadenceName,
					"mode":         "agent_order",
				},
			}
			return hub.Send(ctx, env)
		})
	}

	// Sensor cadence manager
	sensorMgr := pulse.NewSensorManager(application.Bus, application.Logger)
	if application.SecretRegistry != nil {
		sensorMgr.SetSecretResolver(secrets.BuildResolverFunc(application.SecretRegistry))
	} else if application.Store.Secrets != nil {
		sensorMgr.SetSecretResolver(pulse.BuildSecretResolver(application.Store.Secrets))
	}
	application.SensorManager = sensorMgr

	// Plugin system tools
	pluginDir := filepath.Join(application.Bootstrap.DataDir, "plugins")
	pm := plugin.NewPluginManager(mcpServer.Registry, application.Bus, application.Logger, pluginDir, application.Version)

	// Wire config/secret resolver for plugin variable injection.
	if application.Config != nil || application.SecretRegistry != nil {
		resolver := &plugin.PluginConfigResolver{}
		if application.Config != nil {
			cfgStore := application.Config
			resolver.GetVar = func(ctx context.Context, pluginName, varName string) (string, error) {
				key := "plugin." + pluginName + ".var." + varName
				return cfgStore.Resolve(ctx, key, "", "")
			}
		}
		if application.SecretRegistry != nil {
			secReg := application.SecretRegistry
			resolver.ResolveSecret = func(ctx context.Context, ref string) (string, error) {
				return secrets.ResolveSecretRef(ctx, secReg, ref)
			}
		}
		pm.SetConfigResolver(resolver)
	}

	// Wire CronRepo for auto-creating plugin resources (cron jobs).
	if application.Store.Cron != nil {
		pm.SetCronRepo(application.Store.Cron)
	}

	// Wire ConfigKeySeeder so plugin variables appear in list_config_keys.
	if application.Store.Config != nil {
		pm.SetConfigKeySeeder(application.Store.Config)
	}

	// Wire PluginRepo so enabled/disabled state persists across restarts.
	if application.Store.Plugins != nil {
		pm.SetPluginRepo(application.Store.Plugins)
	}

	// Wire secret registry bridge so secret_provider plugins can register/unregister
	// as secrets.Provider implementations when enabled/disabled.
	if application.SecretRegistry != nil {
		secReg := application.SecretRegistry
		pm.SetSecretBridge(&plugin.SecretRegistryBridge{
			RegisterAdapter: func(adapter *plugin.PluginSecretAdapter) {
				secReg.Register(adapter)
			},
			UnregisterAdapter: func(name string) {
				secReg.Unregister(name)
			},
			IsActive: func(name string) bool {
				return secReg.IsActive(name)
			},
			HasSecrets: func(ctx context.Context, name string) (bool, error) {
				// Check if the provider has any secrets by listing them.
				p := secReg.Get(name)
				if p == nil {
					return false, nil
				}
				keys, err := p.List(ctx, "global")
				if err != nil {
					return false, err
				}
				return len(keys) > 0, nil
			},
		})
	}

	// Wire approval gate into the event bridge for blocking unapproved plugin events.
	var approvalGate *plugin.ApprovalGate
	if application.Config != nil {
		approvalGate = plugin.NewApprovalGate(application.Config, application.Logger)
		if eb := pm.EventBridge(); eb != nil {
			eb.SetApprovalGate(approvalGate)
			eb.SetManifestLookup(pm.GetManifest)
		}
	}

	pluginHandler := handlers.NewPluginHandler(pm, application.Config, application.SecretRegistry)
	if approvalGate != nil {
		pluginHandler.SetApprovalGate(approvalGate)
	}
	pluginHandler.SetToolRegistry(mcpServer.Registry)
	mcpServer.RegisterHandler(pluginHandler)
	application.PluginManager = pm

	// Plugin Catalog — wire into the consolidated plugin handler.
	catalogMgr, catalogErr := plugin.NewCatalogManager(catalog.PluginsYAML, pm, application.Logger)
	if catalogErr != nil {
		application.Logger.Error("failed to load plugin catalog", "error", catalogErr)
	} else {
		catalogMgr.SetRegistry(pm.InstallRegistry())
		pluginHandler.SetCatalogDeps(catalogMgr, application.Logger)
	}

	// Workflow Engine (wired into consolidated pipeline handler)
	if application.Store.Workflows != nil {
		wfExecutor := workflow.NewExecutor(application.Store.Workflows, application.Bus, application.Logger)
		pipelineHandler.SetWorkflowDeps(wfExecutor)
	}

	// Enhanced Nervous System tools
	ringBuffer := nervous.NewRingBuffer(10000)
	application.RingBuffer = ringBuffer

	// Feed the RingBuffer from the EventBus so that late-join WebSocket
	// clients can replay missed events via ?since=N.
	rbSub := application.Bus.Subscribe("ring-buffer-feeder", nil)
	go func() {
		for event := range rbSub.Ch {
			ringBuffer.Push(event)
		}
	}()

	// Event Handler Executor — runs declarative handlers against live events.
	var nervousExecutor *nervous.Executor
	if application.Store.Nervous != nil {
		nervousExecutor = nervous.NewExecutor(application.Store.Nervous, application.Bus, application.Logger)
		application.NervousExecutor = nervousExecutor
	}

	// Consolidated event handler (nervous + pulse + sensor)
	eventHandler := handlers.NewEventHandler(application.Store.Nervous, application.Bus, ringBuffer, nervousExecutor, application.Logger)
	if application.PulseEngine != nil {
		eventHandler.SetPulseDeps(application.PulseEngine)
	}
	eventHandler.SetSensorDeps(sensorMgr)
	mcpServer.RegisterHandler(eventHandler)

	// Wire CommHub extended deps (hierarchy, logging, permissions, broadcast) into commHandler.
	if application.Store.CommHub != nil {
		hierarchyMgr := commhub.NewHierarchyManager(application.Store.CommHub, application.Bus, application.Logger)
		commHandler.SetExtDeps(application.Store.CommHub, hierarchyMgr, application.Bus, application.Store.Agents)

		// Wire cascading halt resolver: allows interjection manager to walk
		// the agent hierarchy when propagating halts to child agents.
		if application.InterjectionMgr != nil {
			commHubRepo := application.Store.CommHub
			application.InterjectionMgr.SetChildResolver(func(ctx context.Context, parentAgent string) ([]string, error) {
				rels, err := commHubRepo.GetChildren(ctx, parentAgent)
				if err != nil {
					return nil, err
				}
				ids := make([]string, len(rels))
				for i, r := range rels {
					ids[i] = r.ChildAgent
				}
				return ids, nil
			})

			// Wire remediation dispatch: sends a CommHub message to the
			// configured remediation persona when a halt fires.
			application.InterjectionMgr.SetRemediationDispatch(func(ctx context.Context, personaID, interjectionID, description string) error {
				env := &types.MessageEnvelope{
					ID:          "remediation-" + interjectionID,
					From:        "system:andon",
					To:          personaID,
					Trust:       types.TrustInternal,
					ContentType: "remediation_dispatch",
					Content:     description,
					Metadata: map[string]string{
						"interjection_id": interjectionID,
						"dispatch_type":   "auto_remediation",
					},
				}
				return hub.Send(ctx, env)
			})

			// Wire config lookup for default remediation persona.
			if application.Store.Config != nil {
				cfgRepo := application.Store.Config
				application.InterjectionMgr.SetConfigLookup(func(ctx context.Context, key string) (string, error) {
					return cfgRepo.GetValue(ctx, key, types.ConfigScope{Type: "global"})
				})
			}
		}
	}

	// Wire session deps into commHandler.
	if application.Store.Sessions != nil {
		commHandler.SetSessionDeps(application.Store.Sessions)
	}

	// Telemetry tools
	var sessionTracker *telemetry.SessionTracker
	if application.Store.Telemetry != nil {
		tCollector := telemetry.NewCollector()
		sessionTracker = telemetry.NewSessionTracker(application.Store.Telemetry, application.Bus, application.Logger)
		tAlerts := telemetry.NewAlertEvaluator(application.Store.Telemetry, application.Bus, application.Logger)
		obsHandler.SetTelemetryDeps(application.Store.Telemetry, sessionTracker, tAlerts, tCollector)

		// Start the AlertEvaluator periodic evaluation loop. It checks all
		// enabled alerts against current metrics at a configurable interval
		// (default 60s) and publishes EventTelemetryAlert events when thresholds
		// are breached. Runs until the server shuts down.
		alertInterval := 60 * time.Second
		if application.Config != nil {
			if v, err := application.Config.Resolve(context.Background(), "telemetry.alert_interval", "", ""); err == nil && v != "" {
				if d, parseErr := time.ParseDuration(v); parseErr == nil && d > 0 {
					alertInterval = d
				}
			}
		}
		go tAlerts.Run(context.Background(), alertInterval)
		application.Logger.Info("alert evaluator started", "interval", alertInterval)
	}

	// Wire AgentMail deps into commHandler.
	if application.Store.AgentMail != nil {
		amPostbox := agentmail.NewPostbox(application.Store.AgentMail, application.Bus, application.Logger)
		amRegistry := agentmail.NewAdapterRegistry(application.Logger)
		amAckTracker := agentmail.NewAckTracker(application.Bus, application.Logger)
		amDLO := agentmail.NewDeadLetterOffice(application.Bus, application.Logger)
		commHandler.SetMailDeps(amPostbox, amRegistry, amAckTracker, amDLO)
	}

	// Register the consolidated comm handler after all deps are wired.
	mcpServer.RegisterHandler(commHandler)

	// Memory context injection for MCP tool responses (User-Led mode).
	if application.Store.Memory != nil {
		injector := mcp.NewMemoryContextInjector(application.Store.Memory, application.Logger)
		if injector != nil {
			mcpServer.Registry.SetContextInjector(injector)
		}
	}

	// Apply ABAC clearance levels to all registered tools.
	mcp.ApplyDefaultABACLevels(mcpServer.Registry)

	// Build tool schemas for resolver (before guard/ABAC wrapping).
	mcpSchemas := mcpServer.Registry.Schemas()
	toolSchemas := make([]tooluse.ToolSchema, len(mcpSchemas))
	for i, s := range mcpSchemas {
		toolSchemas[i] = tooluse.ToolSchema{
			Name:              s.Name,
			Description:       s.Description,
			InputSchema:       s.InputSchema,
			MinClearanceLevel: s.MinClearanceLevel,
			RequiredAction:    s.RequiredAction,
			ExposedToLLM:      s.ExposedToLLM,
		}
	}
	toolResolver := tooluse.NewResolver(toolSchemas)

	// MCP transport with optional token authentication and ABAC enforcement.
	transport := mcp.NewSSETransport(mcpServer, application.Logger)
	if application.Store.MCPTokens != nil {
		mcpAuth := mcp.NewAuthenticator(application.Store.MCPTokens, application.Store.Config, application.Logger)
		mcpAuth.SetEventBus(application.Bus)
		// Wire guard_bypass resolution from agent records.
		if application.Store.Agents != nil {
			agentRepo := application.Store.Agents
			mcpAuth.SetGuardBypassResolver(func(ctx context.Context, personaID string) bool {
				// Try agent lookup first (personaID may actually be an agent ID after consolidation).
				agent, err := agentRepo.Get(ctx, personaID)
				if err != nil {
					return false
				}
				return agent.GuardBypass
			})
		}
		transport.SetAuthenticator(mcpAuth)
	}
	abac := mcp.NewABACMiddleware(mcpServer.Registry, application.Logger)
	transport.SetABAC(abac)

	// Guard middleware — intercepts tool calls that require human approval.
	// Uses function injection to avoid circular imports between guard and mcp.
	guardMgr := guard.NewActionManager(application.Bus, application.Logger)
	toolActionLookup := func(toolName string) string {
		schema := mcpServer.Registry.GetSchema(toolName)
		if schema == nil {
			return ""
		}
		return schema.RequiredAction
	}
	guardMw := guard.NewGuardMiddleware(toolActionLookup, mcp.AuthFromContext, guardMgr, application.Config, application.Logger)
	guardMw.RegisterGuard(guard.NewApproveWritesGuard(5 * time.Minute))
	transport.SetGuard(guardMw)
	mcpServer.RegisterHandler(handlers.NewGovernanceHandler(guardMgr, application.InterjectionMgr))

	// Wire guard bridge so guard-type plugins register their evaluator with the
	// guard middleware when enabled, and unregister when disabled.
	pm.SetGuardBridge(&plugin.GuardBridge{
		RegisterGuard: func(pluginName, toolName string, dispatch func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error), timeout time.Duration) {
			guardMw.RegisterGuard(guard.NewPluginGuard(pluginName, toolName, dispatch, timeout))
		},
		UnregisterGuard: func(pluginName string) {
			guardMw.UnregisterGuard(pluginName)
		},
	})

	// Audit stream — subscribe to comm.* events and forward to audit plugins.
	auditSink := audit.NewPluginAuditSink(application.Bus, application.Logger, 1000)
	go auditSink.Run(context.Background())
	pm.SetAuditBridge(&plugin.AuditBridge{
		RegisterWriter: func(pluginName string, dispatch func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)) {
			auditSink.RegisterWriter(pluginName, func(ctx context.Context, event *audit.AuditEvent) error {
				payload, err := json.Marshal(event)
				if err != nil {
					return fmt.Errorf("marshal audit event: %w", err)
				}
				_, callErr := dispatch(ctx, "write_audit_event", payload)
				return callErr
			})
		},
		UnregisterWriter: func(pluginName string) {
			auditSink.UnregisterWriter(pluginName)
		},
	})

	// MCP Federation — connect to external MCP servers and proxy their tools.
	fedManager := federation.NewManager(mcpServer.Registry, application.Logger)
	wsHandler.SetFederationManager(fedManager)

	// Build tool-use bridge with ABAC + Guard wrapping for agentic completions.
	guardedDispatch := guardMw.WrapDispatch(abac.WrapDispatch(mcpServer.Registry.Dispatch))
	application.ToolUseBridge = tooluse.NewBridge(toolResolver, guardedDispatch, application.Logger)
	application.Logger.Info("tool-use bridge initialised", "tools", len(toolSchemas))

	// Create ChatAPI early so we can wire its completion trigger into CommHandler
	// and the Agent Scheduler. This bridges MCP send_message with the LLM completion
	// loop: when an agent sends a message to another agent via the MCP tool, the
	// message is enqueued to the durable work queue and the Agent Scheduler picks it up.
	chatAPI := api.NewChatAPI(hub, commLogger, application.Store, application.Bus, application.ToolUseBridge, application.RoleTemplateRegistry, application.Logger)
	if sessionTracker != nil {
		chatAPI.SetSessionTracker(sessionTracker)
	}
	commHandler.SetCompletionTrigger(chatAPI.TriggerCompletion)

	// Wire the Agent Scheduler's completion function to call ChatAPI synchronously.
	// The scheduler's drain loop processes queue items one at a time, blocking on
	// each completion before moving to the next.
	if application.AgentScheduler != nil {
		application.AgentScheduler.SetCompletionFunc(chatAPI.GenerateResponseSync)
	}

	// Cron Executor: listens for cron.fire events and executes job payloads.
	// Uses the MCP dispatch function so cron jobs can invoke any registered tool.
	if application.Scheduler != nil && application.Store.Cron != nil {
		cronExec := cron.NewExecutor(
			application.Store.Cron,
			application.Bus,
			mcpServer.Registry.Dispatch,
			application.Logger,
		)
		go cronExec.Run(context.Background())
		application.Logger.Info("cron executor started")
	}

	// Channel Bridge: routes messages between external plugins (Discord, Slack, Email)
	// and internal agents. Reads routing config from plugin variables (plugin.<name>.channel_routes).
	{
		bridge := channelbridge.New(
			application.Store,
			application.Bus,
			hub,
			commLogger,
			guardedDispatch,
			application.Logger,
		)
		bridge.SetCompletionFunc(chatAPI.TriggerCompletion)

		// Wire Security Lead review function for untrusted routes.
		reviewFn := channelbridge.BuildSecurityReviewFunc(
			application.Store,
			chatAPI.TriggerCompletion,
			hub,
			application.Bus,
			application.Logger,
		)
		bridge.SetSecurityReviewFunc(reviewFn)

		// Resolve root agent name from config if set.
		if application.Config != nil {
			if rootAgent, err := application.Config.Resolve(context.Background(), "channel_bridge.root_agent", "", ""); err == nil && rootAgent != "" {
				bridge.SetRootAgent(rootAgent)
			}
		}

		application.ChannelBridge = bridge
		go bridge.Run(context.Background())
		application.Logger.Info("channel bridge started")
	}

	// MCP protocol endpoints
	r.Get("/mcp/sse", transport.HandleSSE)
	r.Post("/mcp/messages", transport.HandleMessage)
	r.Post("/mcp", transport.HandleStreamableHTTP)

	// WebSocket events bridge (with late-join replay via ring buffer)
	wsHub := NewWSHub(application.Bus, ringBuffer, application.Logger)
	if application.JWTIssuer != nil {
		wsHub.SetJWTIssuer(application.JWTIssuer)
	}
	r.Get("/ws/events", wsHub.HandleWebSocket)

	// JWT token endpoint: exchanges a valid MCP token for a short-lived JWT.
	// The TokenValidator is nil until the MCP token auth system is wired in;
	// in that state the endpoint returns 503 Service Unavailable.
	// Rate-limited per IP to prevent brute-force token guessing.
	tokenRateLimiter := auth.NewRateLimiter(auth.DefaultRateLimitRPM, application.Logger)
	tokenHandler := auth.HandleTokenEndpoint(application.JWTIssuer, nil, application.Logger)
	r.Post("/auth/token", tokenRateLimiter.Middleware(tokenHandler).ServeHTTP)

	// REST API
	r.Route("/api", func(r chi.Router) {
		r.Get("/status", handleStatus(application, mcpServer.Registry.ToolCount(), startTime))

		// v1 REST endpoints for dashboard consumption
		r.Route("/v1", func(r chi.Router) {
			r.Mount("/workspaces", api.NewWorkspaceAPI(application.Store.Workspaces).Routes())
			r.Mount("/config", api.NewConfigAPI(application.Store.Config).Routes())
			r.Mount("/providers", api.NewProviderAPI(application.Store.Providers, application.Store.Secrets).Routes())
			r.Mount("/pipelines", api.NewPipelineAPI(application.Store.Pipelines).Routes())
			r.Mount("/chat", chatAPI.Routes())
			if application.InterjectionMgr != nil {
				r.Mount("/interjections", api.NewInterjectionAPI(application.InterjectionMgr).Routes())
			}
		})
	})

	// Health endpoint
	r.Get("/health", handleHealth)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	// SPA handler — serves React UI
	if uiFS != nil {
		r.Handle("/*", newSPAHandler(uiFS))
	}

	application.Logger.Info("router configured",
		"tools", mcpServer.Registry.ToolCount(),
	)

	return r
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	render.Success(w, r)
}

func handleStatus(application *app.HyperaxApp, toolCount int, startTime time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsCount := 0
		if wsList, err := application.Store.Workspaces.ListWorkspaces(r.Context()); err == nil {
			wsCount = len(wsList)
		}

		status := map[string]interface{}{
			"version":         "0.1.0",
			"storage":         application.Bootstrap.Storage.Backend,
			"tool_count":      toolCount,
			"uptime_seconds":  int(time.Since(startTime).Seconds()),
			"workspace_count": wsCount,
		}
		render.JSON(w, r, status, http.StatusOK)
	}
}

// newSPAHandler serves the embedded React SPA.
func newSPAHandler(uiFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to serve the file directly
		data, err := fs.ReadFile(uiFS, path)
		if err == nil {
			switch {
			case strings.HasSuffix(path, ".js"):
				w.Header().Set("Content-Type", "application/javascript")
			case strings.HasSuffix(path, ".css"):
				w.Header().Set("Content-Type", "text/css")
			case strings.HasSuffix(path, ".svg"):
				w.Header().Set("Content-Type", "image/svg+xml")
			case strings.HasSuffix(path, ".html"):
				w.Header().Set("Content-Type", "text/html")
			case strings.HasSuffix(path, ".json"):
				w.Header().Set("Content-Type", "application/json")
			case strings.HasSuffix(path, ".png"):
				w.Header().Set("Content-Type", "image/png")
			case strings.HasSuffix(path, ".ico"):
				w.Header().Set("Content-Type", "image/x-icon")
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			if _, err := w.Write(data); err != nil {
				slog.Error("failed to write static file response", "path", path, "error", err)
			}
			return
		}

		// SPA fallback: serve index.html for client-side routing
		index, err := fs.ReadFile(uiFS, "index.html")
		if err != nil {
			render.Error(w, r, "UI not available", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(index)))
		if _, err := w.Write(index); err != nil {
			slog.Error("failed to write index.html response", "error", err)
		}
	})
}
