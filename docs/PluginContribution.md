# Hyperax Plugin Contribution Guide

A practical guide for developers who want to build plugins for the Hyperax platform. For architectural details, see [PluginSystem.md](PluginSystem.md).

---

## Quick Start

Building a Hyperax plugin involves three decisions:

1. **Plugin type** -- how your plugin executes
2. **Integration category** -- where your plugin slots into Hyperax
3. **Tools** -- what capabilities your plugin exposes

### Choosing Your Plugin Type

| Type | Use When | Communication | Tool Discovery |
|------|----------|---------------|----------------|
| **mcp** | On-demand tool calls (most common) | JSON-RPC 2.0 over stdin/stdout | Dynamic via `tools/list` |
| **service** | Persistent external connections | JSON-RPC 2.0 over stdin/stdout | Static from manifest |
| **wasm** | Sandboxed, untrusted code | WASM runtime (planned) | Static from manifest |
| **http** | Remote service integration | HTTP (planned) | Static from manifest |
| **native** | Compiled into Hyperax binary | Go interfaces (planned) | Compiled in |

For most plugins, choose **mcp**. Choose **service** if your plugin needs to maintain long-lived connections (like a Discord bot or Slack socket).

### Choosing Your Integration Category

| Category | Purpose | ABAC Clearance |
|----------|---------|----------------|
| **tooling** | Agent-facing tools (default) | Operator (tier 1) |
| **channel** | External messaging bridge | Admin (tier 2) |
| **secret_provider** | External secret backend | ChiefOfStaff (tier 3) |
| **sensor** | Monitoring data source | Admin (tier 2) |
| **guard** | Tool call approval gate | Admin (tier 2) |
| **audit** | Audit trail sink | Admin (tier 2) |

---

## Creating an MCP Plugin

MCP plugins are the most common type. They run as subprocesses and implement the Model Context Protocol for tool discovery and invocation.

### Step 1: Create a New Go Module

```bash
mkdir hax-plugin-mytools
cd hax-plugin-mytools
go mod init github.com/your-org/hax-plugin-mytools
```

### Step 2: Implement the MCP Server

Your binary must read JSON-RPC 2.0 requests from stdin and write responses to stdout. Each line is one complete JSON message.

At minimum, implement these methods:

- `initialize` -- return server capabilities
- `tools/list` -- return your tool definitions
- `tools/call` -- execute a tool and return results

```go
package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"
)

type Request struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      *int64          `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      *int64      `json:"id,omitempty"`
    Result  interface{} `json:"result,omitempty"`
    Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func main() {
    scanner := bufio.NewScanner(os.Stdin)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

    for scanner.Scan() {
        var req Request
        if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
            continue
        }

        // Notifications have no ID -- acknowledge silently.
        if req.ID == nil {
            continue
        }

        var resp Response
        resp.JSONRPC = "2.0"
        resp.ID = req.ID

        switch req.Method {
        case "initialize":
            resp.Result = handleInitialize()
        case "tools/list":
            resp.Result = handleListTools()
        case "tools/call":
            resp.Result = handleCallTool(req.Params)
        default:
            resp.Error = &RPCError{Code: -32601, Message: "method not found"}
        }

        data, _ := json.Marshal(resp)
        fmt.Fprintln(os.Stdout, string(data))
    }
}

func handleInitialize() map[string]interface{} {
    return map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities": map[string]interface{}{
            "tools": map[string]interface{}{},
        },
        "serverInfo": map[string]interface{}{
            "name":    "my-plugin",
            "version": "0.1.0",
        },
    }
}

func handleListTools() map[string]interface{} {
    return map[string]interface{}{
        "tools": []map[string]interface{}{
            {
                "name":        "greet",
                "description": "Generate a greeting message",
                "inputSchema": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "name": map[string]interface{}{
                            "type":        "string",
                            "description": "Name to greet",
                        },
                    },
                    "required": []string{"name"},
                },
            },
        },
    }
}

