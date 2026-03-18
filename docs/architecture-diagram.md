# Hyperax Architecture Diagrams

## 1. System Overview

```mermaid
graph TB
    subgraph External["External Channels"]
        AMAIL[AgentMail API]
        IMAP[IMAP/SMTP]
        SLACK[Slack]
        DISCORD[Discord]
        WEBHOOK[Webhooks]
    end

    subgraph Clients["AI Agent Clients / Users"]
        CLAUDE[Claude Code]
        CURSOR[Cursor]
        COPILOT[Copilot]
        DASH[Web Dashboard]
        CLI[CLI]
    end

    subgraph Core["Hyperax Core (Single Binary)"]

        subgraph Transport["Transport Layer"]
            MCP_SSE["MCP SSE\n/mcp/sse"]
            MCP_HTTP["MCP Streamable HTTP\nPOST /mcp"]
            WS["WebSocket\n/ws/events"]
            REST["REST API\n/api/v1/"]
        end

        subgraph Security["Security Layer"]
            AUTH["Auth\nEd25519 JWT"]
            ABAC["ABAC\n4-Tier Clearance"]
            GUARD["Guard System\nAction Approval"]
            RATE["Rate Limiter\nPer-IP"]
        end

        subgraph CommLayer["Communication Governance"]
            COMMHUB["CommHub"]
            SIEVE["Context Sieve\n5 Layers"]
            INBOXES["Agent Inboxes\nPer-Agent Channels"]
            OVERFLOW["Overflow\nPersistence"]
            HIERARCHY["Hierarchy\nEnforcement"]
        end

        subgraph MCP_Server["MCP Server (16 Tools)"]
            REGISTRY["Tool Registry"]
            DISPATCH["Action Dispatch"]
            HANDLERS["Handler Domains\nworkspace | project | doc | pipeline\naudit | code | config | agent\nobservability | memory | secret\nrefactor | event | comm | governance | plugin"]
        end

        subgraph Agentic["Agentic Execution"]
            BRIDGE["Tool-Use Bridge"]
            RESOLVER["Tool Resolver\nABAC-Filtered"]
            EXECUTOR["Executor Loop\nMax 100 iterations"]
            ADAPTERS["Provider Adapters\nAnthropic | OpenAI | Google\nBedrock | Azure | Ollama"]
        end

        subgraph Intelligence["Code Intelligence"]
            INDEXER["AST Indexer\nTree-sitter"]
            SEARCH["Hybrid Search\nBM25 + Vector + RRF"]
            SENTINEL["File Sentinel\nfsnotify"]
            CACHE["Symbol Cache"]
        end

        subgraph Temporal["Temporal Orchestration"]
            PULSE["Pulse Engine\nCadence Scheduler"]
            CRON["Cron System"]
            WATCHDOG["Watchdog\nFail-Closed"]
            SENSOR["Sensor Manager"]
        end

        subgraph Safety["Safety Systems"]
            INTERJECT["Interjection Manager\nAndon Cord"]
            BUDGET["Budget Monitor\nPer-Provider"]
            ALERTS["Alert Evaluator"]
        end

        subgraph Nervous["Nervous System"]
            EVENTBUS["EventBus\nLamport Clock"]
            RINGBUF["Ring Buffer\n10K Events"]
            TRANSPORT_S["Transport Stream\nEphemeral"]
            DOMAIN_S["Domain Stream\nPersisted"]
        end

        subgraph Lifecycle["Agent Lifecycle"]
            FSM["FSM Manager\n9 States"]
            ONBOARD["Onboarding\n4-Step Sequence"]
            CHECKPOINT["Checkpointing"]
            REHYDRATE["Rehydration"]
        end

        subgraph Plugins["Plugin System"]
            PLUGMGR["Plugin Manager"]
            SUBPROCESS["MCP Subprocess"]
            BRIDGES["Bridges\nSecret | Guard | Audit"]
            CATALOG["Plugin Catalog"]
        end

        subgraph Context["Context Economics"]
            COMPACTOR["Conversation\nCompactor"]
            HINTS["Tool Hints\nPrompt Queue"]
            SUPERCTX["Super Context\nAgent File Gen"]
        end

        subgraph Storage["Storage Layer"]
            STORE["Store Facade\n30 Repos"]
            SQLITE[(SQLite)]
            PG[(PostgreSQL)]
            MYSQL[(MySQL)]
            MIGRATE["Migration Runner\n43 Migrations"]
        end

        subgraph Frontend["Embedded Frontend"]
            REACT["React 18 + Vite\ngo:embed"]
        end
    end

    %% Client connections
    CLAUDE & CURSOR & COPILOT -->|MCP JSON-RPC| MCP_SSE & MCP_HTTP
    DASH -->|WebSocket + MCP| WS & MCP_HTTP
    CLI -->|REST| REST

    %% External channels
    AMAIL & IMAP & SLACK & DISCORD & WEBHOOK -->|TrustExternal| SIEVE

    %% Transport → Security → MCP
    MCP_SSE & MCP_HTTP --> AUTH
    AUTH --> ABAC
    ABAC --> GUARD
    GUARD --> DISPATCH

    %% MCP dispatch
    DISPATCH --> REGISTRY
    REGISTRY --> HANDLERS

    %% CommHub flow
    SIEVE --> COMMHUB
    COMMHUB --> HIERARCHY
    COMMHUB --> INBOXES
    INBOXES -->|Full| OVERFLOW
    COMMHUB -->|Envelope| BRIDGE

    %% Agentic loop
    BRIDGE --> RESOLVER
    RESOLVER --> EXECUTOR
    EXECUTOR --> ADAPTERS
    EXECUTOR -->|Tool Call| DISPATCH
    ADAPTERS -->|LLM API| LLM_PROVIDERS["LLM Providers\nAnthropic | OpenAI | Google\nAWS Bedrock | Ollama"]

    %% Intelligence
    SENTINEL --> INDEXER
    INDEXER --> SEARCH
    SEARCH --> CACHE

    %% Temporal
    PULSE --> COMMHUB
    PULSE --> WATCHDOG
    CRON --> PULSE
    SENSOR --> PULSE

    %% Safety
    WATCHDOG -->|Stall| INTERJECT
    BUDGET -->|Threshold| INTERJECT
    ALERTS --> INTERJECT
    INTERJECT -->|Safe Mode| COMMHUB
    INTERJECT -->|Suspend| PULSE

    %% Nervous System
    EVENTBUS --> RINGBUF
    RINGBUF --> WS
    EVENTBUS --> TRANSPORT_S
    TRANSPORT_S -->|Promote| DOMAIN_S

    %% Lifecycle
    FSM --> ONBOARD
    ONBOARD --> COMMHUB
    FSM --> CHECKPOINT
    CHECKPOINT --> REHYDRATE

    %% Plugins
    PLUGMGR --> SUBPROCESS
    PLUGMGR --> BRIDGES
    PLUGMGR --> CATALOG
    SUBPROCESS -->|JSON-RPC| REGISTRY

    %% Storage
    STORE --> SQLITE
    STORE --> PG
    STORE --> MYSQL
    MIGRATE --> STORE

    %% Frontend
    REACT -->|mcpCall| MCP_HTTP
    REACT -->|Events| WS

    %% Event publishing (all subsystems → EventBus)
    COMMHUB -.->|Events| EVENTBUS
    PULSE -.->|Events| EVENTBUS
    INTERJECT -.->|Events| EVENTBUS
    BRIDGE -.->|Events| EVENTBUS
    FSM -.->|Events| EVENTBUS
    SENTINEL -.->|fs.*| EVENTBUS

    classDef external fill:#f9f,stroke:#333
    classDef client fill:#bbf,stroke:#333
    classDef security fill:#fbb,stroke:#333
    classDef storage fill:#bfb,stroke:#333

    class AMAIL,IMAP,SLACK,DISCORD,WEBHOOK external
    class CLAUDE,CURSOR,COPILOT,DASH,CLI client
    class AUTH,ABAC,GUARD,RATE security
    class SQLITE,PG,MYSQL storage
```

