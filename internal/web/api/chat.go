package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/commhub"
	hctx "github.com/hyperax/hyperax/internal/context"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/role"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/telemetry"
	"github.com/hyperax/hyperax/internal/tooluse"
	"github.com/hyperax/hyperax/pkg/types"
)

// chatCompletionTimeout is the maximum duration allowed for a single LLM completion
// goroutine, including network I/O. This is independent of the HTTP request context
// because completions run asynchronously after the user receives the "delivered" response.
// Set high to support auto-continue tool-use loops that may run many iterations.
const chatCompletionTimeout = 15 * time.Minute

// sessionHistoryLimit is a high upper bound for session-scoped history retrieval.
// With LLM-based compaction handling overflow, this is deliberately generous.
const sessionHistoryLimit = 500

// schedulerHistoryLimit caps history for system-originated prompts (scheduler,
// cron, internal). These are short action prompts ("go check your tasks") that
// don't need 500 messages of prior conversation context.
const schedulerHistoryLimit = 10

// ChatAPI handles REST endpoints for the chat messaging interface.
// It routes user messages through CommHub and triggers asynchronous LLM
// completions when the target agent has a configured provider and model.
type ChatAPI struct {
	hub              *commhub.CommHub
	commLog          *commhub.CommLogger
	store            *storage.Store
	bus              *nervous.EventBus
	bridge           *tooluse.Bridge
	templateRegistry *role.RoleTemplateRegistry
	sessionTracker   *telemetry.SessionTracker
	logger           *slog.Logger

	// inflightMu guards the inflight map of active LLM completions.
	inflightMu sync.Mutex
	// inflight maps agent names to cancel functions for in-progress generations.
	// When a user requests cancellation via /chat/stop, the corresponding
	// cancel func is called to abort the LLM HTTP request mid-flight.
	inflight map[string]context.CancelFunc
}

// NewChatAPI creates a ChatAPI with all required dependencies.
// The store provides access to personas, providers, and secrets for LLM completion.
// The bus is used to publish completion lifecycle events on the Nervous System.
func NewChatAPI(hub *commhub.CommHub, commLog *commhub.CommLogger, store *storage.Store, bus *nervous.EventBus, bridge *tooluse.Bridge, templateRegistry *role.RoleTemplateRegistry, logger *slog.Logger) *ChatAPI {
	return &ChatAPI{
		hub:              hub,
		commLog:          commLog,
		store:            store,
		bus:              bus,
		bridge:           bridge,
		templateRegistry: templateRegistry,
		logger:           logger,
		inflight:         make(map[string]context.CancelFunc),
	}
}

// SetSessionTracker injects the telemetry session tracker so chat completions
// record session start/end and tool call metrics.
func (a *ChatAPI) SetSessionTracker(tracker *telemetry.SessionTracker) {
	a.sessionTracker = tracker
}

// Routes returns the chi router for chat endpoints.
func (a *ChatAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/history", a.getHistory) // ?agent_id=X&peer_id=Y&limit=50
	r.Post("/send", a.sendMessage)  // {from, to, content}
	r.Post("/stop", a.stopGeneration) // {agent} — cancel in-flight LLM generation
	return r
}

// stopGeneration cancels an in-progress LLM generation for the specified agent.
// The context cancellation propagates through to the underlying HTTP client making
// the LLM API call, causing the request to abort immediately.
// This is invoked via POST /chat/stop with a JSON body containing {"agent": "name"}.
func (a *ChatAPI) stopGeneration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
	}
	if err := decodeBody(r, &body); err != nil || body.Agent == "" {
		respondError(w, r, http.StatusBadRequest, "agent name is required")
		return
	}

	a.inflightMu.Lock()
	cancel, ok := a.inflight[body.Agent]
	a.inflightMu.Unlock()

	if !ok {
		respondJSON(w, r, http.StatusOK, map[string]string{"status": "no_active_generation"})
		return
	}

	cancel()
	a.logger.Info("chat completion cancelled by user", "agent", body.Agent)

	// Publish cancellation event so the UI and telemetry can react.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChatCompletionError,
			"chat",
			body.Agent,
			map[string]string{
				"agent":  body.Agent,
				"error":  "cancelled by user",
				"reason": "user_stop",
			},
		))
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (a *ChatAPI) getHistory(w http.ResponseWriter, r *http.Request) {
	agentID := queryStr(r, "agent_id", "")
	peerID := queryStr(r, "peer_id", "")
	sessionID := queryStr(r, "session_id", "")
	limit := queryInt(r, "limit", 50)

	if agentID == "" {
		respondError(w, r, http.StatusBadRequest, "agent_id is required")
		return
	}

	if a.commLog == nil {
		respondJSON(w, r, http.StatusOK, []any{})
		return
	}

	ctx := r.Context()
	var entries []*types.CommLogEntry
	var err error

	if sessionID != "" {
		// Session-scoped: only messages belonging to this session.
		entries, err = a.commLog.GetLogBySession(ctx, sessionID, limit)
	} else if peerID != "" {
		entries, err = a.commLog.GetLogBetween(ctx, agentID, peerID, limit)
	} else {
		entries, err = a.commLog.GetLog(ctx, agentID, limit)
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, fmt.Sprintf("get comm log: %v", err))
		return
	}

	if entries == nil {
		entries = []*types.CommLogEntry{}
	}

	respondJSON(w, r, http.StatusOK, entries)
}

