-- Channel sessions: tracks connected Claude Code instances via the hyperax-bridge
CREATE TABLE channel_sessions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    workspace_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'connected' CHECK(status IN ('connected', 'disconnected', 'idle')),
    bridge_version TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    connected_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_heartbeat TEXT NOT NULL DEFAULT (datetime('now')),
    disconnected_at TEXT
);
CREATE INDEX idx_channel_sessions_status ON channel_sessions(status);

-- Channel messages: chat history between dashboard users and Claude Code sessions
CREATE TABLE channel_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('inbound', 'outbound', 'system')),
    content TEXT NOT NULL,
    sender TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (session_id) REFERENCES channel_sessions(id)
);
CREATE INDEX idx_channel_messages_session ON channel_messages(session_id);
CREATE INDEX idx_channel_messages_created ON channel_messages(created_at);

-- Channel event queue: pending events to push to Claude Code via the bridge
CREATE TABLE channel_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    event_type TEXT NOT NULL DEFAULT 'message',
    content TEXT NOT NULL,
    meta TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'delivered', 'expired')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    delivered_at TEXT,
    FOREIGN KEY (session_id) REFERENCES channel_sessions(id)
);
CREATE INDEX idx_channel_events_session_status ON channel_events(session_id, status);

-- Permission relay: pending tool-use approval requests forwarded from Claude Code
CREATE TABLE channel_permissions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    input_preview TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'allowed', 'denied', 'expired')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    resolved_at TEXT,
    resolved_by TEXT,
    FOREIGN KEY (session_id) REFERENCES channel_sessions(id)
);
CREATE INDEX idx_channel_permissions_session ON channel_permissions(session_id, status);