## 2. MCP Request Lifecycle

```mermaid
sequenceDiagram
    participant C as AI Client
    participant T as MCP Transport
    participant A as Auth/ABAC
    participant G as Guard
    participant R as Tool Registry
    participant H as Handler
    participant S as Store
    participant E as EventBus

    C->>T: POST /mcp (JSON-RPC 2.0)
    T->>T: Parse JSON-RPC method

    alt initialize
        T-->>C: Server capabilities + tool list
    else tools/list
        T->>R: ListTools(clearanceLevel)
        R-->>T: Filtered tool schemas
        T-->>C: Tool list (ABAC-filtered)
    else tools/call
        T->>A: Authenticate (Bearer token)
        A->>A: Validate MCP token
        A->>A: Check clearance >= tool.minLevel
        alt Insufficient clearance
            A-->>C: Error -32003 (Forbidden)
        end
        A->>G: Check guard rules
        alt Action requires approval
            G->>E: Publish pending_action
            G-->>C: "Action pending approval"
        end
        G->>R: Dispatch(toolName, params)
        R->>H: Execute handler
        H->>S: Database operations
        S-->>H: Results
        H-->>R: ToolResult
        R->>E: Publish mcp.response
        R-->>T: JSON-RPC response
        T-->>C: Tool result
    end
```

