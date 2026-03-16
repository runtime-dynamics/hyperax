package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

// federatedToolClearance is the ABAC clearance level assigned to all federated
// tools. Admin (tier 2) ensures that only privileged callers can invoke tools
// from external MCP servers.
const federatedToolClearance = 2 // Admin

// Connection represents an active federation link to a remote MCP server.
type Connection struct {
	ID          string    `json:"id"`
	Endpoint    string    `json:"endpoint"`
	Client      *Client   `json:"-"` // excluded from JSON serialization
	Tools       []string  `json:"tools"`        // qualified tool names registered
	ConnectedAt time.Time `json:"connected_at"`
}

// Manager tracks active federated server connections and manages tool
// registration/unregistration in the local MCP ToolRegistry.
type Manager struct {
	mu          sync.RWMutex
	connections map[string]*Connection // keyed by connection ID
	registry    *mcp.ToolRegistry
	logger      *slog.Logger
}

// NewManager creates a federation Manager that registers/unregisters remote
// tools in the given ToolRegistry.
//
// Parameters:
//   - registry: the central MCP tool registry for registering proxy handlers
//   - logger: structured logger for federation operations
func NewManager(registry *mcp.ToolRegistry, logger *slog.Logger) *Manager {
	return &Manager{
		connections: make(map[string]*Connection),
		registry:    registry,
		logger:      logger.With("component", "federation"),
	}
}

// Connect establishes a federation link to a remote MCP server. It performs
// the MCP initialize handshake, discovers available tools, and registers
// proxy handlers in the local ToolRegistry with the namespace "fed_{id}_{tool}".
//
// Parameters:
//   - ctx: context for the connection handshake
//   - endpoint: the URL of the remote MCP server
//   - authToken: optional Bearer token for authenticating with the remote server
//
// Returns the new Connection or an error if the handshake or tool discovery fails.
func (m *Manager) Connect(ctx context.Context, endpoint string, authToken string) (*Connection, error) {
	// Build client options.
	var opts []ClientOption
	if authToken != "" {
		opts = append(opts, WithAuthToken(authToken))
	}

	client := NewClient(endpoint, opts...)

	// MCP initialize handshake.
	_, err := client.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("federation.Manager.Connect: %w", err)
	}

	// Discover available tools.
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("federation.Manager.Connect: %w", err)
	}

	connID := uuid.New().String()[:12] // short ID for readable tool names

	// Register each remote tool as a proxy in the local registry.
	qualifiedNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		qualifiedName := fmt.Sprintf("fed_%s_%s", connID, tool.Name)

		handler := makeFederatedProxyHandler(client, tool.Name, connID, m.logger)

		inputSchema := tool.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
		}

		// Unregister if already exists (defensive, should not happen with UUIDs).
		if m.registry.HasTool(qualifiedName) {
			m.registry.Unregister(qualifiedName)
		}

		m.registry.Register(qualifiedName, tool.Description, inputSchema, handler)
		m.registry.SetToolABAC(qualifiedName, federatedToolClearance, "execute")

		qualifiedNames = append(qualifiedNames, qualifiedName)

		m.logger.Info("federated remote tool",
			"connection", connID,
			"remote_tool", tool.Name,
			"qualified", qualifiedName,
		)
	}

	conn := &Connection{
		ID:          connID,
		Endpoint:    endpoint,
		Client:      client,
		Tools:       qualifiedNames,
		ConnectedAt: time.Now(),
	}

	m.mu.Lock()
	m.connections[connID] = conn
	m.mu.Unlock()

	m.logger.Info("federation connection established",
		"connection_id", connID,
		"endpoint", endpoint,
		"tool_count", len(qualifiedNames),
	)

	return conn, nil
}

// Disconnect removes a federation connection, unregisters all its tools from
// the local ToolRegistry, and closes the underlying client.
//
// Parameters:
//   - id: the connection ID returned by Connect
//
// Returns an error if the connection ID is not found.
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	conn, ok := m.connections[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("federation.Manager.Disconnect: connection %q not found", id)
	}
	delete(m.connections, id)
	m.mu.Unlock()

	// Unregister all tools from the registry.
	for _, toolName := range conn.Tools {
		m.registry.Unregister(toolName)
	}

	conn.Client.Close()

	m.logger.Info("federation connection disconnected",
		"connection_id", id,
		"endpoint", conn.Endpoint,
		"tools_removed", len(conn.Tools),
	)

	return nil
}

// List returns all active federation connections. The returned slice is a
// snapshot; callers may read it without holding any lock.
func (m *Manager) List() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		out = append(out, conn)
	}
	return out
}

// GetConnection returns a single connection by ID, or false if not found.
func (m *Manager) GetConnection(id string) (*Connection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.connections[id]
	return conn, ok
}

// Refresh re-discovers tools from an existing federation connection. It unregisters
// stale tools and registers any new ones the remote server has added.
func (m *Manager) Refresh(ctx context.Context, id string) (*Connection, error) {
	m.mu.RLock()
	conn, ok := m.connections[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("federation.Manager.Refresh: connection %q not found", id)
	}

	// Re-discover tools from the remote server.
	tools, err := conn.Client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("federation.Manager.Refresh: %w", err)
	}

	// Unregister old tools.
	for _, toolName := range conn.Tools {
		m.registry.Unregister(toolName)
	}

	// Register the fresh tool set.
	qualifiedNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		qualifiedName := fmt.Sprintf("fed_%s_%s", id, tool.Name)

		handler := makeFederatedProxyHandler(conn.Client, tool.Name, id, m.logger)
		inputSchema := tool.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
		}

		if m.registry.HasTool(qualifiedName) {
			m.registry.Unregister(qualifiedName)
		}
		m.registry.Register(qualifiedName, tool.Description, inputSchema, handler)
		m.registry.SetToolABAC(qualifiedName, federatedToolClearance, "execute")

		qualifiedNames = append(qualifiedNames, qualifiedName)
	}

	m.mu.Lock()
	conn.Tools = qualifiedNames
	m.mu.Unlock()

	m.logger.Info("federation connection refreshed",
		"connection_id", id,
		"endpoint", conn.Endpoint,
		"tool_count", len(qualifiedNames),
	)

	return conn, nil
}

// DisconnectAll removes all federation connections. Called during graceful shutdown.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.connections))
	for id := range m.connections {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Disconnect(id)
	}
}

// makeFederatedProxyHandler creates an MCP ToolHandler that forwards tool calls
// to the remote MCP server via the HTTP Client. The response is returned as a
// ToolResult, following the same pattern as internal/plugin/federation.go.
func makeFederatedProxyHandler(client *Client, toolName, connID string, logger *slog.Logger) mcp.ToolHandler {
	return func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		result, err := client.CallTool(ctx, toolName, params)
		if err != nil {
			logger.Error("federated tool call failed",
				"connection", connID,
				"tool", toolName,
				"error", err.Error(),
			)
			return types.NewErrorResult(fmt.Sprintf("federated tool %q (connection %s) error: %v", toolName, connID, err)), nil
		}

		// Try to unmarshal as a standard MCP ToolResult.
		var toolResult types.ToolResult
		if err := json.Unmarshal(result, &toolResult); err != nil {
			// Non-standard format — wrap as text content.
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
