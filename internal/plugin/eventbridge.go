package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// EventBridge translates JSON-RPC notifications from plugin subprocesses into
// NervousEvent publications on the Hyperax EventBus.
//
// When a plugin sends a notification like:
//
//	{"jsonrpc":"2.0","method":"discord/message_received","params":{...}}
//
// The bridge converts the method to an EventType by replacing "/" with "."
// (e.g., "discord.message_received") and publishes it with the plugin name
// as the event source.
//
// If an ApprovalGate is set and the plugin requires approval, notifications
// are blocked until the plugin has been approved.
type EventBridge struct {
	bus          *nervous.EventBus
	logger       *slog.Logger
	approvalGate *ApprovalGate
	// manifests is looked up via the manager to check approval_required.
	manifestLookup func(name string) (*types.PluginManifest, error)
}

// NewEventBridge creates an EventBridge that publishes to the given EventBus.
func NewEventBridge(bus *nervous.EventBus, logger *slog.Logger) *EventBridge {
	return &EventBridge{
		bus:    bus,
		logger: logger.With("component", "plugin-event-bridge"),
	}
}

// SetApprovalGate configures the approval gate for blocking unapproved plugin events.
func (eb *EventBridge) SetApprovalGate(gate *ApprovalGate) {
	eb.approvalGate = gate
}

// SetManifestLookup sets the function used to retrieve a plugin's manifest
// for checking whether it requires approval.
func (eb *EventBridge) SetManifestLookup(fn func(name string) (*types.PluginManifest, error)) {
	eb.manifestLookup = fn
}

// NotificationHandler returns a function suitable for MCPClient.NotificationHandler.
// Each notification from the plugin is mapped to a NervousEvent and published.
//
// Parameters:
//   - pluginName: the name of the plugin (used as event source prefix)
//
// The returned handler is safe for concurrent use.
func (eb *EventBridge) NotificationHandler(pluginName string) func(method string, params json.RawMessage) {
	return func(method string, params json.RawMessage) {
		// Check approval gate if set.
		if eb.approvalGate != nil && eb.manifestLookup != nil {
			manifest, err := eb.manifestLookup(pluginName)
			if err == nil && manifest.ApprovalRequired {
				ctx := context.Background()
				if !eb.approvalGate.IsApproved(ctx, pluginName) {
					eb.logger.Warn("blocking notification from unapproved plugin",
						"plugin", pluginName, "method", method)
					return
				}
			}
		}

		// Convert method separator: "discord/message_received" → "discord.message_received"
		eventType := types.EventType(strings.ReplaceAll(method, "/", "."))

		// Parse the params into a payload map for the event.
		var payload map[string]any
		if len(params) > 0 {
			if err := json.Unmarshal(params, &payload); err != nil {
				// If params aren't a map, wrap in a "data" key.
				payload = map[string]any{"raw": string(params)}
			}
		}
		if payload == nil {
			payload = map[string]any{}
		}

		// Tag the payload with the plugin source.
		payload["_plugin"] = pluginName

		source := "plugin:" + pluginName
		event := nervous.NewEvent(eventType, source, pluginName, payload)

		eb.bus.Publish(event)

		eb.logger.Debug("bridged plugin notification to event",
			"plugin", pluginName,
			"method", method,
			"event_type", string(eventType),
		)
	}
}