## 3. Chat Completion & Tool-Use Loop

```mermaid
sequenceDiagram
    participant U as User/Dashboard
    participant API as Chat API
    participant CH as CommHub
    participant SV as Context Sieve
    participant B as Tool-Use Bridge
    participant R as Resolver
    participant LLM as LLM Provider
    participant MCP as MCP Registry
    participant T as Telemetry
    participant E as EventBus

    U->>API: POST /api/v1/chat
    API->>CH: Send(MessageEnvelope, TrustAuthorized)
    CH->>SV: Apply sieve layers

    Note over SV: 1. Pattern Filter<br/>2. Length Limiter<br/>3. Content Classifier<br/>4. Metadata Stripper<br/>5. Structural Sifter

    SV-->>CH: Sanitized envelope
    CH->>CH: Check zombie state
    CH->>CH: Proactive memory recall (50ms budget)
    CH->>CH: Inject tool hints
    CH->>CH: Deliver to agent inbox

    API->>B: TriggerCompletion (async)
    B->>T: StartSession(provider, model)
    B->>R: ResolveTools(clearance, scopes)
    R-->>B: ABAC-filtered tool list

    loop Tool-Use Loop (max 100 iterations)
        B->>LLM: ChatCompletion(messages, tools)
        LLM-->>B: Response (content + tool_calls)

        alt StopReason = "end_turn"
            B->>T: EndSession(usage, cost)
            B->>E: Publish completion event
            B-->>API: Final response
        else StopReason = "tool_use"
            loop For each tool call
                B->>MCP: Dispatch(toolName, params)
                MCP-->>B: ToolResult
                B->>T: RecordToolCall(name, duration)
            end
            B->>B: FormatTurnMessages (provider-specific)
            Note over B: Append assistant + tool results<br/>to conversation history
        end
    end

    API->>U: Stream response via SSE
```

## 4. Communication Governance (CommHub)

```mermaid
graph TB
    subgraph Sources["Message Sources"]
        USER["User\nTrustAuthorized"]
        AGENT["Agent\nTrustInternal"]
        EXT["External Channel\nTrustExternal"]
    end

    subgraph Sieve["Context Sieve (5 Layers)"]
        L1["1. Pattern Filter\nRegex injection stripping"]
        L2["2. Length Limiter\nMax 4096 chars"]
        L3["3. Content Classifier\nSuspicious content flagging"]
        L4["4. Metadata Stripper\nHidden Unicode removal"]
        L5["5. Structural Sifter\nSchema validation"]
    end

    subgraph Enrichment["Envelope Enrichment"]
        MEM_RECALL["Proactive Memory Recall\n50ms budget"]
        TOOL_HINTS["Tool Hint Injection\nTop 3-5 relevant tools"]
        TRUST_TAG["Trust Level Tagging"]
    end

    subgraph Dispatch["Dispatch"]
        HIERARCHY_CHECK["Hierarchy Check\nreports_to / can_talk_to"]
        ZOMBIE_CHECK["Zombie Detection"]
        INBOX_DELIVER["Inbox Delivery\nBuffered Go Channels"]
        OVERFLOW_PERSIST["Overflow Persistence\nDatabase Fallback"]
    end

    subgraph AgentInboxes["Agent Inboxes"]
        URGENT["Priority: Urgent\nCapacity: 8"]
        STANDARD["Priority: Standard\nCapacity: 64"]
    end

    EXT --> L1 --> L2 --> L3 --> L4 --> L5
    USER --> L4
    AGENT -->|Recursive sieve\nif trust_lineage=External| L1

    L5 --> MEM_RECALL
    L4 --> MEM_RECALL
    MEM_RECALL --> TOOL_HINTS
    TOOL_HINTS --> TRUST_TAG

    TRUST_TAG --> HIERARCHY_CHECK
    HIERARCHY_CHECK --> ZOMBIE_CHECK
    ZOMBIE_CHECK -->|Zombie detected| REHYDRATE["Trigger Rehydration"]
    ZOMBIE_CHECK -->|Healthy| INBOX_DELIVER
    INBOX_DELIVER -->|Channel full| OVERFLOW_PERSIST
    INBOX_DELIVER --> URGENT
    INBOX_DELIVER --> STANDARD
```

