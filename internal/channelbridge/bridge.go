package channelbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// DispatchFunc calls an MCP tool by name with JSON params.
type DispatchFunc func(ctx context.Context, toolName string, params json.RawMessage) (*types.ToolResult, error)

// SecurityReviewFunc synchronously reviews untrusted content.
// Returns the (possibly redacted) content, or an error if rejected.
type SecurityReviewFunc func(ctx context.Context, route *Route, content, sender string) (string, error)

// CompletionFunc triggers a synchronous completion for an agent.
type CompletionFunc func(agentName, senderID, content, sessionID string)

// Route holds the resolved routing config for an external channel.
type Route struct {
	PluginName string `json:"plugin_name"`
	ChannelID  string `json:"channel_id"`
	Agent      string `json:"agent"`
	Trust      string `json:"trust"` // "trusted" or "untrusted"
}

// RouteConfig is the JSON structure stored in plugin config variables.
type RouteConfig struct {
	Agent string `json:"agent"`
	Trust string `json:"trust"`
}

// Bridge routes messages between external communication plugins and internal agents.
// Routing config is read from the ConfigStore under plugin.<name>.channel_routes keys.
type Bridge struct {
	store      *storage.Store
	bus        *nervous.EventBus
	hub        *commhub.CommHub
	commLog    *commhub.CommLogger
	dispatch   DispatchFunc
	logger     *slog.Logger
	reviewFn   SecurityReviewFunc
	completeFn CompletionFunc
	rootAgent  string

	// reviewSem limits concurrent Security Lead completions.
	reviewSem chan struct{}

	mu     sync.RWMutex
	routes map[string]map[string]*RouteConfig // pluginName -> channelID -> config
}

// New creates a Channel Bridge.
func New(
	store *storage.Store,
	bus *nervous.EventBus,
	hub *commhub.CommHub,
	commLog *commhub.CommLogger,
	dispatch DispatchFunc,
	logger *slog.Logger,
) *Bridge {
	return &Bridge{
		store:     store,
		bus:       bus,
		hub:       hub,
		commLog:   commLog,
		dispatch:  dispatch,
		logger:    logger.With("component", "channel-bridge"),
		rootAgent: "Chief of Staff",
		reviewSem: make(chan struct{}, 3), // max 3 concurrent security reviews
		routes:    make(map[string]map[string]*RouteConfig),
	}
}

// SetSecurityReviewFunc sets the function that performs Security Lead review
// of untrusted content.
func (b *Bridge) SetSecurityReviewFunc(fn SecurityReviewFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reviewFn = fn
}

// SetCompletionFunc sets the function that triggers synchronous agent completion.
func (b *Bridge) SetCompletionFunc(fn CompletionFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.completeFn = fn
}

// SetRootAgent overrides the default root agent name for DM routing.
func (b *Bridge) SetRootAgent(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rootAgent = name
}

// Run starts the bridge event loop. It blocks until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) {
	// Load initial routes from plugin config.
	b.loadRoutes(ctx)

	// Subscribe to plugin message events.
	pluginSub := b.bus.Subscribe("channel-bridge-plugins", func(e types.NervousEvent) bool {
		t := string(e.Type)
		return strings.HasPrefix(t, "discord.") ||
			strings.HasPrefix(t, "slack.") ||
			strings.HasPrefix(t, "email.")
	})

	// Subscribe to comm.message events for response mirroring.
	commSub := b.bus.Subscribe("channel-bridge-outbound", func(e types.NervousEvent) bool {
		return e.Type == types.EventCommMessage
	})

	b.logger.Info("channel-bridge subscribed",
		"plugin_sub", "discord.*,slack.*,email.*",
		"comm_sub", "comm.message",
		"routes_loaded", b.routeCount())

	// Periodic route refresh (every 60s) picks up config changes.
	refreshTicker := time.NewTicker(60 * time.Second)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("channel-bridge shutting down")
			return
		case event := <-pluginSub.Ch:
			b.handleInbound(ctx, event)
		case event := <-commSub.Ch:
			b.handleOutbound(ctx, event)
		case <-refreshTicker.C:
			b.loadRoutes(ctx)
		}
	}
}