func handleCallTool(params json.RawMessage) map[string]interface{} {
    var call struct {
        Name      string          `json:"name"`
        Arguments json.RawMessage `json:"arguments"`
    }
    json.Unmarshal(params, &call)

    switch call.Name {
    case "greet":
        var args struct {
            Name string `json:"name"`
        }
        json.Unmarshal(call.Arguments, &args)
        return map[string]interface{}{
            "content": []map[string]interface{}{
                {"type": "text", "text": fmt.Sprintf("Hello, %s!", args.Name)},
            },
        }
    default:
        return map[string]interface{}{
            "content": []map[string]interface{}{
                {"type": "text", "text": "unknown tool"},
            },
            "isError": true,
        }
    }
}
```

### Step 3: Create the Manifest

Create `hyperax-plugin.yaml` in your project root:

```yaml
name: mytools
version: 0.1.0
type: mcp
description: Example MCP plugin with greeting tools
author: Your Name
license: Apache-2.0
source_repo: github.com/your-org/hax-plugin-mytools
integration: tooling
entrypoint: ./hax-plugin-mytools
min_hyperax_version: "1.0.0"
api_version: "1.0"

permissions:
  - tools:register

variables:
  - name: GREETING_PREFIX
    type: string
    required: false
    default: "Hello"
    description: Prefix for greeting messages

tools:
  - name: greet
    description: Generate a greeting message
    parameters:
      - name: name
        type: string
        required: true
        description: Name to greet
```

### Step 4: Build and Test Locally

```bash
# Build the binary
go build -o hax-plugin-mytools .