## 5. Agent Lifecycle (FSM)

```mermaid
stateDiagram-v2
    [*] --> Pending: CreateAgent()

    Pending --> Onboarding: OnboardAgent()
    note right of Onboarding
        1. Identity Definition
        2. Relationship Mapping
        3. Context Hydration
        4. Task Assignment
    end note

    Onboarding --> Active: Hydration complete

    Active --> Suspended: Voluntary pause / budget / stall
    Active --> Halted: Interjection (Andon Cord)
    Active --> Draining: Graceful shutdown
    Active --> Error: Process crash

    Suspended --> Active: resume()
    Suspended --> Draining: Decommission

    Halted --> Active: resolve() (clearance check)

    Error --> Recovering: Host comes back
    Recovering --> Rehydrating: No duplicate found
    Recovering --> Decommissioned: Duplicate running elsewhere
    Rehydrating --> Active: Checkpoint restored

    Draining --> Decommissioned: Drain complete

    Decommissioned --> [*]

    note right of Error
        Heartbeat expired
        Process crashed
    end note

    note right of Halted
        Only PriorityUrgent
        messages delivered
    end note
```

## 6. Storage Architecture

```mermaid
graph TB
    subgraph Facade["Store Facade"]
        STORE["storage.Store"]
    end

    subgraph Repos["30 Repository Interfaces (internal/repo/)"]
        direction LR
        subgraph Workspace["Workspace & Code"]
            R1[WorkspaceRepo]
            R2[SymbolRepo]
            R3[SearchRepo]
            R4[GitRepo]
            R5[VectorRepo]
        end
        subgraph Projects["Projects & Execution"]
            R6[ProjectRepo]
            R7[PipelineRepo]
            R8[WorkflowRepo]
            R9[SpecRepo]
            R10[CronRepo]
        end
        subgraph Agents["Agents & Communication"]
            R11[AgentRepo]
            R12[LifecycleRepo]
            R13[SessionRepo]
            R14[WorkQueueRepo]
            R15[CommHubRepo]
            R16[AgentMailRepo]
        end
        subgraph Config_Sec["Config & Security"]
            R17[ConfigRepo]
            R18[SecretRepo]
            R19[MCPTokenRepo]
            R20[DelegationRepo]
            R21[PluginRepo]
        end
        subgraph Observability_Repos["Observability"]
            R22[TelemetryRepo]
            R23[BudgetRepo]
            R24[MetricsRepo]
            R25[AuditRepo]
        end
        subgraph Other["Other"]
            R26[MemoryRepo]
            R27[CheckpointRepo]
            R28[ExternalDocRepo]
            R29[NervousRepo]
            R30[ProvidersRepo]
        end
    end

    subgraph Backends["Backend Implementations"]
        SQLITE["SQLite\n(Default)"]
        PG["PostgreSQL"]
        MYSQL["MySQL"]
    end

    subgraph Migration["Migration System"]
        RUNNER["migrate.go\nBackend-agnostic runner"]
        M_SQLITE["sqlite/migrations/\n43 files"]
        M_PG["postgres/migrations/\n43 files"]
        M_MYSQL["mysql/migrations/\n43 files"]
    end

    STORE --> Repos
    Repos --> SQLITE & PG & MYSQL
    RUNNER --> M_SQLITE & M_PG & M_MYSQL
```

## 7. Security Model (ABAC + Guard)

