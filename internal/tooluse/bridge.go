package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/pkg/types"
)

// Bridge connects the CommHub message routing layer to the tool-use execution
// loop. When an agent with a configured LLM provider receives a message,
// CommHub calls bridge.ProcessMessage to run a tool-augmented completion.
//
// The bridge does NOT import internal/commhub — the caller (app.go wiring
// layer) is responsible for plumbing CommHub events into the bridge.
type Bridge struct {
	resolver *Resolver
	dispatch DispatchFunc
	logger   *slog.Logger
}

// NewBridge creates a tool-use bridge.
//
// Parameters:
//   - resolver: the ABAC-filtered tool surface resolver
//   - dispatch: the MCP tool dispatch function (typically ToolRegistry.Dispatch)
//   - logger: structured logger for diagnostic output
func NewBridge(resolver *Resolver, dispatch DispatchFunc, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		resolver: resolver,
		dispatch: dispatch,
		logger:   logger,
	}
}

// ProcessMessageConfig holds the parameters for a single tool-augmented
// completion request originating from the CommHub message routing layer.
type ProcessMessageConfig struct {
	// Provider connection details.
	ProviderKind string
	BaseURL      string
	APIKey       string
	Model        string

	// Access control.
	ClearanceLevel   int
	DelegationScopes []string
	AllowedActions   []string // Role-scoped action filter (empty = all actions allowed).

	// Persona identity for event emission.
	PersonaID string

	// AgentName is the display name used as the event scope so the frontend
	// can filter events by agent name. Falls back to PersonaID if empty.
	AgentName string

	// Optional system prompt prepended to the conversation.
	SystemPrompt string

	// UserMessage is the incoming message content.
	UserMessage string

	// History is the conversation history (oldest first) to provide context.
	// These are prepended before the current user message.
	History []provider.ChatMessage

	// MaxIterations overrides the default max tool-use iterations.
	// 0 uses the default (100).
	MaxIterations int

	// MaxTotalToolCalls is the absolute cap on individual tool call dispatches
	// across all iterations. Prevents runaway loops where each iteration
	// dispatches multiple calls. 0 uses the default (50), -1 disables.
	MaxTotalToolCalls int

	// MaxContextMessages caps conversation history in the tool-use loop.
	// Older messages are dropped to prevent unbounded context growth.
	// 0 uses the default (40).
	MaxContextMessages int

	// AutoContinue, when true, resets the iteration counter at MaxIterations
	// instead of stopping. The loop runs until the LLM finishes or context expires.
	AutoContinue bool

	// Bus is the optional EventBus for publishing tool-use lifecycle events.
	// If nil, no events are emitted.
	Bus *nervous.EventBus

	// Recorder is an optional callback for recording tool call metrics to
	// the telemetry session tracker. If nil, no metrics are recorded.
	Recorder ToolCallRecorder
}

// ProcessMessage runs a tool-augmented LLM completion for an incoming message.
// It creates the appropriate provider adapter, builds the conversation messages,
// and executes the tool-use loop.
func (b *Bridge) ProcessMessage(ctx context.Context, cfg ProcessMessageConfig) (*ExecuteResult, error) {
	// Create the provider-specific adapter.
	adapter, err := NewToolAdapter(cfg.ProviderKind)
	if err != nil {
		return nil, fmt.Errorf("tooluse.Bridge.ProcessMessage: %w", err)
	}

	// Build the emitter if an EventBus is available.
	// Use AgentName as the event scope so the frontend can filter by display name.
	var emitter EventEmitter
	if cfg.Bus != nil {
		scope := cfg.AgentName
		if scope == "" {
			scope = cfg.PersonaID
		}
		emitter = NewEventEmitter(cfg.Bus, scope)
	}

	// Create the executor.
	exec := NewExecutor(
		ExecutorConfig{
			MaxIterations:      cfg.MaxIterations,
			MaxTotalToolCalls:  cfg.MaxTotalToolCalls,
			MaxContextMessages: cfg.MaxContextMessages,
			AutoContinue:       cfg.AutoContinue,
			PersonaID:          cfg.PersonaID,
			ClearanceLevel:     cfg.ClearanceLevel,
			AgentName:          cfg.AgentName,
			Dispatch:           b.dispatch,
			Emitter:            emitter,
			Recorder:           cfg.Recorder,
		},
		adapter,
		b.resolver,
		b.logger,
	)

	// Build conversation messages.
	var messages []provider.ChatMessage
	if cfg.SystemPrompt != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: cfg.SystemPrompt,
		})
	}
	messages = append(messages, cfg.History...)
	messages = append(messages, provider.ChatMessage{
		Role:    "user",
		Content: cfg.UserMessage,
	})

	req := &provider.CompletionRequest{
		Kind:      cfg.ProviderKind,
		BaseURL:   cfg.BaseURL,
		APIKey:    cfg.APIKey,
		Model:     cfg.Model,
		Messages:  messages,
		AgentName: cfg.AgentName,
	}

	return exec.Execute(ctx, provider.ChatCompletion, req, cfg.ClearanceLevel, cfg.DelegationScopes, cfg.AllowedActions...)
}