# Install locally (from the Hyperax UI or MCP tools)
# Use the plugin MCP tool:
#   install_plugin(source: "/path/to/hax-plugin-mytools")
```

After installation, the plugin directory will contain the binary and manifest. Enable it via the UI or MCP tools.

### Step 5: Verify

Once enabled, the plugin's tools appear in the MCP tool list as `plugin_mytools_greet`. Call it:

```json
{
  "tool": "plugin_mytools_greet",
  "arguments": {"name": "World"}
}
```

---

## Creating a Service Plugin

Service plugins are for long-running processes that maintain persistent external connections. The key differences from MCP plugins:

1. **No MCP handshake** -- no `initialize` or `tools/list` needed
2. **Static tools** -- tools come from the manifest, not dynamic discovery
3. **Persistent process** -- your binary starts and runs until stopped
4. **Event publishing** -- send JSON-RPC notifications to publish events

### Manifest Differences

```yaml
name: my-channel
version: 0.1.0
type: service                    # <-- service, not mcp
integration: channel
entrypoint: ./hax-plugin-my-channel
approval_required: true          # Recommended for channel plugins
```

### Communication Pattern

Your service plugin communicates over stdin/stdout using JSON-RPC 2.0, but only needs to handle:

**Inbound (Hyperax sends to your plugin via stdin):**
- `tools/call` -- tool invocations from Hyperax

**Outbound (your plugin sends to Hyperax via stdout):**
- JSON-RPC notifications -- events for the nervous system

### Publishing Events

Send JSON-RPC notifications (no `id` field) to stdout to publish events:

```go
type Notification struct {
    JSONRPC string      `json:"jsonrpc"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
}

func publishEvent(method string, data interface{}) {
    notif := Notification{
        JSONRPC: "2.0",
        Method:  method,
        Params:  data,
    }
    out, _ := json.Marshal(notif)
    fmt.Fprintln(os.Stdout, string(out))
}

// Example: publish a message received event
publishEvent("mychannel/message_received", map[string]interface{}{
    "channel": "general",
    "author":  "user123",
    "content": "Hello from external platform",
})
```

The EventBridge converts the method to an event type: `mychannel/message_received` becomes `mychannel.message_received`.

### Handling Dynamic Configuration

If your plugin has variables marked `dynamic: true`, handle `notifications/configChanged`:

```go
// In your message processing loop, handle notifications:
if req.ID == nil && req.Method == "notifications/configChanged" {
    var params struct {
        Variable string `json:"variable"`
        Value    string `json:"value"`
    }
    json.Unmarshal(req.Params, &params)
    // Update your internal config...
}
```

---

## Creating a Channel Plugin

Channel plugins bridge external messaging platforms into the Hyperax CommHub. They are service-type plugins with additional responsibilities.

### Requirements

1. **Bidirectional messaging** -- receive messages from the external platform and send messages via tool calls
2. **Trust level mapping** -- map external identities to Hyperax trust levels
3. **Owner verification** -- support the challenge-response approval flow

### Owner Verification Flow

For security, channel plugins should set `approval_required: true`. The verification flow:

1. User initiates approval in Hyperax UI
2. Hyperax generates an 8-character hex code
3. The code is sent to the user via your plugin's communication channel (e.g., Discord DM)
4. User enters the code in Hyperax UI
5. Plugin is marked as approved and events start flowing

Your plugin must provide a tool (e.g., `send_dm` or `send_message`) that Hyperax can use to deliver the challenge code.

### Typical Tools for a Channel Plugin

```yaml
tools:
  - name: send_message
    description: Send a message to a channel
    parameters:
      - name: channel_id
        type: string
        required: true
        description: Target channel identifier
      - name: content
        type: string
        required: true
        description: Message content

  - name: send_dm
    description: Send a direct message to a user
    parameters:
      - name: user_id
        type: string
        required: true
        description: Target user identifier
      - name: content
        type: string
        required: true
        description: Message content

  - name: list_channels
    description: List available channels
    parameters: []

  - name: read_history
    description: Read recent messages from a channel
    parameters:
      - name: channel_id
        type: string
        required: true
        description: Channel to read from
      - name: limit
        type: int
        required: false
        default: 50
        description: Maximum messages to return
```

---

## Creating a Guard Plugin

Guard plugins intercept tool calls and decide whether they should proceed, require approval, or be blocked.

### Required Tool

Your plugin must implement an `evaluate` tool that receives the tool call context and returns a decision:

```yaml
name: my-guard
type: service
integration: guard

tools:
  - name: evaluate
    description: Evaluate whether a tool call should proceed
    parameters:
      - name: tool_name
        type: string
        required: true
        description: The tool being called
      - name: arguments
        type: string
        required: true
        description: JSON-encoded tool arguments
      - name: caller_id
        type: string
        required: false
        description: Identity of the caller
      - name: context
        type: string
        required: false
        description: Additional context (persona, workspace, etc.)
```

### Decision Format

Return one of three decisions in your tool result:

```json
{
  "content": [{"type": "text", "text": "{\"decision\":\"approve\"}"}]
}
```

Possible decisions:
- `approve` -- allow the tool call to proceed
- `deny` -- block the tool call with a reason
- `queue` -- queue the tool call for human approval

```json
{
  "content": [{"type": "text", "text": "{\"decision\":\"deny\",\"reason\":\"Outside business hours\"}"}]
}
```

---

## Creating a Secret Provider Plugin

Secret provider plugins integrate external secret management systems (Vault, 1Password, AWS Secrets Manager).

### Required Tools

Your plugin must implement all of these tools:

| Tool | Description |
|------|-------------|
| `get_secret` | Retrieve a secret value by key and scope |
| `set_secret` | Create or update a secret |
| `delete_secret` | Remove a secret |
| `list_secrets` | List secret keys for a scope |
| `list_secret_entries` | List secret metadata (not values) |
| `get_access_scope` | Get access restriction for a secret |
| `update_access_scope` | Change access restriction |
| `rotate_secret` | Replace a secret value atomically |
| `health_check` | Verify the backend is reachable |

### Response Format

Each tool must return results as MCP ToolResult with JSON text content:

```json
{
  "content": [{"type": "text", "text": "{\"value\":\"my-secret-value\"}"}],
  "isError": false
}
```

For `get_secret`: return `{"value": "..."}` in the text content.
For `list_secrets`: return `{"keys": ["key1", "key2"]}`.
For `list_secret_entries`: return `{"entries": [...]}`.
For `rotate_secret`: return `{"old_value": "..."}`.

---

## Configuration Variables

### Typed Variables

Variables declared in the manifest are resolved at subprocess launch and injected as environment variables.

```yaml
variables:
  - name: API_KEY
    type: string
    required: true
    secret: true
    description: API key for external service

  - name: POLL_INTERVAL
    type: int
    required: false
    default: 60
    dynamic: true
    description: Polling interval in seconds

  - name: ENABLED_CHANNELS
    type: array_string
    required: false
    default: ["general"]
    description: Channels to monitor
```

### Variable Types

| Type | Go Equivalent | Description |
|------|--------------|-------------|
| `string` | `string` | Text value |
| `int` | `int` | Integer |
| `float` | `float64` | Floating point |
| `bool` | `bool` | Boolean |
| `array_string` | `[]string` | String array |
| `array_int` | `[]int` | Integer array |
| `array_float` | `[]float64` | Float array |

### Resolution Priority

1. OS environment variable (always wins)
2. Config store value (set via `configure_plugin`)
3. Secret resolution (if `secret: true` and value is a `secret:KEY:SCOPE` reference)
4. Default value from manifest
5. Error if `required: true` and no value found

### Secret Variables

For sensitive values, mark variables with `secret: true`. The value stored in config is a reference rather than the actual secret:

```
secret:DISCORD_TOKEN:global
```

At subprocess launch, the reference is resolved through the active secret provider and the actual value is injected into the environment.

### Dynamic Variables

Variables marked `dynamic: true` can be updated at runtime. When changed, Hyperax sends a `notifications/configChanged` JSON-RPC notification to the running subprocess, allowing your plugin to react without restart.

---

## Distribution

### goreleaser Setup

Create a `.goreleaser.yaml` in your project:

```yaml
project_name: hax-plugin-mytools

builds:
  - env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    binary: hax-plugin-mytools

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - hyperax-plugin.yaml
      - LICENSE
      - README.md

release:
  github:
    owner: your-org
    name: hax-plugin-mytools
```

### Build a Release

```bash
# Tag and release
git tag v0.1.0
git push origin v0.1.0
goreleaser release
```

### Install from GitHub

Users install your plugin by source reference:

```
install_plugin(source: "github.com/your-org/hax-plugin-mytools@v0.1.0")
```

Hyperax will:
1. Fetch the release from GitHub
2. Download the correct platform archive (matches `runtime.GOOS`/`runtime.GOARCH`)
3. Extract the binary and manifest
4. Load the plugin

If no version is specified, the latest release is used.

### Asset Name Matching

Hyperax tries these naming patterns when locating your platform archive:

```
{repo}_{version}_{os}_{arch}.tar.gz       # Default goreleaser format
{repo}_{version}_{os}_{arch}.zip
{repo}-{os}-{arch}.tar.gz                  # Dash-separated without version
{repo}-{version}-{os}-{arch}.tar.gz        # Dash-separated with version
```

Falls back to substring matching on `{os}_{arch}` or `{os}-{arch}`.

### Plugin Catalog Submission

To have your plugin listed in the official Hyperax catalog:

1. Host under the `runtime-dynamics` GitHub organization (or request inclusion)
2. Use the `hax-plugin-` repository name prefix
3. Include a valid `hyperax-plugin.yaml` in your releases
4. Create GitHub releases with goreleaser-compatible archives
5. The catalog refresh discovers repos matching the prefix pattern

---

## Resource Auto-Creation

Plugins can declare resources that Hyperax automatically provisions when the plugin is enabled:

### Cron Jobs

```yaml
resources:
  - type: cron_job
    name: poll-data
    config:
      schedule: "*/5 * * * *"          # Standard cron expression
      job_type: tool                    # Executes a tool call
      payload:
        tool: plugin_mytools_poll       # Qualified tool name
        arguments:
          source: "default"
```

Resources are tracked in the install registry and automatically cleaned up on disable/uninstall. The cron job name follows the pattern `plugin:{pluginName}:{resourceName}`.

---

## Error Handling

### Tool Call Errors

Return errors using the MCP ToolResult format with `isError: true`:

```json
{
  "content": [{"type": "text", "text": "Failed to connect: connection refused"}],
  "isError": true
}
```

### JSON-RPC Errors

For protocol-level errors, return a JSON-RPC error response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "Invalid params: missing required field 'name'"
  }
}
```

Standard JSON-RPC error codes:
- `-32700` -- Parse error
- `-32600` -- Invalid request
- `-32601` -- Method not found
- `-32602` -- Invalid params
- `-32603` -- Internal error

### Subprocess Crashes

If your plugin crashes, Hyperax will:
1. Log the crash with exit error details
2. Publish a `plugin.error` event
3. Attempt automatic restart (up to 3 times with linear backoff)
4. Leave the plugin in error state if all restarts fail

Write to stderr for log output. Hyperax captures stderr and logs it at Info level, tagged with your plugin name.

---

## Testing

### Manual Testing

```bash
# Build your plugin
go build -o hax-plugin-mytools .