```mermaid
graph TB
    subgraph Request["Incoming MCP Request"]
        TOKEN["Bearer Token"]
    end

    subgraph AuthChain["Authentication Chain"]
        VALIDATE["Validate MCP Token\n(MCPTokenRepo)"]
        EXTRACT["Extract Claims\npersona_id, clearance, scopes"]
        JWT_ISSUE["JWT Exchange\n(WebSocket only)"]
    end

    subgraph ABAC_Check["ABAC Enforcement"]
        TIER0["Tier 0: Observer\nRead-only tools"]
        TIER1["Tier 1: Operator\nCreate/Update, Pipelines"]
        TIER2["Tier 2: Admin\nConfig, Providers, Plugins"]
        TIER3["Tier 3: Chief of Staff\nSecrets, Safety Overrides"]
    end

    subgraph Guard_Check["Guard System"]
        APPROVE_WRITES["ApproveWrites Guard\nBlocks write/delete 5min"]
        PLUGIN_GUARDS["Plugin Guards\nCustom evaluators"]
        APPROVAL_QUEUE["Approval Queue\nPending actions"]
    end

    subgraph Dispatch_Layer["Tool Dispatch"]
        TOOL["Execute Tool Handler"]
    end

    TOKEN --> VALIDATE --> EXTRACT
    EXTRACT --> ABAC_Check

    ABAC_Check -->|clearance >= minLevel| Guard_Check
    ABAC_Check -->|clearance < minLevel| REJECT["Error -32003\nForbidden"]

    Guard_Check -->|Approved or no guard| TOOL
    Guard_Check -->|Requires approval| APPROVAL_QUEUE
    APPROVAL_QUEUE -->|Human approves| TOOL
```

## 8. Nervous System (Event Architecture)

```mermaid
graph LR
    subgraph Publishers["Event Publishers"]
        PUB_COMM["CommHub\ncomm.*"]
        PUB_PULSE["Pulse Engine\npulse.*"]
        PUB_INTER["Interjections\ninterject.*"]
        PUB_PIPE["Pipelines\npipeline.*"]
        PUB_MEM["Memory\nmemory.*"]
        PUB_MCP["MCP Server\nmcp.*"]
        PUB_MAIL["AgentMail\nagentmail.*"]
        PUB_LIFE["Lifecycle\nlifecycle.*"]
        PUB_FS["Sentinel\nfs.*"]
        PUB_TEL["Telemetry\ntelemetry.*"]
    end

    subgraph Bus["EventBus"]
        LAMPORT["Lamport Clock\nCausal Ordering"]
        FANOUT["Fan-Out\nPer-Subscriber Channels"]
        FILTER["Subscription Filters\nType + Scope"]
    end

    subgraph Streams["Dual Streams"]
        TRANSPORT["Transport Stream\nEphemeral, Lossy\nGo Channels"]
        DOMAIN["Domain Stream\nPersisted, Auditable\nDatabase"]
    end

    subgraph Consumers["Event Consumers"]
        RING["Ring Buffer\n10K Events\nLate-Join Replay"]
        WS_CONN["WebSocket Clients\n/ws/events"]
        EVT_HANDLERS["Event Handlers\nDeclarative Rules"]
        AUDIT_SINK["Audit Sink\nJSONL / Kafka"]
        INDEXER_SUB["Index Watcher\nIncremental Reindex"]
        CONFLICT["Conflict Detector\nfs.* → Agent Files"]
    end

    Publishers --> Bus
    Bus --> TRANSPORT
    TRANSPORT -->|Promote| DOMAIN
    TRANSPORT --> RING --> WS_CONN
    TRANSPORT --> EVT_HANDLERS
    TRANSPORT --> INDEXER_SUB
    TRANSPORT --> CONFLICT
    DOMAIN --> AUDIT_SINK
```

## 9. Plugin System

```mermaid
graph TB
    subgraph Lifecycle["Plugin Lifecycle"]
        DISCOVER["Discover\nScan plugin_dir"]
        LOAD["Load\nRestore from registry"]
        ENABLE["Enable\nStart subprocess"]
        DISABLE["Disable\nStop subprocess"]
        UNINSTALL["Uninstall\nCleanup resources"]
    end

    subgraph Types["Plugin Types"]
        MCP_PLUGIN["MCP\nJSON-RPC subprocess"]
        SERVICE["Service\nLong-running process"]
        WASM["WASM\nWebAssembly module"]
        HTTP_PLUGIN["HTTP\nRemote service"]
        NATIVE["Native\nCompiled Go"]
    end

    subgraph Categories["Integration Categories"]
        CHANNEL["channel\nDiscord, Slack, Email"]
        TOOLING["tooling\nGitHub, AWS"]
        SECRET_PROV["secret_provider\nVault, 1Password"]
        SENSOR_CAT["sensor\nPrometheus"]
        GUARD_CAT["guard\nApproval Gates"]
        AUDIT_CAT["audit\nEvent Logging"]
    end

    subgraph Bridges_Detail["Bridge System"]
        SEC_BRIDGE["SecretRegistryBridge\nRegister/Unregister providers"]
        GUARD_BRIDGE["GuardBridge\nCustom evaluators"]
        AUDIT_BRIDGE["AuditBridge\nExternal writers"]
        EVENT_BRIDGE["EventBridge\nApproval gate"]
    end

    subgraph Distribution["Distribution"]
        GITHUB["GitHub Releases\ngoreleaser archives"]
        CATALOG_SRC["Embedded Catalog\ncatalog/plugins.yaml"]
        MANIFEST["Plugin Manifest\nhyperax-plugin.yaml"]
    end

    DISCOVER --> LOAD --> ENABLE
    ENABLE --> DISABLE --> UNINSTALL

    ENABLE --> MCP_PLUGIN & SERVICE
    MCP_PLUGIN --> SEC_BRIDGE & GUARD_BRIDGE & AUDIT_BRIDGE
    SERVICE --> EVENT_BRIDGE

    GITHUB --> DISCOVER
    CATALOG_SRC --> DISCOVER
```

