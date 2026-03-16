# Hyperax Plugin System Architecture

The Hyperax plugin system extends the platform with external capabilities through a unified lifecycle model. Plugins run as managed subprocesses, communicate via JSON-RPC 2.0, and have their tools federated into the main MCP server with ABAC clearance gating.

This document describes the architecture. For a practical guide to building plugins, see [PluginContribution.md](PluginContribution.md).

---

## Plugin Types

Hyperax supports five plugin execution mechanisms. Each determines how the binary is launched, how communication is established, and how tools are discovered.

### MCP (`mcp`)

The most common plugin type. MCP plugins run as child processes and communicate over stdin/stdout using JSON-RPC 2.0, following the Model Context Protocol specification (protocol version `2024-11-05`).

**Lifecycle on enable:**
1. Subprocess launched with stdin/stdout/stderr pipes
2. MCP `initialize` request sent (30-second timeout)
3. `notifications/initialized` notification sent
4. `tools/list` request discovers available tools
5. Discovered tools replace manifest placeholders in the MCP registry
6. Proxy handlers forward all tool calls to the subprocess

**Tool discovery:** Dynamic. The plugin reports its tools via `tools/list`, which may differ from the manifest declarations. The discovered set is authoritative.

**Use when:** Your plugin provides tools that agents or operators invoke on demand (GitHub API, Jira, custom REST wrappers).

### Service (`service`)

Long-running services that maintain persistent external connections. Service plugins use JSON-RPC over stdin/stdout for tool calls and event notifications, but skip the MCP handshake (`initialize` / `tools/list`).

**Lifecycle on enable:**
1. Subprocess launched with stdin/stdout/stderr pipes
2. MCPClient created for JSON-RPC communication (no handshake)
3. Tools registered from manifest definitions (not discovered dynamically)
4. Event bridge wired for notifications

**Tool discovery:** Static. Tools come from the manifest `tools` section. The plugin does not need to implement `initialize` or `tools/list`.

**Use when:** Your plugin maintains persistent connections to external systems (Discord bot, Slack socket, email IMAP poller) and needs to both receive and send messages.

### WASM (`wasm`)

WebAssembly plugins for sandboxed execution. The manifest can declare sandbox constraints:

| Setting | Description |
|---------|-------------|
| `max_memory_mb` | Maximum memory allocation |
| `allowed_paths` | Filesystem paths the module can access |
| `allow_network` | Whether network access is permitted |
| `timeout` | Maximum execution time per tool call |

**Status:** Type is recognized and validated. Runtime loader is planned.

### HTTP (`http`)

Remote plugins accessed via HTTP endpoints. The plugin runs as an external service and Hyperax communicates with it over HTTP.

**Status:** Type is recognized and validated. Runtime loader is planned.

### Native (`native`)

Go plugins compiled directly into the Hyperax binary. Used for tightly coupled extensions that need direct access to internal APIs.

**Status:** Type is recognized and validated. Runtime loader is planned.

---

## Integration Categories

Every plugin declares an integration category that determines where it slots into the Hyperax architecture. The category controls ABAC clearance levels, bridge registration, and special lifecycle behaviors.

### channel (clearance: Admin / tier 2)

Communication adapters that bridge external messaging platforms into the CommHub.

**Responsibilities:**
- Translate external messages into CommHub format with trust-level mapping
- Support bidirectional message flow (receive from platform, send via tool calls)
- Handle owner verification flow for security-sensitive platforms
- Publish notifications for inbound messages (bridged to nervous system events)

**Examples:** Discord, Slack, Email, SMS

### tooling (clearance: Operator / tier 1)

Additional MCP tools that extend agent capabilities. This is the default category when none is specified in the manifest.

**Responsibilities:**
- Expose tools that agents can invoke via the MCP protocol
- Tools are federated into the main MCP server and appear in `tools/list`
- Agents with Operator-level clearance can call these tools

**Examples:** GitHub API tools, Jira integration, custom REST API wrappers

### secret_provider (clearance: ChiefOfStaff / tier 3)

External secret management backends. When enabled, a `PluginSecretAdapter` is created that bridges the `secrets.Provider` interface to the plugin's MCP tools.

**Required tools:**
- `get_secret` / `set_secret` / `delete_secret` / `list_secrets`
- `list_secret_entries` / `get_access_scope` / `update_access_scope`
- `rotate_secret` / `health_check`

**Special behaviors:**
- Adapter registered with `secrets.Registry` on enable
- Adapter unregistered on disable
- Disable/uninstall blocked if the provider is active and holds secrets
- Highest ABAC clearance (tier 3) required for all tool calls

**Examples:** HashiCorp Vault, 1Password

### sensor (clearance: Admin / tier 2)

Monitoring data sources that feed metrics and alerts into the observability pipeline.

