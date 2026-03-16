package config

import (
	"context"
	"log/slog"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// DefaultConfigKeys defines the built-in configuration keys seeded on first startup.
var DefaultConfigKeys = []types.ConfigKeyMeta{
	{Key: "server.listen_addr", ScopeType: "global", ValueType: "string", DefaultVal: ":9090", Description: "HTTP server listen address"},
	{Key: "server.read_timeout", ScopeType: "global", ValueType: "duration", DefaultVal: "30s", Description: "HTTP read timeout"},
	{Key: "server.write_timeout", ScopeType: "global", ValueType: "duration", DefaultVal: "60s", Description: "HTTP write timeout"},
	{Key: "server.shutdown_timeout", ScopeType: "global", ValueType: "duration", DefaultVal: "30s", Description: "Graceful shutdown timeout"},
	{Key: "log.level", ScopeType: "global", ValueType: "string", DefaultVal: "info", Description: "Log level (debug, info, warn, error)"},
	{Key: "log.format", ScopeType: "global", ValueType: "string", DefaultVal: "json", Description: "Log format (json, text)"},
	{Key: "index.enabled", ScopeType: "workspace", ValueType: "bool", DefaultVal: "true", Description: "Enable file indexing for workspace"},
	{Key: "index.max_file_size", ScopeType: "workspace", ValueType: "int", DefaultVal: "1048576", Description: "Maximum file size to index (bytes)"},
	{Key: "index.watch_enabled", ScopeType: "workspace", ValueType: "bool", DefaultVal: "true", Description: "Enable filesystem watching"},
	{Key: "search.enable_vector", ScopeType: "global", ValueType: "bool", DefaultVal: "false", Description: "Enable vector search (requires ONNX model)"},
	{Key: "search.fusion_k", ScopeType: "global", ValueType: "int", DefaultVal: "60", Description: "RRF fusion constant k"},
	{Key: "pipeline.timeout", ScopeType: "workspace", ValueType: "duration", DefaultVal: "10m", Description: "Default pipeline execution timeout"},
	{Key: "pipeline.max_concurrent", ScopeType: "global", ValueType: "int", DefaultVal: "4", Description: "Maximum concurrent pipeline jobs"},
	{Key: "dashboard.theme", ScopeType: "agent", ValueType: "string", DefaultVal: "system", Description: "Dashboard theme (light, dark, system)"},
	{Key: "mcp.transport", ScopeType: "global", ValueType: "string", DefaultVal: "sse", Critical: true, Description: "MCP transport mode (sse, streamable-http)"},
	{Key: "guard.enabled", ScopeType: "global", ValueType: "bool", DefaultVal: "false", Description: "Enable guard middleware for tool call approval gates"},
	{Key: "guard.auto_approve_reads", ScopeType: "global", ValueType: "bool", DefaultVal: "true", Description: "Auto-approve read-only tool calls without guard evaluation"},
	{Key: "guard.allow_bypass", ScopeType: "global", ValueType: "bool", DefaultVal: "false", Description: "Allow personas with guard_bypass=true to skip guard evaluation"},
	{Key: "tooluse.max_iterations", ScopeType: "global", ValueType: "int", DefaultVal: "100", Description: "Maximum tool-use loop iterations per chat completion"},
	{Key: "tooluse.auto_continue", ScopeType: "global", ValueType: "bool", DefaultVal: "false", Description: "Automatically continue tool-use loop past max iterations instead of stopping"},
}

// SeedConfigKeys inserts default config key definitions if they don't already exist.
func SeedConfigKeys(ctx context.Context, configRepo repo.ConfigRepo, logger *slog.Logger) error {
	for _, key := range DefaultConfigKeys {
		if err := configRepo.UpsertKeyMeta(ctx, &key); err != nil {
			logger.Warn("failed to seed config key", "key", key.Key, "error", err)
			continue
		}
	}
	logger.Info("config keys seeded", "count", len(DefaultConfigKeys))
	return nil
}