func (a *ChatAPI) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From        string `json:"from"`
		To          string `json:"to"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Trust       string `json:"trust"`
	}
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.From == "" || body.To == "" || body.Content == "" {
		respondError(w, r, http.StatusBadRequest, "from, to, and content are required")
		return
	}
	if body.ContentType == "" {
		body.ContentType = "text"
	}

	trust := types.TrustInternal
	if body.Trust != "" {
		trust = types.ParseTrustLevel(body.Trust)
	}

	env := &types.MessageEnvelope{
		ID:          fmt.Sprintf("chat-%s-%s-%d", body.From, body.To, time.Now().UnixMilli()),
		From:        body.From,
		To:          body.To,
		Trust:       trust,
		ContentType: body.ContentType,
		Content:     body.Content,
	}

	ctx := context.Background()
	if err := a.hub.Send(ctx, env); err != nil {
		respondError(w, r, http.StatusInternalServerError, fmt.Sprintf("send failed: %v", err))
		return
	}

	// Ensure an active session exists for this conversation pair.
	sessionID := ""
	if a.store != nil && a.store.Sessions != nil {
		session, sessErr := a.store.Sessions.GetActiveSession(ctx, body.To, body.From)
		if sessErr == nil && session != nil {
			sessionID = session.ID
		} else {
			// Auto-create a session on first message if none exists.
			newID, createErr := a.store.Sessions.CreateSession(ctx, body.To, body.From)
			if createErr == nil {
				sessionID = newID
			}
		}
	}

	// Persist to comm_log for chat history (with session ID if available).
	if a.commLog != nil {
		if sessionID != "" {
			if err := a.commLog.LogWithSession(ctx, env, "sent", sessionID); err != nil {
				a.logger.Error("failed to persist user message to chat history", "session", sessionID, "error", err)
			}
		} else {
			if err := a.commLog.Log(ctx, env, "sent"); err != nil {
				a.logger.Error("failed to persist user message to chat history", "from", env.From, "to", env.To, "error", err)
			}
		}
	}

	// Trigger async LLM completion — the user sees "delivered" immediately
	// while the backend calls the LLM provider in the background.
	go func() {
		if err := a.generateResponse(env.To, env.From, env.Content, sessionID); err != nil {
			a.logger.Error("async chat completion failed", "agent", env.To, "from", env.From, "error", err)
		}
	}()

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "delivered",
		"from":   body.From,
		"to":     body.To,
	})
}

// TriggerCompletion kicks off async LLM completion for an agent that received
// a message. This is used by CommHubHandler to bridge MCP send_message with the
// LLM completion loop, ensuring agent-to-agent messages trigger the recipient's
// response generation. The completion runs in a separate goroutine.
func (a *ChatAPI) TriggerCompletion(agentName, senderID, content, sessionID string) {
	go func() {
		if err := a.generateResponse(agentName, senderID, content, sessionID); err != nil {
			a.logger.Error("async triggered completion failed", "agent", agentName, "from", senderID, "error", err)
		}
	}()
}

// GenerateResponseSync performs synchronous LLM completion for an agent.
// Unlike TriggerCompletion, this blocks until the completion finishes.
// Returns an error if the completion failed, allowing callers (e.g. the scheduler)
// to set the agent to error status.
// Used by the Agent Scheduler's drain loop to process queue items sequentially.
func (a *ChatAPI) GenerateResponseSync(agentName, senderID, content, sessionID string) error {
	return a.generateResponse(agentName, senderID, content, sessionID)
}

// generateResponse performs asynchronous LLM chat completion for a role.
// It resolves the persona's provider configuration, builds conversation context
// from recent history, calls the LLM, and delivers the response back through CommHub.
//
// This method runs in a goroutine with its own context (not tied to the HTTP request).
// Errors are logged and published as Nervous System events but never surface to the user's
// HTTP response (which has already been sent).
func (a *ChatAPI) generateResponse(agentName, userID, latestMessage, sessionID string) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("chat completion: panic in generateResponse", "agent", agentName, "panic", r)
			retErr = fmt.Errorf("api.ChatAPI.generateResponse: panic: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), chatCompletionTimeout)

	// Register the cancel func so /chat/stop can abort this generation mid-flight.
	a.inflightMu.Lock()
	a.inflight[agentName] = cancel
	a.inflightMu.Unlock()
	defer func() {
		a.inflightMu.Lock()
		delete(a.inflight, agentName)
		a.inflightMu.Unlock()
		cancel()
	}()

	// 1. Look up the agent by name (chat uses agent names, not UUIDs).
	if a.store == nil || a.store.Agents == nil {
		return nil // Store not configured — skip silently.
	}

	agent, err := a.store.Agents.GetByName(ctx, agentName)
	if err != nil {
		// Not every chat target is a configured agent — this is expected
		// for user-to-user messages. Log at debug level.
		a.logger.Debug("chat completion: agent not found, skipping",
			"agent", agentName,
			"error", err,
		)
		return nil
	}

	// 2. Validate the agent has a provider and model configured.
	if agent.ProviderID == "" || agent.DefaultModel == "" {
		a.logger.Debug("chat completion: agent has no provider/model configured",
			"agent", agentName,
			"provider_id", agent.ProviderID,
			"model", agent.DefaultModel,
		)
		return nil
	}

	// 3. Look up the provider.
	if a.store.Providers == nil {
		return nil
	}

	prov, err := a.store.Providers.Get(ctx, agent.ProviderID)
	if err != nil {
		a.logger.Warn("chat completion: provider lookup failed",
			"agent", agentName,
			"provider_id", agent.ProviderID,
			"error", err,
		)
		a.publishCompletionError(agentName, userID, fmt.Sprintf("provider lookup: %v", err))
		return fmt.Errorf("api.ChatAPI.generateResponse: provider lookup: %w", err)
	}

	if !prov.IsEnabled {
		a.logger.Info("chat completion: provider disabled, attempting delegation",
			"agent", agentName, "provider", prov.Name)

		// Publish provider unavailable event.
		a.publishDelegationEvent(types.EventProviderUnavailable, agentName, map[string]any{
			"agent": agentName, "provider": prov.Name, "sender": userID,
		})

		// Transition agent to suspended (if valid transition).
		agent.Status = string(types.AgentStateSuspended)
		agent.StatusReason = "provider disabled"
		if updateErr := a.store.Agents.Update(ctx, agent.ID, agent); updateErr != nil {
			a.logger.Warn("chat completion: failed to suspend agent",
				"agent", agentName, "error", updateErr)
		}

		// Auto-reply to sender so they know the message was not silently lost.
		autoReply := fmt.Sprintf("%s is currently unavailable (provider disabled). Attempting to delegate to a team member.", agentName)
		a.sendSystemMessage(ctx, agentName, userID, autoReply, sessionID)

		// Attempt delegation to a capable subordinate or parent.
		if err := a.attemptDelegation(ctx, agentName, userID, latestMessage, sessionID); err != nil {
			a.logger.Warn("chat completion: delegation failed",
				"agent", agentName, "error", err)
		}
		return nil
	}

	a.logger.Info("chat completion: provider resolved", "agent", agentName, "provider", prov.Name, "kind", prov.Kind)

	// 3b. Start a telemetry session to track this completion lifecycle.
	telemetrySessionID := ""
	if a.sessionTracker != nil {
		metadata := fmt.Sprintf(`{"provider_id":%q,"model":%q,"user":%q}`,
			agent.ProviderID, agent.DefaultModel, userID)
		sessID, sessErr := a.sessionTracker.StartSession(ctx, agent.ID, metadata)
		if sessErr != nil {
			a.logger.Warn("chat completion: failed to start telemetry session",
				"agent", agentName, "error", sessErr)
		} else {
			telemetrySessionID = sessID
			defer func() {
				if endErr := a.sessionTracker.EndSession(ctx, telemetrySessionID); endErr != nil {
					a.logger.Warn("chat completion: failed to end telemetry session",
						"session", telemetrySessionID, "error", endErr)
				}
			}()
		}
	}

	// 4. Resolve API key from secrets store.
	apiKey := ""
	if prov.SecretKeyRef != "" && a.store.Secrets != nil {
		apiKey, err = a.store.Secrets.Get(ctx, prov.SecretKeyRef, "global")
		if err != nil {
			a.logger.Warn("chat completion: failed to resolve API key",
				"agent", agentName,
				"provider", prov.Name,
				"secret_ref", prov.SecretKeyRef,
				"error", err,
			)
			a.publishCompletionError(agentName, userID, fmt.Sprintf("resolve API key: %v", err))
			return fmt.Errorf("api.ChatAPI.generateResponse: resolve API key: %w", err)
		}
	}

	a.logger.Info("chat completion: API key resolved", "agent", agentName, "has_key", apiKey != "")

	// 5. Build conversation history from comm log with structured prompt.
	messages := a.buildMessagesFromAgent(ctx, agent, agentName, userID, latestMessage, sessionID)

	// 5b. Check for LLM-based auto-compaction at 150k tokens.
	llmCompactor := hctx.NewLLMCompactor(hctx.DefaultLLMCompactorConfig())
	compactMsgs := make([]hctx.CompactMessage, len(messages))
	for i, m := range messages {
		compactMsgs[i] = hctx.CompactMessage{Role: m.Role, Content: m.Content}
	}
	if llmCompactor.NeedsCompaction(compactMsgs) {
		a.logger.Info("chat completion: triggering LLM auto-compaction",
			"agent", agentName,
			"estimated_tokens", hctx.EstimateTokens(compactMsgs),
		)

		// Extract the system prompt for the compaction prompt.
		var compactionSystemPrompt string
		for _, m := range messages {
			if m.Role == "system" {
				compactionSystemPrompt = m.Content
				break
			}
		}

		compResult, compErr := llmCompactor.CompactWithLLM(ctx, hctx.LLMCompactionRequest{
			Messages:     compactMsgs,
			SystemPrompt: compactionSystemPrompt,
			ProviderKind: prov.Kind,
			BaseURL:      prov.BaseURL,
			APIKey:       apiKey,
			Model:        agent.DefaultModel,
		})
		if compErr != nil {
			a.logger.Warn("chat completion: LLM compaction failed, continuing with full history",
				"agent", agentName, "error", compErr,
			)
		} else {
			// Store extracted memory if available.
			if compResult.ExtractedMemory != nil && a.store != nil && a.store.Memory != nil {
				mem := &types.Memory{
					Scope:     types.MemoryScopePersona,
					Type:      types.MemoryTypeEpisodic,
					Content:   compResult.ExtractedMemory.Details,
					PersonaID: agent.ID,
					Metadata: map[string]any{
						"source":      "auto_compaction",
						"description": compResult.ExtractedMemory.Description,
						"keywords":    compResult.ExtractedMemory.Keywords,
						"session_id":  sessionID,
					},
				}
				if _, storeErr := a.store.Memory.Store(ctx, mem); storeErr != nil {
					a.logger.Warn("chat completion: failed to store compaction memory",
						"agent", agentName, "error", storeErr,
					)
				}
			}

			// Store session summary for future prompt construction.
			if compResult.Summary != "" && sessionID != "" && a.store != nil && a.store.Sessions != nil {
				if err := a.store.Sessions.SetSessionSummary(ctx, sessionID, compResult.Summary); err != nil {
					a.logger.Error("failed to persist session summary", "session", sessionID, "error", err)
				}
			}

			// Rebuild messages from compacted result.
			messages = make([]provider.ChatMessage, len(compResult.RecentMessages))
			for i, m := range compResult.RecentMessages {
				messages[i] = provider.ChatMessage{Role: m.Role, Content: m.Content}
			}

			a.logger.Info("chat completion: compaction complete",
				"agent", agentName,
				"tokens_before", compResult.TokensBefore,
				"tokens_after", compResult.TokensAfter,
			)
		}
	}

	// 6. Publish completion start event.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChatCompletionStart,
			"chat",
			agentName,
			map[string]string{
				"agent":    agentName,
				"user":     userID,
				"provider": prov.Name,
				"model":    agent.DefaultModel,
			},
		))
	}

	// 7. Resolve delegation scopes for this agent's tool access.
	var delegScopes []string
	if a.store.Delegations != nil {
		delegations, dlgErr := a.store.Delegations.ListByGrantee(ctx, agent.ID)
		if dlgErr != nil {
			a.logger.Warn("chat completion: failed to resolve delegations",
				"agent", agentName, "error", dlgErr)
		} else {
			// Convert []*types.Delegation to []types.Delegation for ResolveDelegationScopes.
			plain := make([]types.Delegation, len(delegations))
			for i, d := range delegations {
				plain[i] = *d
			}
			delegScopes = tooluse.ResolveDelegationScopes(plain)
		}
	}

	// 8. Separate system prompt and history for the bridge.
	var systemPrompt string
	var history []provider.ChatMessage
	for _, msg := range messages {
		if msg.Role == "system" {
			if systemPrompt != "" {
				systemPrompt += "\n\n"
			}
			systemPrompt += msg.Content
		} else {
			history = append(history, msg)
		}
	}

	// Dual-model architecture: select model based on completion path.
	// - workModel (default_model): used for tool-use bridge execution (real work).
	// - chatModel (chat_model if set, otherwise default_model): used for direct
	//   conversational responses without tool augmentation.
	workModel := agent.DefaultModel
	chatModel := agent.DefaultModel
	if agent.ChatModel != "" {
		chatModel = agent.ChatModel
	}

	a.logger.Info("chat completion: calling LLM",
		"agent", agentName,
		"work_model", workModel,
		"chat_model", chatModel,
		"message_count", len(messages),
		"has_bridge", a.bridge != nil,
	)

	// 9. Call the LLM via the tool-use bridge (provides tool-augmented completion).
	// If no bridge is configured, fall back to a direct completion call using the chat model.
	var respContent string
	var respModel string
	var usage *provider.UsageInfo
	var iterations int

	// 10. Resolve role-scoped allowed actions from the linked role template.
	// When an agent is linked to a template with AllowedActions, only tools
	// matching those action types are sent to the LLM.
	var allowedActions []string
	if agent.RoleTemplateID != "" && a.templateRegistry != nil {
		if tmpl := a.templateRegistry.Get(agent.RoleTemplateID); tmpl != nil {
			allowedActions = tmpl.AllowedActions
		}
	}

	if a.bridge != nil {
		// Resolve tool-use settings from config store.
		maxIter := tooluse.DefaultMaxIterations
		autoContinue := false
		if a.store != nil && a.store.Config != nil {
			if v, err := a.store.Config.GetValue(ctx, "tooluse.max_iterations", types.ConfigScope{Type: "global"}); err == nil {
				if parsed, parseErr := strconv.Atoi(v); parseErr == nil && parsed > 0 {
					maxIter = parsed
				}
			}
			if v, err := a.store.Config.GetValue(ctx, "tooluse.auto_continue", types.ConfigScope{Type: "global"}); err == nil {
				autoContinue = v == "true"
			}
		}

		// Build the tool call recorder closure for telemetry session tracking.
		var recorder tooluse.ToolCallRecorder
		if a.sessionTracker != nil && telemetrySessionID != "" {
			tracker := a.sessionTracker
			sessID := telemetrySessionID
			provID := agent.ProviderID
			recorder = func(toolName string, duration time.Duration, success bool, errMsg string, inputSize, outputSize int64) {
				metric := &types.ToolCallMetric{
					ToolName:   toolName,
					ProviderID: provID,
					StartedAt:  time.Now().Add(-duration),
					Duration:   duration,
					Success:    success,
					ErrorMsg:   errMsg,
					InputSize:  inputSize,
					OutputSize: outputSize,
				}
				if recErr := tracker.RecordToolCall(ctx, sessID, metric); recErr != nil {
					a.logger.Warn("chat completion: failed to record tool call metric",
						"session", sessID, "tool", toolName, "error", recErr)
				}
			}
		}

		// Bridge path: use workModel (default_model) for tool-use execution.
		cfg := tooluse.ProcessMessageConfig{
			ProviderKind:     prov.Kind,
			BaseURL:          prov.BaseURL,
			APIKey:           apiKey,
			Model:            workModel,
			ClearanceLevel:   agent.ClearanceLevel,
			DelegationScopes: delegScopes,
			AllowedActions:   allowedActions,
			PersonaID:        agent.ID,
			AgentName:        agentName,
			SystemPrompt:     systemPrompt,
			UserMessage:      latestMessage,
			History:          history,
			MaxIterations:    maxIter,
			AutoContinue:     autoContinue,
			Bus:              a.bus,
			Recorder:         recorder,
		}
		result, bridgeErr := a.bridge.ProcessMessage(ctx, cfg)
		if bridgeErr != nil {
			a.logger.Error("chat completion: tool-use bridge failed",
				"agent", agentName,
				"provider", prov.Name,
				"model", workModel,
				"error", bridgeErr,
			)
			a.publishCompletionError(agentName, userID, fmt.Sprintf("tool-use bridge: %v", bridgeErr))
			return fmt.Errorf("api.ChatAPI.generateResponse: tool-use bridge: %w", bridgeErr)
		}
		respContent = result.Response.Content
		respModel = result.Response.Model
		usage = &result.TotalUsage
		iterations = result.Iterations
	} else {
		// Direct completion path: use chatModel (cheap model for conversation).
		resp, compErr := provider.ChatCompletion(ctx, &provider.CompletionRequest{
			Kind:      prov.Kind,
			BaseURL:   prov.BaseURL,
			APIKey:    apiKey,
			Model:     chatModel,
			Messages:  messages,
			AgentName: agentName,
		})
		if compErr != nil {
			a.logger.Error("chat completion: LLM call failed",
				"agent", agentName,
				"provider", prov.Name,
				"model", chatModel,
				"error", compErr,
			)
			a.publishCompletionError(agentName, userID, fmt.Sprintf("LLM call: %v", compErr))
			return fmt.Errorf("api.ChatAPI.generateResponse: LLM call: %w", compErr)
		}
		respContent = resp.Content
		respModel = resp.Model
		usage = resp.Usage
	}

	// 9b. Record provider cost against the provider budget scope.
	// This enables per-provider budget tracking and threshold enforcement.
	if usage != nil && a.store != nil && a.store.Budgets != nil {
		costUSD := provider.EstimateProviderCost(
			prov.Kind, respModel,
			int64(usage.PromptTokens), int64(usage.CompletionTokens),
		)
		if costUSD > 0 {
			budgetScope := fmt.Sprintf("provider:%s", agent.ProviderID)
			if budgetErr := a.store.Budgets.RecordEnergyCost(ctx, budgetScope, costUSD, agent.ProviderID, respModel); budgetErr != nil {
				a.logger.Warn("chat completion: failed to record provider cost",
					"agent", agentName,
					"provider", prov.Name,
					"scope", budgetScope,
					"cost_usd", costUSD,
					"error", budgetErr,
				)
			} else {
				a.logger.Debug("chat completion: recorded provider cost",
					"agent", agentName,
					"scope", budgetScope,
					"cost_usd", costUSD,
					"tokens_in", usage.PromptTokens,
					"tokens_out", usage.CompletionTokens,
				)
			}
		}
	}

	// 10. Deliver the assistant's response back through CommHub.
	responseEnv := &types.MessageEnvelope{
		ID:          fmt.Sprintf("llm-%s-%s-%d", agentName, userID, time.Now().UnixMilli()),
		From:        agentName,
		To:          userID,
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     respContent,
	}

	if sendErr := a.hub.Send(ctx, responseEnv); sendErr != nil {
		a.logger.Error("chat completion: failed to deliver response",
			"agent", agentName,
			"user", userID,
			"error", sendErr,
		)
		return fmt.Errorf("api.ChatAPI.generateResponse: deliver response: %w", sendErr)
	}

	// 11. Log the response to comm_log (with session ID if available).
	if a.commLog != nil {
		if sessionID != "" {
			if err := a.commLog.LogWithSession(ctx, responseEnv, "sent", sessionID); err != nil {
				a.logger.Error("failed to persist agent response to chat history", "agent", agentName, "session", sessionID, "error", err)
			}
		} else {
			if err := a.commLog.Log(ctx, responseEnv, "sent"); err != nil {
				a.logger.Error("failed to persist agent response to chat history", "agent", agentName, "user", userID, "error", err)
			}
		}
	}

	// 12. Publish completion done event with usage metrics.
	if a.bus != nil {
		payload := map[string]any{
			"agent":    agentName,
			"user":     userID,
			"provider": prov.Name,
			"model":    respModel,
		}
		if usage != nil {
			payload["prompt_tokens"] = usage.PromptTokens
			payload["completion_tokens"] = usage.CompletionTokens
			payload["total_tokens"] = usage.TotalTokens
			if usage.CacheCreationTokens > 0 {
				payload["cache_creation_tokens"] = usage.CacheCreationTokens
			}
			if usage.CacheReadTokens > 0 {
				payload["cache_read_tokens"] = usage.CacheReadTokens
			}
		}
		if iterations > 0 {
			payload["tool_use_iterations"] = iterations
		}
		a.bus.Publish(nervous.NewEvent(
			types.EventChatCompletionDone,
			"chat",
			agentName,
			payload,
		))
	}

	a.logger.Info("chat completion: response delivered",
		"agent", agentName,
		"user", userID,
		"provider", prov.Name,
		"model", respModel,
		"iterations", iterations,
	)

	return nil
}

// buildMessagesFromAgent constructs the LLM message array using the structured
// XML-tagged prompt template, session-scoped history, and the latest user message.
// Messages are returned in chronological order with proper role assignment.
func (a *ChatAPI) buildMessagesFromAgent(ctx context.Context, agent *repo.Agent, agentName, userID, latestMessage, sessionID string) []provider.ChatMessage {
	var messages []provider.ChatMessage

	// Resolve the role template for structured prompt construction.
	var tmpl *role.RoleTemplate
	if agent.RoleTemplateID != "" && a.templateRegistry != nil {
		tmpl = a.templateRegistry.Get(agent.RoleTemplateID)
	}

	// Look up session summary if a session is active (from prior compaction).
	sessionSummary := ""
	if sessionID != "" && a.store != nil && a.store.Sessions != nil {
		session, err := a.store.Sessions.GetActiveSession(ctx, agentName, userID)
		if err == nil && session != nil {
			sessionSummary = session.Summary
		}
	}

	// Build the structured system prompt via the prompt builder.
	structuredPrompt := hctx.BuildStructuredSystemPrompt(hctx.PromptBuilderConfig{
		AgentName:       agent.Name,
		Personality:     agent.Personality,
		SystemPrompt:    agent.SystemPrompt,
		RoleTemplateID:  agent.RoleTemplateID,
		Template:        tmpl,
		EngagementRules: agent.EngagementRules,
		SessionSummary:  sessionSummary,
	})

	if structuredPrompt != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: structuredPrompt,
		})
	}

	// Retrieve conversation history scoped to the active session.
	// System-originated prompts (scheduler, cron) use a smaller history window
	// since they're short action prompts that don't need full conversation context.
	historyLimit := sessionHistoryLimit
	if strings.HasPrefix(userID, "system:") {
		historyLimit = schedulerHistoryLimit
	}

	latestInHistory := false
	if a.commLog != nil {
		var entries []*types.CommLogEntry
		var err error

		if sessionID != "" {
			entries, err = a.commLog.GetLogBySession(ctx, sessionID, historyLimit)
		} else {
			entries, err = a.commLog.GetLogBetween(ctx, userID, agentName, historyLimit)
		}

		if err != nil {
			a.logger.Warn("chat completion: failed to retrieve history",
				"agent", agentName,
				"user", userID,
				"session_id", sessionID,
				"error", err,
			)
		} else if len(entries) > 0 {
			// Sort entries by creation time (oldest first) for chronological order.
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].CreatedAt.Before(entries[j].CreatedAt)
			})

			for _, entry := range entries {
				entryRole := "user"
				if entry.FromAgent == agentName {
					entryRole = "assistant"
				}
				messages = append(messages, provider.ChatMessage{
					Role:    entryRole,
					Content: entry.Content,
				})
			}

			// Check if the last entry is the user's latest message.
			last := entries[len(entries)-1]
			if last.FromAgent == userID && last.Content == latestMessage {
				latestInHistory = true
			}
		}
	}

	// Only append the latest message if it was not already captured in history.
	// This prevents duplicate messages when the comm log write completes before
	// the history query executes.
	if !latestInHistory {
		messages = append(messages, provider.ChatMessage{
			Role:    "user",
			Content: latestMessage,
		})
	}

	// Filter empty messages and deduplicate consecutive identical messages.
	// Empty messages waste context tokens. Consecutive duplicates occur when the
	// scheduler enqueues a message that's already in the comm log history.
	var cleaned []provider.ChatMessage
	for _, m := range messages {
		// Skip empty messages (no content in any form).
		if m.Content == "" && m.RawContent == nil && m.RawMessage == nil {
			continue
		}
		// Skip consecutive duplicates (same role + identical plain-text content).
		if len(cleaned) > 0 {
			prev := cleaned[len(cleaned)-1]
			if prev.Role == m.Role && m.Content != "" && prev.Content == m.Content &&
				prev.RawContent == nil && m.RawContent == nil &&
				prev.RawMessage == nil && m.RawMessage == nil {
				continue
			}
		}
		cleaned = append(cleaned, m)
	}

	return cleaned
}

// publishCompletionError publishes a chat.completion.error event on the Nervous System.
// This allows telemetry, alerts, and the UI to react to LLM failures.
func (a *ChatAPI) publishCompletionError(agentName, userID, errMsg string) {
	if a.bus == nil {
		return
	}
	a.bus.Publish(nervous.NewEvent(
		types.EventChatCompletionError,
		"chat",
		agentName,
		map[string]string{
			"agent": agentName,
			"user":  userID,
			"error": errMsg,
		},
	))
}

// maxDelegationDepth limits how deep delegation can recurse to prevent loops.
const maxDelegationDepth = 3

// publishDelegationEvent publishes a delegation lifecycle event on the Nervous System.
func (a *ChatAPI) publishDelegationEvent(eventType types.EventType, agentName string, payload map[string]any) {
	if a.bus == nil {
		return
	}
	a.bus.Publish(nervous.NewEvent(eventType, "chat", agentName, payload))
}

// sendSystemMessage delivers an informational system message to the sender via
// CommHub and persists it to the comm log so it appears in chat history.
func (a *ChatAPI) sendSystemMessage(ctx context.Context, from, to, content, sessionID string) {
	env := &types.MessageEnvelope{
		ID:          fmt.Sprintf("sys-%s-%s-%d", from, to, time.Now().UnixMilli()),
		From:        from,
		To:          to,
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     content,
	}

	if sendErr := a.hub.Send(ctx, env); sendErr != nil {
		a.logger.Warn("chat completion: failed to send system message",
			"from", from, "to", to, "error", sendErr)
		return
	}

	// Persist to comm log so the message appears in chat history.
	if a.commLog != nil {
		if sessionID != "" {
			if err := a.commLog.LogWithSession(ctx, env, "sent", sessionID); err != nil {
				a.logger.Warn("failed to persist system message to chat history",
					"from", from, "to", to, "session", sessionID, "error", err)
			}
		} else {
			if err := a.commLog.Log(ctx, env, "sent"); err != nil {
				a.logger.Warn("failed to persist system message to chat history",
					"from", from, "to", to, "error", err)
			}
		}
	}
}

// attemptDelegation tries to find a capable subordinate (or parent) to handle
// a request that the disabled agent cannot process. It walks the hierarchy:
// first children, then parent, looking for an agent whose provider is enabled.
// On success it forwards the message and triggers the delegate's LLM completion.
// On terminal failure it notifies the sender that no delegate was found.
func (a *ChatAPI) attemptDelegation(ctx context.Context, disabledAgentName, senderID, content, sessionID string) error {
	return a.attemptDelegationWithDepth(ctx, disabledAgentName, senderID, content, sessionID, 0)
}

func (a *ChatAPI) attemptDelegationWithDepth(ctx context.Context, disabledAgentName, senderID, content, sessionID string, depth int) error {
	if depth >= maxDelegationDepth {
		return a.terminalDelegationFailure(ctx, disabledAgentName, senderID, sessionID)
	}

	if a.store == nil || a.store.CommHub == nil {
		return fmt.Errorf("api.ChatAPI.attemptDelegation: commhub repo not available")
	}

	// 1. Try children of the disabled agent.
	children, childErr := a.store.CommHub.GetChildren(ctx, disabledAgentName)
	if childErr == nil {
		for _, child := range children {
			delegate, delegateErr := a.tryDelegate(ctx, child.ChildAgent, disabledAgentName, senderID, content, sessionID)
			if delegateErr == nil && delegate {
				return nil
			}
		}
	}

	// 2. Try the parent of the disabled agent.
	parentRel, parentErr := a.store.CommHub.GetParent(ctx, disabledAgentName)
	if parentErr == nil && parentRel.ParentAgent != "" {
		delegate, delegateErr := a.tryDelegate(ctx, parentRel.ParentAgent, disabledAgentName, senderID, content, sessionID)
		if delegateErr == nil && delegate {
			return nil
		}
	}

	// 3. Terminal failure — no capable agent found.
	return a.terminalDelegationFailure(ctx, disabledAgentName, senderID, sessionID)
}

// tryDelegate checks whether candidateName has an enabled provider, and if so,
// forwards the delegated message and triggers LLM completion for the candidate.
// Returns (true, nil) on successful delegation.
func (a *ChatAPI) tryDelegate(ctx context.Context, candidateName, disabledAgentName, senderID, content, sessionID string) (bool, error) {
	candidate, err := a.store.Agents.GetByName(ctx, candidateName)
	if err != nil {
		a.logger.Debug("chat completion: delegation candidate lookup failed",
			"candidate", candidateName, "error", err)
		return false, err
	}

	// Skip candidates without a provider or model.
	if candidate.ProviderID == "" || candidate.DefaultModel == "" {
		return false, nil
	}

	// Verify the candidate's provider is enabled BEFORE calling generateResponse
	// to prevent recursive delegation loops.
	if a.store.Providers == nil {
		return false, nil
	}
	prov, provErr := a.store.Providers.Get(ctx, candidate.ProviderID)
	if provErr != nil || !prov.IsEnabled {
		return false, nil
	}

	// Compose the delegation message so the delegate has context about why
	// they are receiving this request.
	delegationMsg := fmt.Sprintf(
		"This was originally assigned to %s who is currently unavailable. "+
			"Please attempt to complete this task. If you cannot handle it, explain why.\n\n"+
			"Original request: %s", disabledAgentName, content)

	// Deliver the delegation message through CommHub.
	delegEnv := &types.MessageEnvelope{
		ID:          fmt.Sprintf("deleg-%s-%s-%d", senderID, candidateName, time.Now().UnixMilli()),
		From:        senderID,
		To:          candidateName,
		Trust:       types.TrustInternal,
		ContentType: "text",
		Content:     delegationMsg,
	}
	if sendErr := a.hub.Send(ctx, delegEnv); sendErr != nil {
		a.logger.Warn("chat completion: failed to send delegation message",
			"candidate", candidateName, "error", sendErr)
		return false, sendErr
	}

	// Persist the delegation message to comm log.
	if a.commLog != nil {
		if sessionID != "" {
			if err := a.commLog.LogWithSession(ctx, delegEnv, "sent", sessionID); err != nil {
				a.logger.Warn("failed to persist delegation message to chat history", "session", sessionID, "error", err)
			}
		} else {
			if err := a.commLog.Log(ctx, delegEnv, "sent"); err != nil {
				a.logger.Warn("failed to persist delegation message to chat history", "candidate", candidateName, "error", err)
			}
		}
	}

	a.publishDelegationEvent(types.EventDelegationAttempted, candidateName, map[string]any{
		"delegate":       candidateName,
		"disabled_agent": disabledAgentName,
		"sender":         senderID,
	})

	a.logger.Info("chat completion: delegating to capable agent",
		"delegate", candidateName, "disabled_agent", disabledAgentName)

	// Trigger the delegate's completion asynchronously.
	go func() {
		if err := a.generateResponse(candidateName, senderID, delegationMsg, sessionID); err != nil {
			a.logger.Error("async delegation completion failed", "delegate", candidateName, "from", senderID, "error", err)
		}
	}()

	return true, nil
}

// terminalDelegationFailure handles the case where no delegate is available.
// It notifies the sender and publishes a delegation_exhausted event.
func (a *ChatAPI) terminalDelegationFailure(ctx context.Context, disabledAgentName, senderID, sessionID string) error {
	a.publishDelegationEvent(types.EventDelegationExhausted, disabledAgentName, map[string]any{
		"agent":  disabledAgentName,
		"sender": senderID,
	})

	failMsg := "Unable to find an available team member to handle this request. " +
		"The message has been queued for when a team member becomes available."
	a.sendSystemMessage(ctx, disabledAgentName, senderID, failMsg, sessionID)

	return fmt.Errorf("api.ChatAPI.attemptDelegation: no capable delegate found for %s", disabledAgentName)
}
