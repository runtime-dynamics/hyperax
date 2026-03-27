# Hyperax

**Hyper Agentic eXchange — the Agent Operating System for AI-driven software teams.**

[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![MCP 2025-03-26](https://img.shields.io/badge/MCP-2025--03--26-6B4FBB)](https://modelcontextprotocol.io/)
[![GitHub release](https://img.shields.io/github/v/release/runtime-dynamics/hyperax)](https://github.com/runtime-dynamics/hyperax/releases)

A single Go binary with an embedded React dashboard that gives AI agents — and the humans working alongside them — the complete infrastructure they need: governed communication, code intelligence, project management, pipelines, secret management, and a plugin ecosystem for extending into any tool or service.

---

## What is Hyperax?

Hyperax ships as **one static binary, zero runtime dependencies, sub-100ms startup**. It operates in two complementary modes:

**As an MCP server** — Claude Code, Cursor, Copilot, and any MCP-capable coding assistant connects to Hyperax and gains 16 consolidated, action-dispatched tools covering code search, documentation, project management, pipelines, observability, configuration, secrets, governance, and more. Instead of each AI tool maintaining its own context about your codebase, Hyperax becomes the shared intelligence layer.

**As an agentic ecosystem** — Hyperax hosts a configurable organizational hierarchy of AI agents (Chief of Staff, Team Leads, Backend/Frontend Developers, QA Engineers, Security Analysts) that work autonomously on your projects. Human operators, Claude Code, Gemini CLI, and any other AI system can work side-by-side on the same project through governed communication channels with built-in safety controls. Each agent gets a dual-model architecture (a lightweight coordination model + a powerful execution model), role-based access control, and a tool-use bridge that lets it invoke any MCP tool autonomously.

---

## Quick Start

### 1. Build & Run

**Binary download:**

```bash
curl -L https://github.com/runtime-dynamics/hyperax/releases/latest/download/hyperax_$(uname -s)_$(uname -m).tar.gz | tar xz
./hyperax init        # interactive setup wizard
./hyperax serve       # start on :9090
```

**Build from source:**

```bash
git clone https://github.com/runtime-dynamics/hyperax
cd hyperax
./build.sh            # produces hyperax + hyperax-bridge binaries
./hyperax init
./hyperax serve
```

### 2. Connect Claude Code

**Add Hyperax as an MCP server** (gives Claude Code access to all 17 tools):

```bash
claude mcp add hyperax --transport sse http://localhost:9090/mcp/sse
```

That's it. Claude Code now has access to code search, project management, pipelines, observability, and everything else Hyperax provides.

### 3. Enable Channels (optional)

If you want Hyperax to push tasks, messages, and approval prompts directly into your Claude Code session, add the bridge:

```bash
claude mcp add hyperax-bridge -- /path/to/hyperax-bridge --url http://localhost:9090
```

Then start Claude Code with the channel active:

```bash
claude --channels server:hyperax-bridge
```

The bridge appears as a connected session in the Hyperax dashboard **Sessions** tab. See [Claude Code Channel Integration](#claude-code-channel-integration) for details.

### 4. Dashboard

Open `http://localhost:9090` for the full dashboard — agents, pipelines, tasks, chat, observability, and sessions.

---

## Two Modes of Operation

### MCP Server for AI Coding Tools

Connect Hyperax to Claude Code, Cursor, Copilot, or any MCP-capable client. Your AI assistant gains access to 16 action-dispatched tools:

| Tool | What it covers |
|------|---------------|
| `code` | Symbol search, AST-aware code intelligence, hybrid BM25 + vector search (Python, Go, Rust, TypeScript, C++, JavaScript) |
| `doc` | Documentation search, section retrieval, doc management |
| `project` | Hierarchical project plans, milestones, tasks, status workflows |
| `pipeline` | Define and run build/test/lint pipelines with parallel swimlane execution |
| `workspace` | Multi-workspace and git submodule management |
| `config` | Runtime configuration with typed variables |
| `secret` | Secret management with encrypted storage and external provider adapters |
| `agent` | Agent lifecycle, persona management, organizational hierarchy |
| `comm` | CommHub messaging, inbox management, trust-level routing |
| `memory` | Persistent agent memory, cross-session knowledge retention |
| `observability` | Session tracking, cost reporting, budget management, alerting |
| `governance` | ABAC policy, interjection control, audit trail |
| `plugin` | Plugin install, config, and lifecycle management |
| `refactor` | Transaction-safe symbol moves, code block operations, import management |
| `channel` | Claude Code session management, message push, task dispatch, permission relay |
| `audit` | Full audit log with structured event recording |

Workspace identity is git-native — a workspace IS a git repository, identified by its unique commit graph. Submodules are discovered automatically and indexed as separate workspaces.

### Agentic Ecosystem

Hyperax provides a pre-configured organizational hierarchy that you can customize to fit any team structure. Each agent:

- Uses **any LLM provider**: Anthropic, OpenAI, Google Gemini, Ollama, Azure OpenAI, or custom endpoints
- Has a **dual-model architecture**: a cheap coordination model for routing and a powerful work model for execution
- Is governed by **4-tier ABAC**: Observer (read-only) → Operator (create/execute) → Admin (config/plugins) → Chief of Staff (secrets/overrides)
- Can **invoke MCP tools autonomously** through the Tool-Use Bridge, supporting full agentic loops with parallel tool execution

Humans retain Chief of Staff override access — you can inject instructions, pause any agent, or take control at any point.

---

## Dashboard

<!-- screenshot placeholder -->

The embedded React dashboard provides full visibility into every subsystem: active agents and their current task, pipeline execution with live log streaming, the Kanban task board, CommHub message flows, cost heatmaps by provider, audit trails, and plugin management.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│          External Channels (AgentMail, IMAP/SMTP, Discord)       │
└───────────────────────────────┬──────────────────────────────────┘
                                │ TrustExternal (Context Sieve)
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│                  AI Agents / Human Operators                      │
│       (Claude, Cursor, Copilot, CLI — TrustAuthorized)           │
└───────────────────────────────┬──────────────────────────────────┘
                                │ MCP (SSE / Streamable HTTP)
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│                          Hyperax Core                             │
│                                                                   │
│  CommHub (Governance) → Agent Org → Pipeline Engine              │
│  Code Intelligence (Tree-sitter) → Hybrid Search (BM25 + Vec)   │
│  Project Management → Observability → Plugin Registry            │
│                                                                   │
│  Storage: SQLite (default) / PostgreSQL / MySQL                  │
│  Dashboard: React 18 + Vite (embedded via go:embed)              │
└──────────────────────────────────────────────────────────────────┘
```

Key subsystems:

- **CommHub** — Communication governance fabric with three trust levels (Internal/Authorized/External). A 5-layer Context Sieve strips prompt-injection attempts from all external input. Every message carries a trust level, trace ID, and return channel.
- **AgentMail** — Cross-instance communication via messenger adapters (AgentMail API, IMAP/SMTP, Slack, Discord). Each Hyperax instance is self-contained; cross-instance work delegation uses the same channels agents use to communicate with humans.
- **Pulse Engine** — Temporal orchestration for scheduled tasks, periodic agent heartbeats, and cron-driven pipeline runs.
- **Nervous System** — Real-time WebSocket event bus that connects all subsystems and streams live state to the dashboard.

Full architecture detail: [docs/Architecture.md](docs/Architecture.md)

---

## Plugin Ecosystem

Plugins are distributed as GitHub releases and installed with a single command:

```bash
# Install from the official plugin catalog
hyperax plugin install discord
hyperax plugin install prometheus

# Install directly from a GitHub release
hyperax plugin install github.com/runtime-dynamics/hax-plugin-vault@v0.1.0
```

Six integration categories:

| Category | Clearance | Purpose |
|----------|-----------|---------|
| `channel` | Admin | Communication adapters — Discord, Slack, Email |
| `tooling` | Operator | Additional MCP tools — GitHub, custom APIs |
| `secret_provider` | Chief of Staff | External secret backends — Vault, 1Password |
| `sensor` | Admin | Monitoring sources — Prometheus, custom metrics |
| `guard` | Admin | Approval gates — require human sign-off before sensitive operations |
| `audit` | Admin | Audit trail sinks — file, SIEM, custom targets |

Five plugin types: **MCP** (full tool/list discovery), **Service** (subprocess with manifest-defined tools), **WASM**, **HTTP**, and **Native**.

Official plugins: [github.com/runtime-dynamics](https://github.com/runtime-dynamics)
Build your own: [docs/PluginDevelopment.md](docs/PluginDevelopment.md)

---

## Claude Code Channel Integration

Hyperax includes a **channel bridge** that pushes events directly into a running Claude Code session using the [channel protocol](https://code.claude.com/docs/en/channels). This turns Hyperax into a task dispatcher that keeps Claude Code busy while you're away.

### What Channels Enable

- **Task dispatch** — Hyperax picks the next pending task and pushes it to Claude Code. Claude works on it, reports back, task gets marked complete. Repeat.
- **Remote chat** — send messages to your Claude Code session from the Hyperax dashboard on your phone
- **Permission relay** — when Claude Code needs to run a command and asks for approval, the prompt appears in the dashboard with Approve/Deny buttons

### Setup

The build produces two binaries:

```bash
./build.sh
# Output:
#   hyperax          — the server
#   hyperax-bridge   — the Claude Code channel bridge (stdio MCP server)
```

**Step 1** — Add Hyperax as an MCP server (tools only, no channel push):

```bash
claude mcp add hyperax --transport sse http://localhost:9090/mcp/sse
```

**Step 2** — Add the bridge for channel push support:

```bash
claude mcp add hyperax-bridge -- /path/to/hyperax-bridge --url http://localhost:9090
```

**Step 3** — Start Claude Code with the channel active:

```bash
claude --channels server:hyperax-bridge
```

The session appears in the dashboard **Sessions** tab immediately.

You can also generate or merge a `.mcp.json` file with `hyperax bridge-config`:

```bash
# Print the config snippet
hyperax bridge-config --url http://localhost:9090

# Write directly to .mcp.json
hyperax bridge-config --url http://my-server:9090 --name "backend-dev" --output .mcp.json
```

### How It Works

```
Claude Code ←── stdio (JSON-RPC) ──→ hyperax-bridge ←── HTTP ──→ Hyperax Server
                                                                       ↑
                                                            Dashboard (Sessions tab)
```

1. Hyperax enqueues an event (task, message, or notification)
2. The bridge polls every 2s and pushes it to Claude Code as a `<channel source="hyperax">` tag
3. Claude Code works on it, then calls the `reply` tool
4. The bridge forwards the reply to Hyperax — task status updates, response shows in chat

### Sessions Tab

The dashboard **Sessions** tab gives you:

- **Connected sessions** with live status indicators
- **Chat interface** per session — type a message, it gets pushed to Claude Code, reply appears in chat
- **Permission relay** — approve or deny tool-use prompts remotely
- **Task dispatch** — pick a pending task and push it to any session with one click

### Remote Access

Put Hyperax behind an nginx reverse proxy for mobile access:

```nginx
server {
    listen 443 ssl;
    server_name hyperax.yourdomain.com;

    location / {
        proxy_pass http://127.0.0.1:9090;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

Approve tasks, chat with Claude Code, and monitor progress from your phone.

---

## Enterprise-Grade Controls

**Interjection System (Andon Cord)** — Pull the cord at any time to halt all agents, switch to safe mode, or inject human-in-the-middle control into any active workflow. Agents respect interjections immediately and queue work until the cord is released.

**Guard Plugins** — Intercept sensitive tool calls before execution. Define approval policies that route dangerous operations (database migrations, secret rotation, production deploys) to a human approver via any channel plugin.

**Budget Governance** — Per-provider cost tracking with configurable thresholds. Automatic interjections fire when spending exceeds limits, preventing runaway agent loops from generating unexpected bills.

**Secret Management** — Built-in encrypted file provider (AES-256-GCM + Argon2id key derivation) with plugin adapters for HashiCorp Vault and 1Password. Secrets are injected into subprocesses at runtime and never stored in cleartext.

**Audit Trail** — Structured event recording for every tool call, agent action, config change, and pipeline execution. Route audit events to any sink via audit-category plugins.

**4-Tier ABAC** — Role-based access control enforced at every MCP tool call, with per-action clearance requirements. Roles are assigned per-agent and verifiable at runtime.

---

## Configuration

Hyperax uses a YAML bootstrap file (generated by `hyperax init`) with environment variable overrides:

```yaml
server:
  port: 9090
  host: "0.0.0.0"

storage:
  backend: "sqlite"          # sqlite | postgres | mysql
  path: "~/.hyperax/data.db"

docs:
  root: "docs/"
  external_dirs: []

telemetry:
  alert_interval: "60s"
```

All runtime config is manageable through the dashboard or via the `config` MCP tool.

---

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.25+, Chi router, goroutines |
| Frontend | React 18, TypeScript, Vite, shadcn/ui (embedded) |
| MCP transport | SSE + Streamable HTTP (MCP 2025-03-26 spec) |
| Storage | SQLite (default), PostgreSQL, MySQL |
| Code intelligence | Tree-sitter (CGO bindings) |
| Search | BM25/FTS5 + sqlite-vec/pgvector + ONNX embeddings |
| Auth | Ed25519 JWT, per-IP rate limiting |
| Distribution | Single static binary, Docker, goreleaser multi-arch |

---

## Contributing

Contributions are welcome. Before opening a PR, please read the [Coding Guidelines](docs/CodingGuidelines.md) — they cover project structure, patterns, testing requirements, and the refactoring workflow.

For larger changes, open an issue first to discuss the approach. This is especially important for changes to CommHub, the MCP server, or the storage layer, where subtle invariants need to be preserved.

---

## Links

- [Architecture](docs/Architecture.md) — system overview and subsystem detail
- [Plugin Development](docs/PluginDevelopment.md) — how to build and distribute plugins
- [Coding Guidelines](docs/CodingGuidelines.md) — development standards and patterns
- [GitHub Releases](https://github.com/runtime-dynamics/hyperax/releases) — pre-built binaries
- [runtime-dynamics org](https://github.com/runtime-dynamics) — official plugins and tooling

---

## License

Apache 2.0 — see [LICENSE](LICENSE).

Built by [Runtime Dynamics](https://github.com/runtime-dynamics).
