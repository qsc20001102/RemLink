-- Session participant IDs are historical identities. Revoking a registered
-- Node must release its credentials and address without deleting its Sessions.
-- Rebuild the parent and both child tables together so foreign_keys stays on
-- and existing CIDRs/counters survive replacing the original Sessions table.
CREATE TABLE sessions_retained (
    session_id TEXT PRIMARY KEY,
    engineer_node_id TEXT NOT NULL,
    site_node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    active_at TEXT,
    closed_at TEXT,
    error_code TEXT
);

CREATE TABLE session_cidrs_retained (
    session_id TEXT NOT NULL REFERENCES sessions_retained(session_id) ON DELETE CASCADE,
    cidr TEXT NOT NULL,
    PRIMARY KEY (session_id, cidr)
);

CREATE TABLE session_stats_retained (
    session_id TEXT PRIMARY KEY REFERENCES sessions_retained(session_id) ON DELETE CASCADE,
    tx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
    rx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
    tx_packets INTEGER NOT NULL DEFAULT 0 CHECK (tx_packets >= 0),
    rx_packets INTEGER NOT NULL DEFAULT 0 CHECK (rx_packets >= 0),
    updated_at TEXT NOT NULL
);

INSERT INTO sessions_retained SELECT * FROM sessions;
INSERT INTO session_cidrs_retained SELECT * FROM session_cidrs;
INSERT INTO session_stats_retained SELECT * FROM session_stats;

DROP TABLE session_cidrs;
DROP TABLE session_stats;
DROP TABLE sessions;

ALTER TABLE sessions_retained RENAME TO sessions;
ALTER TABLE session_cidrs_retained RENAME TO session_cidrs;
ALTER TABLE session_stats_retained RENAME TO session_stats;

CREATE INDEX idx_sessions_engineer_status ON sessions(engineer_node_id, status);
CREATE INDEX idx_sessions_site_status ON sessions(site_node_id, status);

-- New Sessions still require registered participants; only the lifetime of
-- historical records is independent of the Node registration lifetime.
CREATE TRIGGER sessions_require_participants_insert
BEFORE INSERT ON sessions
WHEN NOT EXISTS (SELECT 1 FROM nodes WHERE node_id = NEW.engineer_node_id)
  OR NOT EXISTS (SELECT 1 FROM nodes WHERE node_id = NEW.site_node_id)
BEGIN
    SELECT RAISE(ABORT, 'Session participants must be registered nodes');
END;

CREATE TRIGGER sessions_require_participants_update
BEFORE UPDATE OF engineer_node_id, site_node_id ON sessions
WHEN NOT EXISTS (SELECT 1 FROM nodes WHERE node_id = NEW.engineer_node_id)
  OR NOT EXISTS (SELECT 1 FROM nodes WHERE node_id = NEW.site_node_id)
BEGIN
    SELECT RAISE(ABORT, 'Session participants must be registered nodes');
END;