## 10. Deployment Architecture

```mermaid
graph TB
    subgraph Build["Build Pipeline"]
        GO_BUILD["Go Build\ngo:embed React UI"]
        GORELEASER["goreleaser\namd64 + arm64"]
        DOCKER["Docker\nMulti-stage\nNode → Go → distroless"]
        ACTIONS["GitHub Actions\nLint → Test → Build → Release"]
    end

    subgraph Binary["Single Binary"]
        CLI_CMDS["CLI Commands"]
        SERVE["serve\nStart HTTP server"]
        INIT["init\nBootstrap wizard"]
        VERSION["version\nBuild info"]
        DOCTOR["doctor\nHealth check"]
    end

    subgraph Config_Boot["Configuration"]
        BOOTSTRAP["hyperax.yaml\n4 fields only"]
        RUNTIME["Database Config\nScoped key-value pairs"]
    end

    subgraph Endpoints["Network"]
        HTTP_PORT["HTTP :9090\n(configurable)"]
        TLS_PORT["HTTPS\nOptional TLS"]
    end

    GO_BUILD --> GORELEASER --> DOCKER
    ACTIONS --> GORELEASER

    CLI_CMDS --> SERVE & INIT & VERSION & DOCTOR
    SERVE --> BOOTSTRAP --> RUNTIME
    SERVE --> HTTP_PORT
    SERVE -.->|cert + key| TLS_PORT
```

## 11. Data Flow: Pipeline Execution

```mermaid
sequenceDiagram
    participant U as User/Agent
    participant H as Pipeline Handler
    participant E as Pipeline Executor
    participant R as LocalStepRunner
    participant S as Store
    participant EB as EventBus
    participant WS as WebSocket

    U->>H: MCP: pipeline(action: "run", pipeline_id)
    H->>S: GetPipeline(id)
    S-->>H: Pipeline definition

    H->>E: Execute(pipeline, params)
    E->>S: CreateJob(status: "running")
    E->>EB: Publish pipeline.start

    loop For each execution level (topological sort)
        par Parallel swimlanes
            E->>R: RunStep(step)
            R->>R: exec.Command with timeout
            R->>EB: Publish pipeline.log (streaming)
            EB->>WS: Real-time log lines

            alt Step succeeds
                R-->>E: StepResult(success)
                E->>S: UpdateStepStatus("completed")
            else Step fails
                R-->>E: StepResult(error)
                E->>S: UpdateStepStatus("failed")
                E->>E: Cancel remaining steps
            end
        end
    end

    E->>S: UpdateJob(status: "completed" | "failed")
    E->>EB: Publish pipeline.complete
    E-->>H: JobResult
    H-->>U: Tool result with job summary
```

## 12. Interjection System (Andon Cord)