// loadRoutes reads channel routing config from the ConfigStore.
// Key pattern: plugin.<name>.channel_routes
// Value: JSON map {"<channelID>": {"agent": "...", "trust": "..."}}
func (b *Bridge) loadRoutes(ctx context.Context) {
	if b.store.Config == nil {
		return
	}

	// Known channel plugin prefixes.
	pluginNames := []string{"discord", "slack", "email"}

	newRoutes := make(map[string]map[string]*RouteConfig)
	for _, name := range pluginNames {
		key := "plugin." + name + ".channel_routes"
		val, err := b.store.Config.GetValue(ctx, key, types.ConfigScope{Type: "global"})
		if err != nil || val == "" {
			continue
		}

		var routes map[string]*RouteConfig
		if err := json.Unmarshal([]byte(val), &routes); err != nil {
			b.logger.Warn("channel-bridge: invalid route config",
				"plugin", name, "error", err)
			continue
		}
		newRoutes[name] = routes
	}

	b.mu.Lock()
	b.routes = newRoutes
	b.mu.Unlock()

	b.logger.Debug("channel-bridge routes loaded", "count", b.routeCount())
}

// getRoute looks up a route for a plugin + channel ID.
func (b *Bridge) getRoute(pluginName, channelID string) *Route {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pluginRoutes, ok := b.routes[pluginName]
	if !ok {
		return nil
	}
	rc, ok := pluginRoutes[channelID]
	if !ok {
		return nil
	}
	return &Route{
		PluginName: pluginName,
		ChannelID:  channelID,
		Agent:      rc.Agent,
		Trust:      rc.Trust,
	}
}

// routeCount returns the total number of configured routes.
func (b *Bridge) routeCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	count := 0
	for _, m := range b.routes {
		count += len(m)
	}
	return count
}

// handleInbound processes an inbound message from an external channel plugin.
func (b *Bridge) handleInbound(ctx context.Context, event types.NervousEvent) {
	// Parse event payload.
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		b.logger.Debug("channel-bridge: failed to parse event payload", "error", err)
		return
	}

	// Extract plugin name from the event payload or type prefix.
	pluginName := ""
	if pn, ok := payload["_plugin"].(string); ok {
		pluginName = pn
	} else {
		// Derive from event type: "discord.message" -> "discord"
		parts := strings.SplitN(string(event.Type), ".", 2)
		if len(parts) > 0 {
			pluginName = parts[0]
		}
	}
	if pluginName == "" {
		return
	}

	// Extract channel ID and content.
	channelID := ""
	if cid, ok := payload["channel_id"].(string); ok {
		channelID = cid
	} else if from, ok := payload["from"].(string); ok {
		channelID = from
	}
	if channelID == "" {
		return
	}

	content := ""
	if c, ok := payload["content"].(string); ok {
		content = c
	} else if c, ok := payload["message"].(string); ok {
		content = c
	}
	if content == "" {
		return
	}

	sender := ""
	if s, ok := payload["author"].(string); ok {
		sender = s
	} else if s, ok := payload["sender"].(string); ok {
		sender = s
	}

	// Check if this is a DM — route to root agent with trusted trust.
	isDM := false
	if dm, ok := payload["is_dm"].(bool); ok {
		isDM = dm
	}

	b.bus.Publish(nervous.NewEvent(
		types.EventChannelBridgeReceived,
		pluginName,
		"channel-bridge",
		map[string]string{
			"plugin":     pluginName,
			"channel_id": channelID,
			"sender":     sender,
		},
	))

	if isDM {
		b.mu.RLock()
		rootAgent := b.rootAgent
		b.mu.RUnlock()

		b.deliverToAgent(ctx, &Route{
			PluginName: pluginName,
			ChannelID:  channelID,
			Agent:      rootAgent,
			Trust:      "trusted",
		}, content, sender)
		return
	}

	// Look up route from plugin config.
	route := b.getRoute(pluginName, channelID)
	if route == nil {
		b.logger.Debug("channel-bridge: no route found",
			"plugin", pluginName, "channel", channelID)
		return
	}

	// Handle trust level.
	if route.Trust == "untrusted" {
		b.mu.RLock()
		reviewFn := b.reviewFn
		b.mu.RUnlock()

		if reviewFn != nil {
			// Acquire semaphore to limit concurrent reviews.
			select {
			case b.reviewSem <- struct{}{}:
				defer func() { <-b.reviewSem }()
			case <-ctx.Done():
				return
			}

			reviewed, err := reviewFn(ctx, route, content, sender)
			if err != nil {
				b.logger.Warn("channel-bridge: security review rejected",
					"plugin", pluginName, "channel", channelID, "error", err)
				b.bus.Publish(nervous.NewEvent(
					types.EventChannelBridgeRejected,
					pluginName,
					"channel-bridge",
					map[string]string{
						"plugin":     pluginName,
						"channel_id": channelID,
						"reason":     err.Error(),
					},
				))
				return
			}
			content = reviewed
		} else {
			b.logger.Warn("channel-bridge: untrusted route but no review function configured, rejecting",
				"plugin", pluginName, "channel", channelID)
			return
		}
	}

	b.deliverToAgent(ctx, route, content, sender)
}

