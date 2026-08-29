CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE nodes (
    node_id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('engineer', 'site')),
    name TEXT NOT NULL,
    overlay_ip TEXT NOT NULL UNIQUE,
    wg_public_key TEXT NOT NULL UNIQUE,
    node_token_hash BLOB NOT NULL,
    status TEXT NOT NULL DEFAULT 'OFFLINE',
    version TEXT NOT NULL DEFAULT '',
    os_version TEXT NOT NULL DEFAULT '',
    last_seen TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_nodes_type_status ON nodes(type, status);

CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY,
    engineer_node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    site_node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    active_at TEXT,
    closed_at TEXT,
    error_code TEXT
);

CREATE INDEX idx_sessions_engineer_status ON sessions(engineer_node_id, status);
CREATE INDEX idx_sessions_site_status ON sessions(site_node_id, status);

CREATE TABLE session_cidrs (
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    cidr TEXT NOT NULL,
    PRIMARY KEY (session_id, cidr)
);

CREATE TABLE session_stats (
    session_id TEXT PRIMARY KEY REFERENCES sessions(session_id) ON DELETE CASCADE,
    tx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
    rx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
    tx_packets INTEGER NOT NULL DEFAULT 0 CHECK (tx_packets >= 0),
    rx_packets INTEGER NOT NULL DEFAULT 0 CHECK (rx_packets >= 0),
    updated_at TEXT NOT NULL
);

CREATE TABLE event_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    time TEXT NOT NULL,
    level TEXT NOT NULL,
    module TEXT NOT NULL,
    node_id TEXT,
    session_id TEXT,
    message TEXT NOT NULL,
    fields_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_event_logs_time ON event_logs(time DESC);
CREATE INDEX idx_event_logs_node ON event_logs(node_id, time DESC);
CREATE INDEX idx_event_logs_session ON event_logs(session_id, time DESC);