# Test the MCP protocol manually:
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}' | ./hax-plugin-mytools

# Should output an initialize response, then you can send tools/list:
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./hax-plugin-mytools
```

### Integration Testing

Install your plugin into a running Hyperax instance:

1. Start Hyperax in dev mode
2. Install from local path: `install_plugin(source: "/path/to/hax-plugin-mytools")`
3. Enable: `enable_plugin(name: "mytools")`
4. Call your tools: `plugin_mytools_greet(name: "test")`
5. Check logs for errors
6. Disable and uninstall to test cleanup

---

## Reference: Existing Plugins

Study these plugins as implementation references. All are hosted under the `runtime-dynamics` GitHub organization.

### hax-plugin-discord (channel / service)

Bi-directional Discord integration. Maintains a persistent gateway connection. Publishes message events and provides tools for sending messages, creating threads, reading history, and managing reactions.

**Key patterns:**
- Long-lived goroutine for Discord gateway connection
- Publishes `discord/message_received` notifications
- Owner verification via Discord DM
- Config: `DISCORD_TOKEN` (secret), `GUILD_ID`, `CHANNEL_IDS`

**Repo:** `github.com/runtime-dynamics/hax-plugin-discord`

### hax-plugin-github (tooling / mcp)

Agent-facing tools for GitHub operations. Standard MCP plugin with `initialize` / `tools/list` / `tools/call`.

**Tools:** create_issue, list_issues, create_pr, list_prs, get_repo_info

**Repo:** `github.com/runtime-dynamics/hax-plugin-github`

### hax-plugin-prometheus (sensor / mcp)

Prometheus metrics query plugin. Provides tools to query metrics, list targets, and list alerts.

**Tools:** query, query_range, list_targets, list_alerts

**Repo:** `github.com/runtime-dynamics/hax-plugin-prometheus`

### hax-plugin-vault (secret_provider / mcp)

HashiCorp Vault integration. Implements the full secret provider tool set for KV v2 with AppRole and token authentication.

**Repo:** `github.com/runtime-dynamics/hax-plugin-vault`

### hax-plugin-approval-gate (guard / service)

Configurable approval gate with rule evaluation. Supports action-based, tool-based, persona-based, and time-based rules.

**Key patterns:**
- Service type (no MCP handshake)
- `evaluate` tool returns approve/deny/queue decisions
- Rules loaded from config variables

**Repo:** `github.com/runtime-dynamics/hax-plugin-approval-gate`

### hax-plugin-audit-file (audit / service)

File-based audit log writer. Receives audit events and writes rotated JSONL files.

**Key patterns:**
- Service type
- `write_audit_event` tool writes structured events
- File rotation based on size/time

**Repo:** `github.com/runtime-dynamics/hax-plugin-audit-file`