**Responsibilities:**
- Poll external monitoring systems for metrics
- Publish threshold-based events to the nervous system
- Provide query tools for on-demand metric retrieval

**Examples:** Prometheus, Datadog, CloudWatch

### guard (clearance: Admin / tier 2)

Approval gate plugins that intercept tool calls requiring human approval.

**Required tools:**
- `evaluate` -- receives tool call context and returns approve/deny/queue

**Special behaviors:**
- On enable, the `evaluate` tool is registered with the guard middleware
- The guard middleware calls the plugin's evaluate tool before executing gated actions
- 5-minute timeout on evaluation calls

**Examples:** Approval Gate (configurable rules for action-based, tool-based, persona-based, and time-based gating)

### audit (clearance: Admin / tier 2)

Audit trail sinks that receive events for compliance logging.

**Required tools:**
- `write_audit_event` -- receives structured audit event data

**Special behaviors:**
- On enable, the tool is registered with the `PluginAuditSink`
- All auditable events are forwarded to the plugin for external persistence

**Examples:** Audit File (JSONL writer), SIEM forwarding, S3 archival

---

## Plugin Manifest

Every plugin must include a `hyperax-plugin.yaml` file. This is the single source of truth for the plugin's identity, capabilities, and requirements.

### Full Schema

```yaml
# Required fields
name: my-plugin                    # Unique identifier (alphanumeric + hyphens)
version: 1.0.0                     # Semantic version
type: mcp                          # mcp | service | wasm | http | native
description: What this plugin does
tools:                             # At least one tool required
  - name: my_tool
    description: What this tool does
    parameters:
      - name: input
        type: string
        required: true
        description: The input value

# Recommended fields
author: Your Name
license: Apache-2.0
source_repo: github.com/org/repo
integration: tooling               # channel | tooling | secret_provider | sensor | guard | audit
entrypoint: ./my-plugin            # Binary to execute (resolved relative to plugin dir)
args: []                           # Additional arguments passed to entrypoint
min_hyperax_version: "1.0.0"       # Minimum compatible Hyperax version
api_version: "1.0"                 # Plugin API version (major must match)

# Configuration variables
variables:
  - name: API_KEY
    type: string                   # string | int | float | bool | array_string | array_int | array_float
    required: true
    secret: true                   # If true, value resolved from secret store
    dynamic: false                 # If true, changes pushed via notifications/configChanged
    env_name: MY_PLUGIN_API_KEY    # Override environment variable name (default: variable name)
    description: API key for the service

# Legacy environment variables (auto-converted to variables if variables is empty)
env:
  - name: SOME_VAR
    required: false
    default: "value"
    description: Legacy env var

# Approval gate (for channel plugins requiring owner verification)
approval_required: false

# Auto-created resources
resources:
  - type: cron_job
    name: poll-metrics
    config:
      schedule: "*/5 * * * *"
      job_type: tool
      payload:
        tool: plugin_my-plugin_poll

# Events emitted by this plugin
events:
  - type: my_plugin.data_received
    description: Fired when new data arrives

# Health check configuration
health_check:
  interval: 30s
  timeout: 5s
  endpoint: /health              # For HTTP plugins

# WASM sandbox settings (wasm type only)
sandbox:
  max_memory_mb: 128
  allowed_paths: ["/tmp/plugin-data"]
  allow_network: false
  timeout: 30s

# Permissions requested
permissions:
  - tools:register
  - network:local

# goreleaser artifact mappings (for GitHub release distribution)
artifacts:
  darwin_amd64: my-plugin-darwin-amd64.tar.gz
  darwin_arm64: my-plugin-darwin-arm64.tar.gz
  linux_amd64: my-plugin-linux-amd64.tar.gz
```

### Manifest Validation

The following rules are enforced at load time:

1. `name` must be non-empty
2. `version` must be non-empty
3. `type` must be one of: `wasm`, `mcp`, `http`, `native`, `service`
4. At least one tool must be declared with non-empty `name` and `description`
5. `min_hyperax_version` is checked via semver (skipped for `dev` builds)
6. `api_version` major must match the supported API major version (`1`)
7. `integration` must be a valid category (defaults to `tooling`)
8. Variables must have valid types and environment-compatible names (`[A-Za-z_][A-Za-z0-9_]*`)
9. Permissions must be from the allowed set

### Allowed Permissions

| Permission | Description |
|-----------|-------------|
| `workspace:read` | Read files and symbols in workspaces |
| `workspace:write` | Modify files in workspaces |
| `tools:register` | Register new MCP tools |
| `storage:read` | Read from plugin-private storage |
| `storage:write` | Write to plugin-private storage |
| `network:local` | Make HTTP requests to localhost |
| `network:*` | Make HTTP requests to any host |

---

## Plugin Lifecycle

### States