// ProcessMessageWithCompleteFn is like ProcessMessage but accepts a custom
// CompletionFunc. This is the primary entry point for testing and for callers
// that need to intercept or mock the LLM completion call.
func (b *Bridge) ProcessMessageWithCompleteFn(
	ctx context.Context,
	cfg ProcessMessageConfig,
	completeFn CompletionFunc,
) (*ExecuteResult, error) {
	adapter, err := NewToolAdapter(cfg.ProviderKind)
	if err != nil {
		return nil, fmt.Errorf("tooluse.Bridge.ProcessMessageWithCompleteFn: %w", err)
	}

	var emitter EventEmitter
	if cfg.Bus != nil {
		scope := cfg.AgentName
		if scope == "" {
			scope = cfg.PersonaID
		}
		emitter = NewEventEmitter(cfg.Bus, scope)
	}

	exec := NewExecutor(
		ExecutorConfig{
			MaxIterations:      cfg.MaxIterations,
			MaxTotalToolCalls:  cfg.MaxTotalToolCalls,
			MaxContextMessages: cfg.MaxContextMessages,
			AutoContinue:       cfg.AutoContinue,
			PersonaID:          cfg.PersonaID,
			ClearanceLevel:     cfg.ClearanceLevel,
			AgentName:          cfg.AgentName,
			Dispatch:           b.dispatch,
			Emitter:            emitter,
			Recorder:           cfg.Recorder,
		},
		adapter,
		b.resolver,
		b.logger,
	)

	var messages []provider.ChatMessage
	if cfg.SystemPrompt != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: cfg.SystemPrompt,
		})
	}
	messages = append(messages, cfg.History...)
	messages = append(messages, provider.ChatMessage{
		Role:    "user",
		Content: cfg.UserMessage,
	})

	req := &provider.CompletionRequest{
		Kind:      cfg.ProviderKind,
		BaseURL:   cfg.BaseURL,
		APIKey:    cfg.APIKey,
		Model:     cfg.Model,
		Messages:  messages,
		AgentName: cfg.AgentName,
	}

	return exec.Execute(ctx, completeFn, req, cfg.ClearanceLevel, cfg.DelegationScopes, cfg.AllowedActions...)
}

// -- Delegation scope extraction ---------------------------------------------

// ResolveDelegationScopes extracts tool-related scopes from a list of active
// delegations. Only GrantScopeAccess delegations contribute scopes, and only
// active (non-revoked) delegations are considered. Expired delegations are
// filtered by checking ExpiresAt against the current time.
func ResolveDelegationScopes(delegations []types.Delegation) []string {
	var scopes []string
	now := time.Now().UTC()

	for _, d := range delegations {
		if !d.IsActive() {
			continue
		}
		// Skip expired.
		if d.ExpiresAt != "" {
			exp, err := time.Parse(time.RFC3339, d.ExpiresAt)
			if err == nil && now.After(exp) {
				continue
			}
		}
		if d.GrantType != types.GrantScopeAccess {
			continue
		}
		scopes = append(scopes, d.Scopes...)
	}

	return scopes
}

// -- Event emitter factory ---------------------------------------------------

// NewEventEmitter creates an EventEmitter that publishes tool-use lifecycle
// events on the given EventBus with source="tooluse" and scope=personaID.
func NewEventEmitter(bus *nervous.EventBus, personaID string) EventEmitter {
	return func(eventType types.EventType, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("tooluse: failed to marshal event payload, skipping publish",
				"event_type", eventType,
				"persona_id", personaID,
				"error", err,
			)
			return
		}
		bus.Publish(types.NervousEvent{
			Type:      eventType,
			Source:    "tooluse",
			Scope:     personaID,
			Payload:   data,
			Timestamp: time.Now().UTC(),
		})
	}
}