```mermaid
graph TB
    subgraph Triggers["Halt Triggers"]
        HUMAN["Human\nDashboard button"]
        AGENT_HALT["Agent\nCVE detected, audit fail"]
        WEBHOOK_HALT["Webhook\nWiz/Snyk/Prisma alert"]
        WATCHDOG_HALT["Watchdog\nPulse Engine stalled"]
        BUDGET_HALT["Budget\nSpend threshold exceeded"]
    end

    subgraph Scopes["Blast Radius"]
        SCOPE_PIPE["Pipeline Scope\nCancel specific job"]
        SCOPE_WS["Workspace Scope\nFreeze workspace agents"]
        SCOPE_GLOBAL["Global Scope\nFull organizational halt"]
    end

    subgraph SafeMode["Safe Mode Effects"]
        PIPE_SUSPEND["Pipeline Suspension\nCancel via context"]
        COMM_BLOCK["CommHub Blockade\nOnly PriorityUrgent"]
        PULSE_DEFER["Pulse Suspension\nDefer cadences"]
        RECALL_OFF["Memory Recall\nDisabled"]
    end

    subgraph Resolution["Resolution Protocol"]
        RESOLVE["Resolver submits evidence"]
        CLEARANCE["Clearance Check\nresolver.level >= source.level"]
        EXIT_SAFE["Exit Safe Mode"]
        REPLAY["Replay deferred messages\nFIFO order"]
    end

    subgraph Cascade["Cascade & Propagation"]
        FLOOD["Flood Push\nAll agents in instance"]
        CHILD_HALT["Child Agent Halt\nvia hierarchy traversal"]
        CROSS_INST["Cross-Instance\nPriorityUrgent AgentMail"]
    end

    Triggers --> Scopes
    Scopes --> SafeMode
    SafeMode --> Resolution
    SCOPE_WS & SCOPE_GLOBAL --> Cascade
    Resolution --> EXIT_SAFE --> REPLAY
```

## 13. Provider Adapter Architecture

```mermaid
graph TB
    subgraph Bridge["Tool-Use Bridge"]
        PROCESS["ProcessMessage()"]
    end

    subgraph Adapters["Provider Adapters"]
        ANTH["Anthropic Adapter\nanthropic-sdk-go"]
        OAI["OpenAI Adapter\nopenai-go"]
        GOOG["Google Adapter\ngenai SDK"]
        BEDR["Bedrock Adapter\naws-sdk-go-v2"]
        AZURE["Azure Adapter\nopenai-go"]
        OLLAMA["Ollama Adapter\nNative HTTP"]
    end

    subgraph Interface["Adapter Interface"]
        FORMAT_REQ["FormatRequest()\nConvert to provider schema"]
        FORMAT_TURN["FormatTurnMessages()\nBuild conversation turns"]
        PARSE_RESP["ParseResponse()\nExtract content + tool_calls"]
    end

    subgraph SDKs["Official SDKs (Mandatory)"]
        SDK_ANTH["anthropic-sdk-go"]
        SDK_OAI["openai-go"]
        SDK_GOOG["google.golang.org/genai"]
        SDK_AWS["aws-sdk-go-v2"]
    end

    PROCESS --> Adapters
    Adapters --> Interface
    ANTH --> SDK_ANTH
    OAI --> SDK_OAI
    GOOG --> SDK_GOOG
    BEDR --> SDK_AWS
    AZURE --> SDK_OAI
```

## 14. Frontend Architecture

```mermaid
graph TB
    subgraph App["React 18 + Vite + TypeScript"]
        ROUTER["React Router v6"]
    end

    subgraph Pages["Pages"]
        CHAT["/\nChatPage"]
        PIPE["/pipelines\nPipelinesPage"]
        TASKS["/tasks\nTasksPage\nKanban Board"]
        ORG["/org\nOrgPage\nHierarchy Viz"]
        DOCS["/docs\nDocsPage"]
        AUDIT_P["/audit\nAuditPage"]
        ACTIONS_P["/actions\nActionsPage"]
    end

    subgraph Settings["Settings Hub"]
        PROV["/settings/providers"]
        WS_SET["/settings/workspaces"]
        OBS["/settings/observability"]
        BUDGET_SET["/settings/budget"]
        PLUGIN_SET["/settings/plugins"]
        SEC_SET["/settings/security"]
        SYS["/settings/system"]
    end

    subgraph Services["Service Layer"]
        MCP_CALL["mcpCall()\nMCP via HTTP"]
        WS_SVC["WebSocket\nReal-time events"]
        TANSTACK["TanStack Query\nServer state"]
    end

    subgraph UI["UI Framework"]
        SHADCN["shadcn/ui\nTailwind CSS"]
        LUCIDE["Lucide Icons"]
        MARKDOWN["ReactMarkdown\nremark-gfm"]
    end

    ROUTER --> Pages & Settings
    Pages --> Services
    Settings --> Services
    MCP_CALL -->|POST /mcp| BACKEND["Hyperax Backend"]
    WS_SVC -->|/ws/events| BACKEND
```
