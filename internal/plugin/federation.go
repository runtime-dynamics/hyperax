package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

// defaultPluginClearance is the ABAC clearance level assigned to plugin tools
// when the manifest does not specify one. Operator (tier 1) allows create/update
// operations but not admin-level actions.
const defaultPluginClearance = 1 // Operator

// FederateTool registers a single plugin tool in the main ToolRegistry.
// The tool is namespaced as "plugin_{pluginName}_{toolName}" and its handler
// proxies calls through the MCPClient to the plugin subprocess.
//
// Parameters:
//   - registry: the central MCP tool registry
//   - pluginName: the plugin's manifest name (used for namespacing)
//   - tool: the tool definition from the plugin's tools/list response
//   - client: the MCP client connected to the plugin subprocess
//   - clearance: ABAC clearance level (0=Observer, 1=Operator, 2=Admin, 3=ChiefOfStaff)
//   - logger: structured logger for the plugin
//
// Returns the qualified tool name that was registered.
func FederateTool(
	registry *mcp.ToolRegistry,
	pluginName string,
	tool MCPToolDef,
	client *MCPClient,
	clearance int,
	logger *slog.Logger,
) string {
	qualifiedName := fmt.Sprintf("plugin_%s_%s", pluginName, tool.Name)

	// Build the handler that proxies through the MCP client.
	handler := makeProxyHandler(client, tool.Name, pluginName, logger)

	// Use the input schema from the plugin directly.
	inputSchema := tool.InputSchema
	if len(inputSchema) == 0 {
		inputSchema = json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
	}

	// Register in the main registry. If the tool already exists (e.g., from a
	// previous load), skip to avoid panics. The caller should unregister first.
	if registry.HasTool(qualifiedName) {
		registry.Unregister(qualifiedName)
	}

	registry.Register(qualifiedName, tool.Description, inputSchema, handler)

	// Set ABAC clearance on the newly registered tool.
	if clearance < 0 {
		clearance = defaultPluginClearance
	}
	registry.SetToolABAC(qualifiedName, clearance, "execute")

	logger.Info("federated plugin tool",
		"plugin", pluginName,
		"tool", tool.Name,
		"qualified", qualifiedName,
		"clearance", clearance,
	)

	return qualifiedName
}

// FederateTools registers all tools from a plugin's tools/list response.
// Returns the list of qualified tool names that were registered.
func FederateTools(
	registry *mcp.ToolRegistry,
	pluginName string,
	tools []MCPToolDef,
	client *MCPClient,
	clearance int,
	logger *slog.Logger,
) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := FederateTool(registry, pluginName, tool, client, clearance, logger)
		names = append(names, name)
	}
	return names
}

// FederateToolsFromManifest registers tools from manifest ToolDef entries (not
// from MCP tools/list discovery). Used by service-type plugins that declare their
// tools in the manifest rather than via MCP protocol negotiation.
func FederateToolsFromManifest(
	registry *mcp.ToolRegistry,
	pluginName string,
	toolDefs []types.ToolDef,
	client *MCPClient,
	clearance int,
	logger *slog.Logger,
) []string {
	names := make([]string, 0, len(toolDefs))
	for _, td := range toolDefs {
		// Convert manifest ToolDef to MCPToolDef format.
		mcpTool := MCPToolDef{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: buildInputSchemaFromToolDef(td),
		}
		name := FederateTool(registry, pluginName, mcpTool, client, clearance, logger)
		names = append(names, name)
	}
	return names
}

// buildInputSchemaFromToolDef generates a JSON Schema from a manifest ToolDef.
func buildInputSchemaFromToolDef(tool types.ToolDef) json.RawMessage {
	properties := make(map[string]any)
	var required []string

	for _, p := range tool.Parameters {
		prop := map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	} else {
		schema["required"] = []string{}
	}

	data, err := json.Marshal(schema)
	if err != nil {
		// Schema is built from known types; marshal failure is highly unexpected.
		// Return a minimal valid schema as a safe fallback.
		return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
	}
	return data
}

// makeProxyHandler creates an MCP ToolHandler that forwards the call to the
// plugin subprocess via the MCPClient. The response from the plugin is
// returned as the ToolResult.
func makeProxyHandler(client *MCPClient, toolName, pluginName string, logger *slog.Logger) mcp.ToolHandler {
	return func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		result, err := client.CallTool(ctx, toolName, params)
		if err != nil {
			logger.Error("plugin tool call failed",
				"plugin", pluginName,
				"tool", toolName,
				"error", err.Error(),
			)
			return types.NewErrorResult(fmt.Sprintf("plugin %q tool %q error: %v", pluginName, toolName, err)), nil
		}

		// The plugin returns a standard MCP ToolResult. Try to unmarshal it directly.
		var toolResult types.ToolResult
		if err := json.Unmarshal(result, &toolResult); err != nil {
			// If the plugin returned a non-standard format, wrap it as text content.
			return &types.ToolResult{
				Content: []types.ToolContent{{
					Type: "text",
					Text: string(result),
				}},
			}, nil
		}

		return &toolResult, nil
	}
}