```
Install --> Load --> Enable --> Running --> Disable --> Uninstall
                       |                      |
                       +------ Error ---------+
```

| State | Description |
|-------|-------------|
| **loaded** | Manifest parsed and validated. Placeholder tools registered. Subprocess not running. |
| **enabled** | Subprocess running (MCP/service types). Real proxy tools registered. |
| **disabled** | Subprocess stopped. Placeholder tools re-registered. |
| **error** | Enable failed or subprocess crashed. Error details in `PluginState.Error`. |

### Discovery and Loading

On server startup, the PluginManager executes this sequence:

1. **Discover()** -- scans `pluginDir` for subdirectories containing `hyperax-plugin.yaml`
2. **LoadFromRegistry()** -- loads any plugins from the install registry that Discover() missed (remote installs, alternate paths)
3. **RestoreEnabledPlugins()** -- reads persisted plugin states from the database and auto-enables plugins that were enabled before the last shutdown

Each discovered manifest is parsed, validated, and loaded:
- Permission validation against the allowed set
- Placeholder tools registered in the MCP registry (return "not yet connected" on call)
- Config key metadata seeded for plugin variables
- Plugin state event published

### Enabling

When a plugin is enabled:

1. Entrypoint resolved (absolute path, PATH lookup, plugin dir, registry path)
2. Subprocess launched with `context.Background()` (not the request context)
3. Plugin variables resolved and injected as environment variables
4. For MCP: initialize handshake, tool discovery, federation
5. For Service: JSON-RPC client created, manifest tools federated
6. State persisted to database
7. Auto-created resources provisioned (cron jobs)
8. Integration-specific bridges registered (secret adapter, guard evaluator, audit writer)

### Disabling

When a plugin is disabled:

1. Integration bridges unregistered (with safety checks for secret providers)
2. Subprocess stopped via graceful shutdown sequence
3. Federated tools replaced with manifest placeholders
4. State persisted as disabled

### Uninstalling

When a plugin is uninstalled:

1. Auto-created resources cleaned up
2. Integration bridges unregistered
3. Subprocess stopped
4. All tools deregistered
5. Plugin directory removed from disk
6. Install registry entry removed
7. Database state deleted

---

## Subprocess Management

### Launch

Subprocesses are launched via `exec.CommandContext` with:
- stdin/stdout pipes for JSON-RPC communication
- stderr piped to the Hyperax logger (at Info level)
- Environment: inherited OS environment + resolved plugin variables
- Context: `context.Background()` (survives the enable request)

### Graceful Shutdown Sequence

```
1. Close stdin (signals EOF)      -- wait 5 seconds
2. Send SIGTERM                    -- wait 5 seconds
3. Send SIGKILL                    -- last resort
```

### Auto-Restart on Crash

If a subprocess exits unexpectedly (not during shutdown):
1. Crash event published
2. Restart attempted with linear backoff (`attempt_number * 1 second`)
3. Maximum 3 restart attempts
4. After max attempts, plugin marked as errored and stays stopped

### Health Checking

The HealthChecker runs a periodic loop (default 30 seconds):
- Checks that enabled plugins are in the expected state
- Tracks consecutive failure count per plugin
- Auto-disables plugins after 3 consecutive failures

---

## Tool Federation

Plugin tools are registered in the main MCP ToolRegistry with namespaced names:

```
plugin_{pluginName}_{toolName}
```

For example, a Discord plugin tool `discord_send_message` becomes `plugin_discord_discord_send_message`.

### ABAC Clearance

Each federated tool is assigned a clearance level based on the plugin's integration category:

| Integration | Clearance | Tier | Rationale |
|------------|-----------|------|-----------|
| tooling | Operator | 1 | Agents can call these |
| channel | Admin | 2 | Internal routing |
| sensor | Admin | 2 | Event publishing |
| guard | Admin | 2 | Evaluation tools |
| audit | Admin | 2 | Audit stream |
| secret_provider | ChiefOfStaff | 3 | Highest security |

### Proxy Handlers

For MCP plugins, proxy handlers forward tool calls to the subprocess:

```
MCP Server --> ToolRegistry.Dispatch --> ProxyHandler --> MCPClient.CallTool --> Subprocess stdin
                                                                             <-- Subprocess stdout
```

The plugin's response is unmarshalled as a standard MCP `ToolResult`. If the response doesn't match the expected format, it's wrapped as text content.

### Placeholder Handlers

