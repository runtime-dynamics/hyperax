// Package plugin provides the public SDK interface for Hyperax plugin authors.
//
// Plugin developers implement the ToolProvider interface to expose custom tools
// to the Hyperax MCP server. The types package (pkg/types) defines the shared
// manifest and state structures used by both the SDK and the internal plugin manager.
package plugin

import (
	"context"
	"log/slog"

	"github.com/hyperax/hyperax/pkg/types"
)

// ToolProvider is the interface that Go plugins must implement.
// Native plugins are loaded via Go's plugin.Open() mechanism and must
// export a symbol that satisfies this interface. Wasm, MCP, and HTTP
// plugins use their respective loaders instead.
type ToolProvider interface {
	// Init is called once when the plugin is loaded. The PluginContext provides
	// workspace paths, a scoped logger, and any plugin-specific configuration.
	Init(ctx PluginContext) error

	// Tools returns the list of tools this plugin provides. This is called
	// after Init and used to register tools with the MCP tool registry.
	Tools() []types.ToolDef

	// Execute runs a tool by name with the given arguments. The context
	// carries cancellation and deadline from the MCP request.
	Execute(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error)

	// Shutdown is called when the plugin is unloaded. Implementations should
	// release resources, close connections, and flush any pending writes.
	Shutdown() error
}

// ToolResult is the result returned by a plugin tool execution.
type ToolResult struct {
	// Content is the primary return value. It will be JSON-serialized
	// and wrapped in the MCP ToolResult envelope.
	Content any `json:"content"`

	// Metadata carries optional key-value pairs for diagnostics, tracing,
	// or plugin-specific context. Not shown to the end user by default.
	Metadata map[string]any `json:"metadata,omitempty"`

	// IsError indicates that the tool execution failed. When true, Content
	// should contain a human-readable error message.
	IsError bool `json:"is_error"`
}

// PluginContext is passed to ToolProvider.Init and provides the plugin with
// access to workspace paths, logging, and configuration.
type PluginContext struct {
	// WorkspaceRoot is the absolute path to the workspace root directory.
	WorkspaceRoot string

	// StoragePath is the absolute path to a plugin-private storage directory.
	// Plugins may read and write files here without affecting the workspace.
	StoragePath string

	// Logger is a structured logger scoped to this plugin's name.
	Logger *slog.Logger

	// Config holds plugin-specific configuration from hyperax.yaml or
	// the runtime config store.
	Config map[string]any
}
