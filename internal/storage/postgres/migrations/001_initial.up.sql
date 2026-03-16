-- Hyperax v0.1.0 initial schema (PostgreSQL)
-- Consolidated from 44 incremental migrations

CREATE TABLE migration_history (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (NOW())
		);
CREATE TABLE workspaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    root_path   TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    metadata    TEXT
);
CREATE TABLE config_keys (
    key         TEXT PRIMARY KEY,
    scope_type  TEXT NOT NULL DEFAULT 'global',
    value_type  TEXT NOT NULL DEFAULT 'string',
    default_val TEXT,
    critical    INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE TABLE config_values (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL REFERENCES config_keys(key),
    scope_type  TEXT NOT NULL DEFAULT 'global',
    scope_id    TEXT NOT NULL DEFAULT '',
    value       TEXT NOT NULL,
    updated_by  TEXT NOT NULL DEFAULT 'system',
    updated_at  TEXT NOT NULL DEFAULT (NOW()),
    UNIQUE(key, scope_type, scope_id)
);
CREATE INDEX idx_config_values_key ON config_values(key);
CREATE INDEX idx_config_values_scope ON config_values(scope_type, scope_id);
CREATE TABLE file_hashes (
    file_id      SERIAL PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    file_path    TEXT    NOT NULL,
    hash_value   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL DEFAULT (NOW()),
    UNIQUE (workspace_id, file_path)
);
CREATE TABLE symbols (
    id           SERIAL PRIMARY KEY,
    file_id      INTEGER NOT NULL REFERENCES file_hashes(file_id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    start_line   INTEGER NOT NULL,
    end_line     INTEGER NOT NULL,
    signature    TEXT,
    workspace_id TEXT    NOT NULL
);
CREATE INDEX idx_symbols_workspace ON symbols(workspace_id);
CREATE INDEX idx_symbols_file_id ON symbols(file_id);
CREATE INDEX idx_symbols_name ON symbols(name);
CREATE INDEX idx_symbols_kind ON symbols(kind);
CREATE TABLE imports (
    id             SERIAL PRIMARY KEY,
    file_id        INTEGER NOT NULL REFERENCES file_hashes(file_id) ON DELETE CASCADE,
    module_name    TEXT    NOT NULL,
    imported_names TEXT,
    alias          TEXT,
    is_from_import INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_imports_file_id ON imports(file_id);
CREATE INDEX idx_imports_module ON imports(module_name);
CREATE TABLE standard_sections (
    id           SERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    section      TEXT NOT NULL,
    content      TEXT NOT NULL,
    updated_at   TEXT NOT NULL DEFAULT (NOW()),
    UNIQUE (workspace_id, section)
);
CREATE TABLE doc_chunks (
    id             SERIAL PRIMARY KEY,
    workspace_id   TEXT    NOT NULL,
    file_path      TEXT    NOT NULL,
    file_hash      TEXT    NOT NULL,
    chunk_index    INTEGER NOT NULL,
    section_header TEXT,
    content        TEXT    NOT NULL,
    token_count    INTEGER NOT NULL DEFAULT 0
, content_type TEXT NOT NULL DEFAULT 'doc');
CREATE INDEX idx_doc_chunks_workspace ON doc_chunks(workspace_id);
CREATE INDEX idx_doc_chunks_file ON doc_chunks(workspace_id, file_path);
CREATE UNIQUE INDEX idx_doc_chunks_unique ON doc_chunks(workspace_id, file_path, chunk_index);
CREATE TABLE tool_metrics (
    id                SERIAL PRIMARY KEY,
    tool_name         TEXT    NOT NULL UNIQUE,
    call_count        INTEGER NOT NULL DEFAULT 0,
    last_used         TEXT,
    total_duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE pipelines (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    workspace_name  TEXT NOT NULL,
    project_name    TEXT,
    swimlanes       TEXT NOT NULL DEFAULT '[]',
    setup_commands  TEXT NOT NULL DEFAULT '[]',
    environment     TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    updated_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_pipelines_workspace ON pipelines(workspace_name);
CREATE TABLE pipeline_jobs (
    id              TEXT PRIMARY KEY,
    pipeline_id     TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending',
    workspace_name  TEXT NOT NULL,
    started_at      TEXT,
    completed_at    TEXT,
    error           TEXT,
    result          TEXT
);
CREATE INDEX idx_jobs_pipeline ON pipeline_jobs(pipeline_id);
CREATE INDEX idx_jobs_status ON pipeline_jobs(status);
CREATE TABLE step_results (
    id            TEXT    PRIMARY KEY,
    job_id        TEXT    NOT NULL REFERENCES pipeline_jobs(id) ON DELETE CASCADE,
    swimlane_id   TEXT    NOT NULL,
    step_id       TEXT    NOT NULL,
    step_name     TEXT    NOT NULL,
    status        TEXT    NOT NULL DEFAULT 'pending',
    exit_code     INTEGER,
    started_at    TEXT,
    completed_at  TEXT,
    duration_ms   INTEGER,
    output_log    TEXT,
    error         TEXT
);
CREATE INDEX idx_steps_job ON step_results(job_id);
CREATE TABLE project_plans (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    workspace_name  TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    priority        TEXT NOT NULL DEFAULT 'medium',
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    updated_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_plans_workspace ON project_plans(workspace_name);
CREATE INDEX idx_plans_status ON project_plans(status);
CREATE TABLE comments (
    id          TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    content     TEXT NOT NULL,
    author      TEXT,
    created_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_comments_entity ON comments(entity_type, entity_id);
CREATE TABLE personas (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT,
    system_prompt     TEXT,
    team              TEXT,
    role              TEXT,
    home_machine_uuid TEXT,
    clearance_level   INTEGER NOT NULL DEFAULT 0,
    is_active         INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL DEFAULT (NOW()),
    updated_at        TEXT NOT NULL DEFAULT (NOW())
, provider_id TEXT, default_model TEXT, guard_bypass INTEGER NOT NULL DEFAULT 0, role_template_id TEXT, is_internal INTEGER NOT NULL DEFAULT 0);
CREATE INDEX idx_personas_team ON personas(team);
CREATE TABLE audits (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    workspace_name    TEXT NOT NULL,
    project_name      TEXT,
    status            TEXT NOT NULL DEFAULT 'pending',
    audit_type        TEXT NOT NULL DEFAULT 'general',
    scope_description TEXT,
    created_at        TEXT NOT NULL DEFAULT (NOW()),
    updated_at        TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_audits_workspace ON audits(workspace_name);
CREATE INDEX idx_audits_status ON audits(status);
CREATE TABLE audit_items (
    id            TEXT    PRIMARY KEY,
    audit_id      TEXT    NOT NULL REFERENCES audits(id) ON DELETE CASCADE,
    item_type     TEXT    NOT NULL,
    file_path     TEXT,
    symbol_name   TEXT,
    status        TEXT    NOT NULL DEFAULT 'pending',
    context_data  TEXT    NOT NULL DEFAULT '{}',
    findings      TEXT    NOT NULL DEFAULT '{}',
    reviewed_at   TEXT
);
CREATE INDEX idx_items_audit ON audit_items(audit_id);
CREATE INDEX idx_items_status ON audit_items(status);
CREATE TABLE domain_events (
    id              TEXT PRIMARY KEY,
    event_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    payload         TEXT,
    promoted_by     TEXT NOT NULL,
    scope           TEXT,
    sequence_id     INTEGER NOT NULL DEFAULT 0,
    trace_id        TEXT,
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    expires_at      TEXT
);
CREATE INDEX idx_domain_event_type ON domain_events(event_type);
CREATE INDEX idx_domain_event_scope ON domain_events(scope);
CREATE INDEX idx_domain_event_time ON domain_events(created_at);
CREATE INDEX idx_domain_event_sequence ON domain_events(sequence_id);
CREATE TABLE nervous_event_log (
    id              TEXT PRIMARY KEY,
    event_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    scope           TEXT,
    payload         TEXT,
    sequence_id     INTEGER NOT NULL DEFAULT 0,
    trace_id        TEXT,
    created_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_nervous_event_type ON nervous_event_log(event_type);
CREATE INDEX idx_nervous_event_time ON nervous_event_log(created_at);
CREATE TABLE interjections (
    id          TEXT PRIMARY KEY,
    scope       TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT 'warning',
    source      TEXT NOT NULL,
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    resolution  TEXT,
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    resolved_at TEXT
, created_by TEXT, source_clearance INTEGER NOT NULL DEFAULT 0, resolved_by TEXT, resolver_clearance INTEGER, remediation_persona TEXT, action TEXT, trust_level TEXT, trace_id TEXT, expires_at TEXT);
CREATE INDEX idx_interjections_scope ON interjections(scope);
CREATE INDEX idx_interjections_status ON interjections(status);
CREATE TABLE lifecycle_transitions (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    from_state  TEXT NOT NULL,
    to_state    TEXT NOT NULL,
    reason      TEXT,
    created_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_lifecycle_agent ON lifecycle_transitions(agent_id);
CREATE TABLE agent_heartbeats (
    agent_id    TEXT PRIMARY KEY,
    state       TEXT NOT NULL DEFAULT 'idle',
    updated_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE TABLE agent_memory (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'conversation',
    content     TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_memory_agent ON agent_memory(agent_id);
CREATE INDEX idx_memory_scope ON agent_memory(scope);
CREATE TABLE secrets (
    id          SERIAL PRIMARY KEY,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'global',
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    updated_at  TEXT NOT NULL DEFAULT (NOW()), access_scope TEXT NOT NULL DEFAULT 'global',
    UNIQUE(key, scope)
);
CREATE INDEX idx_secrets_scope ON secrets(scope);
CREATE TABLE budget_thresholds (
    scope       TEXT PRIMARY KEY,
    threshold   REAL NOT NULL DEFAULT 0.0,
    updated_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE TABLE budget_records (
    id          SERIAL PRIMARY KEY,
    scope       TEXT NOT NULL,
    cost        REAL NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (NOW())
, provider_id TEXT DEFAULT '', model TEXT DEFAULT '');
CREATE INDEX idx_budget_scope ON budget_records(scope);
CREATE TABLE cron_jobs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    schedule    TEXT NOT NULL,
    job_type    TEXT NOT NULL DEFAULT 'tool',
    payload     TEXT NOT NULL DEFAULT '{}',
    enabled     INTEGER NOT NULL DEFAULT 1,
    max_retries INTEGER NOT NULL DEFAULT 3,
    next_run_at TEXT,
    last_run_at TEXT,
    last_status TEXT,
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    updated_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX idx_cron_jobs_next_run ON cron_jobs(next_run_at);
CREATE TABLE cron_executions (
    id          TEXT PRIMARY KEY,
    cron_job_id TEXT NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'running',
    started_at  TEXT NOT NULL DEFAULT (NOW()),
    finished_at TEXT,
    duration_ms INTEGER,
    error       TEXT,
    attempt     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_cron_exec_job ON cron_executions(cron_job_id);
CREATE TABLE cron_dlq (
    id            TEXT PRIMARY KEY,
    cron_job_id   TEXT NOT NULL,
    failed_at     TEXT NOT NULL DEFAULT (NOW()),
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    payload       TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_cron_dlq_job ON cron_dlq(cron_job_id);
CREATE TABLE workflows (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    updated_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_workflows_name ON workflows(name);
CREATE TABLE workflow_steps (
    id                TEXT PRIMARY KEY,
    workflow_id       TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    step_type         TEXT NOT NULL DEFAULT 'tool',
    action            TEXT NOT NULL DEFAULT '{}',
    depends_on        TEXT NOT NULL DEFAULT '',
    condition         TEXT NOT NULL DEFAULT '',
    requires_approval INTEGER NOT NULL DEFAULT 0,
    position          INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL DEFAULT (NOW()),
    updated_at        TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_workflow_steps_workflow ON workflow_steps(workflow_id);
CREATE TABLE workflow_runs (
    id          TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending',
    started_at  TEXT,
    finished_at TEXT,
    error       TEXT,
    context     TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT (NOW()),
    updated_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_workflow_runs_workflow ON workflow_runs(workflow_id);
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);
CREATE TABLE workflow_run_steps (
    id         TEXT PRIMARY KEY,
    run_id     TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_id    TEXT NOT NULL REFERENCES workflow_steps(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending',
    started_at TEXT,
    finished_at TEXT,
    output     TEXT,
    error      TEXT,
    created_at TEXT NOT NULL DEFAULT (NOW()),
    updated_at TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_workflow_run_steps_run ON workflow_run_steps(run_id);
CREATE INDEX idx_workflow_run_steps_step ON workflow_run_steps(step_id);
CREATE TABLE workflow_triggers (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    config       TEXT NOT NULL DEFAULT '{}',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (NOW()),
    updated_at   TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_workflow_triggers_workflow ON workflow_triggers(workflow_id);
CREATE INDEX idx_domain_events_expires_at ON domain_events(expires_at);
CREATE TABLE event_handlers (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    event_filter   TEXT NOT NULL DEFAULT '*',
    action         TEXT NOT NULL DEFAULT 'log',
    action_payload TEXT NOT NULL DEFAULT '{}',
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL DEFAULT (NOW()),
    updated_at     TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_event_handlers_enabled ON event_handlers(enabled);
CREATE TABLE agent_relationships (
    id           TEXT PRIMARY KEY,
    parent_agent TEXT NOT NULL,
    child_agent  TEXT NOT NULL,
    relationship TEXT NOT NULL DEFAULT 'supervisor',
    created_at   TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_agent_rel_parent ON agent_relationships(parent_agent);
CREATE INDEX idx_agent_rel_child ON agent_relationships(child_agent);
CREATE TABLE comm_log (
    id           TEXT PRIMARY KEY,
    from_agent   TEXT NOT NULL,
    to_agent     TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text',
    content      TEXT NOT NULL DEFAULT '',
    trust        TEXT NOT NULL DEFAULT 'internal',
    direction    TEXT NOT NULL DEFAULT 'sent',
    created_at   TEXT NOT NULL DEFAULT (NOW())
, session_id TEXT DEFAULT '');
CREATE INDEX idx_comm_log_from ON comm_log(from_agent);
CREATE INDEX idx_comm_log_to ON comm_log(to_agent);
CREATE INDEX idx_comm_log_created ON comm_log(created_at);
CREATE TABLE comm_permissions (
    id         TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL,
    target_id  TEXT NOT NULL,
    permission TEXT NOT NULL DEFAULT 'both',
    created_at TEXT NOT NULL DEFAULT (NOW()),
    UNIQUE(agent_id, target_id)
);
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL,
    started_at TEXT NOT NULL DEFAULT (NOW()),
    ended_at   TEXT,
    tool_calls INTEGER NOT NULL DEFAULT 0,
    total_cost REAL    NOT NULL DEFAULT 0.0,
    status     TEXT    NOT NULL DEFAULT 'active',
    metadata   TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL DEFAULT (NOW())
, provider_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '');
CREATE INDEX idx_sessions_agent_id ON sessions(agent_id);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE TABLE tool_call_metrics (
    id          TEXT PRIMARY KEY,
    session_id  TEXT    NOT NULL REFERENCES sessions(id),
    tool_name   TEXT    NOT NULL,
    started_at  TEXT    NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    success     INTEGER NOT NULL DEFAULT 1,
    error_msg   TEXT,
    input_size  INTEGER NOT NULL DEFAULT 0,
    output_size INTEGER NOT NULL DEFAULT 0,
    cost        REAL    NOT NULL DEFAULT 0.0
, provider_id TEXT NOT NULL DEFAULT '');
CREATE INDEX idx_tool_call_metrics_session ON tool_call_metrics(session_id);
CREATE INDEX idx_tool_call_metrics_tool ON tool_call_metrics(tool_name);
CREATE INDEX idx_tool_call_metrics_started ON tool_call_metrics(started_at);
CREATE TABLE alerts (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    metric        TEXT NOT NULL,
    operator      TEXT NOT NULL,
    threshold     REAL NOT NULL,
    window        TEXT NOT NULL DEFAULT '1h',
    severity      TEXT NOT NULL DEFAULT 'info',
    enabled       INTEGER NOT NULL DEFAULT 1,
    last_fired_at TEXT,
    created_at    TEXT NOT NULL DEFAULT (NOW()),
    updated_at    TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_alerts_enabled ON alerts(enabled);
CREATE TABLE providers (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL,  -- 'openai', 'anthropic', 'ollama', 'azure', 'custom'
    base_url        TEXT NOT NULL,
    secret_key_ref  TEXT,           -- key name in secrets table (scope=global), NULL for keyless providers like local ollama
    is_default      INTEGER NOT NULL DEFAULT 0,
    is_enabled      INTEGER NOT NULL DEFAULT 1,
    models          TEXT NOT NULL DEFAULT '[]',  -- JSON array of available model names
    metadata        TEXT NOT NULL DEFAULT '{}',  -- JSON bag for extra provider-specific config
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    updated_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE UNIQUE INDEX idx_providers_default ON providers(is_default) WHERE is_default = 1;
CREATE INDEX idx_providers_kind ON providers(kind);
CREATE TABLE memories (
    id                TEXT PRIMARY KEY,
    scope             TEXT NOT NULL,          -- 'global', 'project', 'persona'
    type              TEXT NOT NULL,          -- 'episodic', 'semantic', 'procedural'
    content           TEXT NOT NULL,
    workspace_id      TEXT,                   -- NULL for global scope
    persona_id        TEXT,                   -- NULL for global/project scope
    metadata          TEXT DEFAULT '{}',      -- JSON: {source, confidence, tags, anchored}
    embedding         BLOB,                   -- 384-dim float32 vector (NULL if not embedded)
    created_at        TEXT NOT NULL DEFAULT (NOW()),
    accessed_at       TEXT NOT NULL DEFAULT (NOW()),
    access_count      INTEGER DEFAULT 0,
    consolidated_into TEXT,                   -- points to merged memory ID
    contested_by      TEXT,                   -- ID of conflicting memory
    contested_at      TEXT                    -- when conflict was detected
);
CREATE INDEX idx_memories_scope ON memories(scope, workspace_id);
CREATE INDEX idx_memories_persona ON memories(persona_id);
CREATE INDEX idx_memories_type ON memories(type);
CREATE INDEX idx_memories_accessed ON memories(accessed_at);
CREATE INDEX idx_memories_contested ON memories(contested_by)
    WHERE contested_by IS NOT NULL;
CREATE TABLE memory_annotations (
    id              TEXT PRIMARY KEY,
    memory_id       TEXT NOT NULL,
    annotation      TEXT NOT NULL,
    annotation_type TEXT NOT NULL,           -- warning, correction, context, deprecation
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_memory_annotation_memory ON memory_annotations(memory_id);
CREATE INDEX idx_interjections_severity ON interjections(severity);
CREATE INDEX idx_interjections_source ON interjections(source);
CREATE INDEX idx_interjections_created_by ON interjections(created_by);
CREATE INDEX idx_interjections_trace_id ON interjections(trace_id);
CREATE TABLE sieve_bypass (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    pattern         TEXT NOT NULL,
    granted_by      TEXT NOT NULL,
    granted_at      TEXT NOT NULL DEFAULT (NOW()),
    expires_at      TEXT NOT NULL,
    reason          TEXT,
    revoked         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_sieve_bypass_scope ON sieve_bypass(scope);
CREATE INDEX idx_sieve_bypass_expires ON sieve_bypass(expires_at);
CREATE TABLE interject_dlq (
    id              TEXT PRIMARY KEY,
    interjection_id TEXT NOT NULL,
    message_type    TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    source          TEXT NOT NULL,
    scope           TEXT NOT NULL,
    queued_at       TEXT NOT NULL DEFAULT (NOW()),
    replayed_at     TEXT,
    dismissed_at    TEXT,
    status          TEXT NOT NULL DEFAULT 'queued'
);
CREATE INDEX idx_interject_dlq_interjection ON interject_dlq(interjection_id);
CREATE INDEX idx_interject_dlq_status ON interject_dlq(status);
CREATE TABLE pipeline_assignments (
    id          TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    project_id  TEXT,
    assigned_at TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_pa_pipeline ON pipeline_assignments(pipeline_id);
CREATE INDEX idx_pa_workspace ON pipeline_assignments(workspace_id);
CREATE INDEX idx_pa_project ON pipeline_assignments(project_id);
CREATE TABLE agent_checkpoints (
    id               TEXT PRIMARY KEY,
    agent_id         TEXT NOT NULL,
    task_id          TEXT NOT NULL DEFAULT '',
    last_message_id  TEXT NOT NULL DEFAULT '',
    working_context  TEXT NOT NULL DEFAULT '{}',
    active_files     TEXT NOT NULL DEFAULT '[]',
    refactor_tx_id   TEXT NOT NULL DEFAULT '',
    checkpointed_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_checkpoint_agent ON agent_checkpoints(agent_id);
CREATE INDEX idx_checkpoint_agent_time ON agent_checkpoints(agent_id, checkpointed_at DESC);
CREATE TABLE symbol_embeddings (
    symbol_id   TEXT PRIMARY KEY REFERENCES symbols(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    embedding   BLOB NOT NULL,         -- float32 array serialised as little-endian bytes
    dim         INTEGER NOT NULL,      -- embedding dimension (e.g. 384)
    model       TEXT NOT NULL DEFAULT 'all-MiniLM-L6-v2',
    created_at  DATETIME NOT NULL DEFAULT (NOW()),
    updated_at  DATETIME NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_symbol_emb_workspace ON symbol_embeddings(workspace_id);
CREATE TABLE doc_chunk_embeddings (
    chunk_id    TEXT PRIMARY KEY REFERENCES doc_chunks(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    embedding   BLOB NOT NULL,
    dim         INTEGER NOT NULL,
    model       TEXT NOT NULL DEFAULT 'all-MiniLM-L6-v2',
    created_at  DATETIME NOT NULL DEFAULT (NOW()),
    updated_at  DATETIME NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_doc_emb_workspace ON doc_chunk_embeddings(workspace_id);
CREATE TABLE delegations (
    id              TEXT PRIMARY KEY,
    granter_id      TEXT NOT NULL,
    grantee_id      TEXT NOT NULL,
    grant_type      TEXT NOT NULL CHECK(grant_type IN ('clearance_elevation', 'credential_passthrough', 'scope_access')),
    credential_key  TEXT,
    elevated_level  INTEGER,
    scopes          TEXT,
    expires_at      TEXT,
    reason          TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    revoked_at      TEXT
);
CREATE INDEX idx_delegation_grantee ON delegations(grantee_id);
CREATE INDEX idx_delegation_granter ON delegations(granter_id);
CREATE INDEX idx_delegation_active ON delegations(grantee_id, revoked_at) WHERE revoked_at IS NULL;
CREATE TABLE agentmail_messages (
    id          TEXT PRIMARY KEY,
    from_id     TEXT NOT NULL,
    to_id       TEXT NOT NULL,
    workspace_id TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT 'standard',
    payload     TEXT NOT NULL DEFAULT '{}',
    pgp_signature TEXT NOT NULL DEFAULT '',
    encrypted   INTEGER NOT NULL DEFAULT 0,
    ack_deadline_ms INTEGER NOT NULL DEFAULT 300000,
    schema_id   TEXT NOT NULL DEFAULT '',
    direction   TEXT NOT NULL DEFAULT 'outbound',
    sent_at     TEXT NOT NULL DEFAULT (NOW()),
    created_at  TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_agentmail_direction_priority
    ON agentmail_messages(direction, priority, sent_at);
CREATE INDEX idx_agentmail_to
    ON agentmail_messages(to_id);
CREATE TABLE agentmail_acks (
    mail_id     TEXT PRIMARY KEY REFERENCES agentmail_messages(id) ON DELETE CASCADE,
    instance_id TEXT NOT NULL,
    acked_at    TEXT NOT NULL DEFAULT (NOW()),
    status      TEXT NOT NULL DEFAULT 'received'
);
CREATE TABLE agentmail_dead_letters (
    id              TEXT PRIMARY KEY,
    mail_id         TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    attempts        INTEGER NOT NULL DEFAULT 0,
    quarantined_at  TEXT NOT NULL DEFAULT (NOW()),
    original_mail   TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_agentmail_dlo_quarantined
    ON agentmail_dead_letters(quarantined_at DESC);
CREATE TABLE commhub_overflow (
    id           TEXT PRIMARY KEY,
    agent_id     TEXT NOT NULL,
    from_agent   TEXT NOT NULL,
    to_agent     TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text',
    content      TEXT NOT NULL DEFAULT '',
    trust        INTEGER NOT NULL DEFAULT 0,
    metadata     TEXT NOT NULL DEFAULT '{}',
    original_ts  INTEGER NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (NOW()),
    retrieved    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_overflow_agent ON commhub_overflow(agent_id, retrieved);
CREATE INDEX idx_overflow_created ON commhub_overflow(created_at);
CREATE TABLE plugins (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    version       TEXT NOT NULL DEFAULT '',
    type          TEXT NOT NULL DEFAULT 'mcp',
    status        TEXT NOT NULL DEFAULT 'loaded',
    enabled       INTEGER NOT NULL DEFAULT 0,
    tool_count    INTEGER NOT NULL DEFAULT 0,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    failure_count INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT '',
    loaded_at     TEXT NOT NULL DEFAULT (NOW()),
    last_health_at TEXT
);
CREATE INDEX idx_plugins_name ON plugins(name);
CREATE INDEX idx_plugins_enabled ON plugins(enabled);
CREATE INDEX idx_secrets_access_scope ON secrets(access_scope);
CREATE INDEX idx_doc_chunks_content_type ON doc_chunks(content_type);
CREATE VIRTUAL TABLE symbols_fts USING fts5(
    name,
    signature,
    kind,
    content=symbols,
    content_rowid=id,
    tokenize='porter unicode61'
)
/* symbols_fts(name,signature,kind) */;
CREATE TABLE IF NOT EXISTS 'symbols_fts_data'(id INTEGER PRIMARY KEY, block BLOB);
CREATE TABLE IF NOT EXISTS 'symbols_fts_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS 'symbols_fts_docsize'(id INTEGER PRIMARY KEY, sz BLOB);
CREATE TABLE IF NOT EXISTS 'symbols_fts_config'(k PRIMARY KEY, v) WITHOUT ROWID;
CREATE VIRTUAL TABLE doc_chunks_fts USING fts5(
    content,
    section_header,
    content=doc_chunks,
    content_rowid=id,
    tokenize='porter unicode61'
)
/* doc_chunks_fts(content,section_header) */;
CREATE TABLE IF NOT EXISTS 'doc_chunks_fts_data'(id INTEGER PRIMARY KEY, block BLOB);
CREATE TABLE IF NOT EXISTS 'doc_chunks_fts_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS 'doc_chunks_fts_docsize'(id INTEGER PRIMARY KEY, sz BLOB);
CREATE TABLE IF NOT EXISTS 'doc_chunks_fts_config'(k PRIMARY KEY, v) WITHOUT ROWID;
CREATE TRIGGER symbols_ai AFTER INSERT ON symbols BEGIN
    INSERT INTO symbols_fts(rowid, name, signature, kind)
    VALUES (new.id, new.name, new.signature, new.kind);
END;
CREATE TRIGGER symbols_ad AFTER DELETE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, signature, kind)
    VALUES ('delete', old.id, old.name, old.signature, old.kind);
END;
CREATE TRIGGER symbols_au AFTER UPDATE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, signature, kind)
    VALUES ('delete', old.id, old.name, old.signature, old.kind);
    INSERT INTO symbols_fts(rowid, name, signature, kind)
    VALUES (new.id, new.name, new.signature, new.kind);
END;
CREATE TRIGGER doc_chunks_ai AFTER INSERT ON doc_chunks BEGIN
    INSERT INTO doc_chunks_fts(rowid, content, section_header)
    VALUES (new.id, new.content, new.section_header);
END;
CREATE TRIGGER doc_chunks_ad AFTER DELETE ON doc_chunks BEGIN
    INSERT INTO doc_chunks_fts(doc_chunks_fts, rowid, content, section_header)
    VALUES ('delete', old.id, old.content, old.section_header);
END;
CREATE TRIGGER doc_chunks_au AFTER UPDATE ON doc_chunks BEGIN
    INSERT INTO doc_chunks_fts(doc_chunks_fts, rowid, content, section_header)
    VALUES ('delete', old.id, old.content, old.section_header);
    INSERT INTO doc_chunks_fts(rowid, content, section_header)
    VALUES (new.id, new.content, new.section_header);
END;
CREATE TABLE agents (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    persona_id      TEXT NOT NULL,
    parent_agent_id TEXT,
    workspace_id    TEXT,
    status          TEXT NOT NULL DEFAULT 'idle',
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    updated_at      TEXT NOT NULL DEFAULT (NOW())
, personality TEXT DEFAULT '', role_template_id TEXT DEFAULT '', clearance_level INTEGER NOT NULL DEFAULT 0, provider_id TEXT DEFAULT '', default_model TEXT DEFAULT '', is_internal INTEGER NOT NULL DEFAULT 0, system_prompt TEXT DEFAULT '', guard_bypass INTEGER NOT NULL DEFAULT 0, engagement_rules TEXT DEFAULT '', status_reason TEXT DEFAULT '', chat_model TEXT NOT NULL DEFAULT '');
CREATE INDEX idx_agents_name ON agents(name);
CREATE INDEX idx_agents_persona ON agents(persona_id);
CREATE INDEX idx_agents_parent ON agents(parent_agent_id);
CREATE INDEX idx_agents_workspace ON agents(workspace_id);
CREATE INDEX idx_agents_clearance ON agents(clearance_level);
CREATE INDEX idx_agents_provider ON agents(provider_id);
CREATE INDEX idx_agents_is_internal ON agents(is_internal);
CREATE TABLE IF NOT EXISTS "mcp_tokens" (
    id              TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    label           TEXT NOT NULL DEFAULT '',
    clearance_level INTEGER NOT NULL DEFAULT 0,
    scopes          TEXT NOT NULL DEFAULT '[]',
    expires_at      TEXT,
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    revoked_at      TEXT
);
CREATE INDEX idx_mcp_tokens_agent ON mcp_tokens(agent_id);
CREATE INDEX idx_mcp_tokens_hash ON mcp_tokens(token_hash);
CREATE TABLE IF NOT EXISTS "milestones" (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES project_plans(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    description      TEXT,
    status           TEXT NOT NULL DEFAULT 'pending',
    priority         TEXT NOT NULL DEFAULT 'medium',
    due_date         TEXT,
    order_index      INTEGER NOT NULL DEFAULT 0,
    assignee_agent_id TEXT
);
CREATE INDEX idx_milestones_project ON milestones(project_id);
CREATE TABLE IF NOT EXISTS "tasks" (
    id               TEXT PRIMARY KEY,
    milestone_id     TEXT NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    description      TEXT,
    status           TEXT NOT NULL DEFAULT 'pending',
    priority         TEXT NOT NULL DEFAULT 'medium',
    order_index      INTEGER NOT NULL DEFAULT 0,
    assignee_agent_id TEXT,
    created_at       TEXT NOT NULL DEFAULT (NOW()),
    updated_at       TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_tasks_milestone ON tasks(milestone_id);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_agent_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE TABLE external_doc_sources (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    UNIQUE(workspace_id, path)
);
CREATE TABLE doc_tags (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    tag TEXT NOT NULL CHECK(tag IN ('architecture', 'standards')),
    source_type TEXT NOT NULL DEFAULT 'internal' CHECK(source_type IN ('internal', 'external')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    UNIQUE(workspace_id, tag)
);
CREATE TABLE chat_sessions (
    id          TEXT PRIMARY KEY,
    agent_name  TEXT NOT NULL,
    peer_id     TEXT NOT NULL,
    started_at  TEXT NOT NULL DEFAULT (NOW()),
    ended_at    TEXT,
    summary     TEXT DEFAULT ''
, archived_at TEXT);
CREATE INDEX idx_chat_sessions_agent_peer ON chat_sessions(agent_name, peer_id);
CREATE TABLE agent_work_queue (
    id           TEXT PRIMARY KEY,
    agent_name   TEXT NOT NULL,
    from_agent   TEXT NOT NULL,
    content      TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text',
    trust        TEXT NOT NULL DEFAULT 'internal',
    session_id   TEXT DEFAULT '',
    priority     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (NOW()),
    consumed_at  TEXT
);
CREATE INDEX idx_work_queue_agent_pending
    ON agent_work_queue(agent_name, priority DESC, created_at ASC)
    WHERE consumed_at IS NULL;
CREATE TABLE specs (
    id              TEXT PRIMARY KEY,
    spec_number     INTEGER NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'draft',
    project_id      TEXT REFERENCES project_plans(id) ON DELETE SET NULL,
    workspace_name  TEXT NOT NULL,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (NOW()),
    updated_at      TEXT NOT NULL DEFAULT (NOW()),
    UNIQUE(workspace_name, spec_number)
);
CREATE INDEX idx_specs_workspace ON specs(workspace_name);
CREATE INDEX idx_specs_status ON specs(status);
CREATE INDEX idx_specs_project ON specs(project_id);
CREATE TABLE spec_milestones (
    id              TEXT PRIMARY KEY,
    spec_id         TEXT NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    description     TEXT,
    order_index     INTEGER NOT NULL DEFAULT 0,
    milestone_id    TEXT REFERENCES milestones(id) ON DELETE SET NULL,
    created_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_spec_milestones_spec ON spec_milestones(spec_id);
CREATE TABLE spec_tasks (
    id                  TEXT PRIMARY KEY,
    spec_id             TEXT NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
    spec_milestone_id   TEXT NOT NULL REFERENCES spec_milestones(id) ON DELETE CASCADE,
    title               TEXT NOT NULL,
    requirement         TEXT,
    acceptance_criteria TEXT,
    order_index         INTEGER NOT NULL DEFAULT 0,
    task_id             TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    created_at          TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_spec_tasks_spec ON spec_tasks(spec_id);
CREATE INDEX idx_spec_tasks_milestone ON spec_tasks(spec_milestone_id);
CREATE TABLE spec_amendments (
    id              TEXT PRIMARY KEY,
    spec_id         TEXT NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    author          TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (NOW())
);
CREATE INDEX idx_spec_amendments_spec ON spec_amendments(spec_id);
CREATE UNIQUE INDEX idx_agents_unique_internal_name
    ON agents (name) WHERE is_internal = 1;
CREATE VIRTUAL TABLE memory_fts USING fts5(
    memory_id UNINDEXED,
    content,
    tokenize='porter unicode61'
)
/* memory_fts(memory_id,content) */;
CREATE TABLE IF NOT EXISTS 'memory_fts_data'(id INTEGER PRIMARY KEY, block BLOB);
CREATE TABLE IF NOT EXISTS 'memory_fts_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS 'memory_fts_content'(id INTEGER PRIMARY KEY, c0, c1);
CREATE TABLE IF NOT EXISTS 'memory_fts_docsize'(id INTEGER PRIMARY KEY, sz BLOB);
CREATE TABLE IF NOT EXISTS 'memory_fts_config'(k PRIMARY KEY, v) WITHOUT ROWID;
CREATE TRIGGER memories_fts_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memory_fts(memory_id, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER memories_fts_ad AFTER DELETE ON memories BEGIN
    DELETE FROM memory_fts WHERE memory_id = old.id;
END;
CREATE TRIGGER memories_fts_au AFTER UPDATE OF content ON memories BEGIN
    DELETE FROM memory_fts WHERE memory_id = old.id;
    INSERT INTO memory_fts(memory_id, content) VALUES (new.id, new.content);
END;
CREATE INDEX idx_sessions_provider_id ON sessions(provider_id);
CREATE INDEX idx_tool_call_metrics_provider ON tool_call_metrics(provider_id);
CREATE INDEX idx_budget_records_provider ON budget_records(provider_id);