Before a plugin is enabled (or after it's disabled), placeholder handlers are registered that return an informational error message: "Plugin tool X from plugin Y is registered but not yet connected."

---

## Event Bridge

Plugin subprocesses can publish events to the Hyperax nervous system by sending JSON-RPC notifications:

```json
{"jsonrpc":"2.0","method":"discord/message_received","params":{"content":"hello"}}
```

The EventBridge transforms these into NervousEvent publications:
- Method separator `/` replaced with `.` for event type (e.g., `discord.message_received`)
- Source tagged as `plugin:{pluginName}`
- Payload augmented with `_plugin` field
- Published on the EventBus for all subscribers

### Approval Gating

If a plugin has `approval_required: true` in its manifest and an ApprovalGate is configured, notifications from unapproved plugins are blocked. The approval flow:

1. User requests approval for the plugin
2. ApprovalGate generates an 8-character hex challenge code (10-minute TTL)
3. The code is delivered via the plugin's own channel (proving the connection works)
4. User enters the code in the Hyperax UI
5. Gate validates the code and marks the plugin as approved in config
6. Notifications now flow through the EventBridge

---

## Secret Injection

Plugin variables marked with `secret: true` have their values resolved through the secret store at subprocess launch time.

### Resolution Flow

1. Check OS environment variable (always takes precedence)
2. Retrieve value from config storage via `PluginConfigResolver.GetVar`
3. If the stored value is a secret reference (`secret:KEY:SCOPE`), resolve via `PluginConfigResolver.ResolveSecret`
4. Fall back to the variable's default value
5. Error if `required: true` and no value is found

### Config Key Format

Plugin variables are stored in the config system under:

```
plugin.{pluginName}.var.{variableName}
```

For secret variables, the config stores a reference (e.g., `secret:DISCORD_TOKEN:global`) rather than the actual value.

---

## Resource Auto-Creation

Plugins can declare resources in their manifest that are automatically created when the plugin is enabled and cleaned up when disabled or uninstalled.

### Supported Resource Types

| Type | Description |
|------|-------------|
| `cron_job` | Periodic task via the cron scheduler |

### Cron Job Resources

```yaml
resources:
  - type: cron_job
    name: poll-metrics
    config:
      schedule: "*/5 * * * *"
      job_type: tool
      payload:
        tool: plugin_prometheus-sensor_query
        arguments:
          query: "up"
```

Created resources are tracked in the install registry under `CreatedResources` so they can be cleaned up on uninstall.

---

## Plugin Catalog

Hyperax ships with an embedded plugin catalog (`catalog/plugins.yaml`) that lists verified plugins from the `runtime-dynamics` GitHub organization.

### Catalog Features

- **List** -- browse plugins filtered by category or verified status
- **Search** -- keyword search across name, description, and tags
- **Refresh** -- check GitHub for new plugins and latest release versions
- **Status annotation** -- catalog entries are enriched with installation status (installed, enabled, version)

### GitHub Release Distribution

Plugins are distributed as goreleaser archives via GitHub Releases:

```
install_plugin(source: "github.com/runtime-dynamics/hax-plugin-discord@v0.0.3")
```

The installation process:
1. Parse source into `owner/repo@version`
2. Fetch release metadata from GitHub API (or latest if no version specified)
3. Pattern-match release assets for current OS/architecture
4. Download and extract the archive (tar.gz or zip)
5. Read `hyperax-plugin.yaml` from the archive (or download as separate asset)
6. Override manifest version with release tag version (authoritative)
7. Copy extracted files to `pluginDir/{pluginName}/`
8. Load and optionally enable the plugin

### Install Registry

The install registry (`registry.json` in the plugin directory) persists installation records so plugins survive server restarts. Each record tracks:
- Plugin name
- Installation source (local path or GitHub reference)
- Manifest path
- Auto-created resource IDs for cleanup

---

## Dynamic Configuration

Plugin variables marked with `dynamic: true` support live updates without restart:

1. Variable updated via `configure_plugin` MCP tool
2. Value stored in config system
3. `notifications/configChanged` notification sent to running subprocess
4. Plugin handles the notification and updates its internal state

```json
{"jsonrpc":"2.0","method":"notifications/configChanged","params":{"variable":"POLL_INTERVAL","value":"30"}}
```

---

## Official Plugins

All official plugins are hosted under the `runtime-dynamics` GitHub organization with the `hax-plugin-` prefix.

| Plugin | Repo | Type | Integration | Status |
|--------|------|------|-------------|--------|
| Discord | hax-plugin-discord | service | channel | v0.0.3 |
| Slack | hax-plugin-slack | service | channel | Skeleton |
| Email | hax-plugin-email | service | channel | Skeleton |
| GitHub Tools | hax-plugin-github | mcp | tooling | Skeleton |
| Prometheus | hax-plugin-prometheus | mcp | sensor | Skeleton |
| Vault | hax-plugin-vault | mcp | secret_provider | Scaffold |
| 1Password | hax-plugin-1password | mcp | secret_provider | Scaffold |
| Approval Gate | hax-plugin-approval-gate | service | guard | v0.1.0 |
| Audit File | hax-plugin-audit-file | service | audit | v0.1.0 |