// deliverToAgent sends a message to the routed agent via CommHub and triggers completion.
func (b *Bridge) deliverToAgent(ctx context.Context, route *Route, content, sender string) {
	peerID := fmt.Sprintf("ext:%s:%s", route.PluginName, route.ChannelID)

	// Build message envelope.
	env := &types.MessageEnvelope{
		ID:          fmt.Sprintf("ext-%s-%s", route.PluginName, route.ChannelID),
		From:        peerID,
		To:          route.Agent,
		Trust:       types.TrustExternal,
		ContentType: "text/plain",
		Content:     content,
		Metadata: map[string]string{
			"source_plugin":  route.PluginName,
			"source_channel": route.ChannelID,
			"sender":         sender,
		},
	}

	if err := b.hub.Send(ctx, env); err != nil {
		b.logger.Error("channel-bridge: failed to deliver message",
			"agent", route.Agent, "error", err)
		b.bus.Publish(nervous.NewEvent(
			types.EventChannelBridgeError,
			route.PluginName,
			"channel-bridge",
			map[string]string{"error": err.Error()},
		))
		return
	}

	// Log to comm_log if available.
	if b.commLog != nil {
		if err := b.commLog.Log(ctx, env, "delivered"); err != nil {
			b.logger.Debug("channel-bridge: comm log error", "error", err)
		}
	}

	b.bus.Publish(nervous.NewEvent(
		types.EventChannelBridgeDelivered,
		route.PluginName,
		"channel-bridge",
		map[string]string{
			"plugin":     route.PluginName,
			"channel_id": route.ChannelID,
			"agent":      route.Agent,
		},
	))

	// Trigger completion via the agent scheduler's work queue or directly.
	b.mu.RLock()
	completeFn := b.completeFn
	b.mu.RUnlock()

	if completeFn != nil {
		sessionID := fmt.Sprintf("ext:%s:%s", route.PluginName, route.ChannelID)
		completeFn(route.Agent, peerID, content, sessionID)
	}
}

// handleOutbound mirrors agent responses back to external channels.
func (b *Bridge) handleOutbound(ctx context.Context, event types.NervousEvent) {
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return
	}

	// Check if the "to" field starts with "ext:" indicating an external target.
	to, ok := payload["to"].(string)
	if !ok || !strings.HasPrefix(to, "ext:") {
		return
	}

	// Only mirror text content (skip tool_call, tool_result).
	contentType, _ := payload["content_type"].(string)
	if contentType != "" && contentType != "text/plain" && contentType != "text" {
		return
	}

	content, _ := payload["content"].(string)
	if content == "" {
		return
	}

	// Parse "ext:<pluginName>:<channelID>"
	parts := strings.SplitN(strings.TrimPrefix(to, "ext:"), ":", 2)
	if len(parts) != 2 {
		return
	}
	pluginName := parts[0]
	channelID := parts[1]

	// Build plugin-specific send params.
	sendParams := map[string]any{
		"channel_id": channelID,
		"content":    content,
	}

	paramsJSON, err := json.Marshal(sendParams)
	if err != nil {
		return
	}

	// Call the plugin's send_message tool.
	toolName := fmt.Sprintf("plugin_%s_%s_send_message", pluginName, pluginName)
	_, err = b.dispatch(ctx, toolName, paramsJSON)
	if err != nil {
		b.logger.Warn("channel-bridge: failed to mirror response",
			"plugin", pluginName, "channel", channelID, "error", err)
		b.bus.Publish(nervous.NewEvent(
			types.EventChannelBridgeError,
			pluginName,
			"channel-bridge",
			map[string]string{
				"error":      err.Error(),
				"channel_id": channelID,
			},
		))
		return
	}

	b.bus.Publish(nervous.NewEvent(
		types.EventChannelBridgeResponse,
		pluginName,
		"channel-bridge",
		map[string]string{
			"plugin":     pluginName,
			"channel_id": channelID,
		},
	))
}
