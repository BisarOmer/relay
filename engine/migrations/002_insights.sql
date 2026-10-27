-- +migrate Up
CREATE TABLE IF NOT EXISTS insights_systems (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    monitoring_base_url TEXT NOT NULL,
    connection_id TEXT,
    collect_interval_ms INTEGER NOT NULL DEFAULT 5000,
    request_timeout_ms INTEGER NOT NULL DEFAULT 3000,
    retention_epochs INTEGER NOT NULL DEFAULT 2880,
    enabled INTEGER NOT NULL DEFAULT 1,
    demo INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_epoch_id TEXT,
    last_collected_at TEXT,
    last_status TEXT NOT NULL DEFAULT 'idle'
);

CREATE TABLE IF NOT EXISTS insights_epochs (
    id TEXT PRIMARY KEY,
    system_id TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    completeness REAL NOT NULL DEFAULT 0,
    endpoint_status_json TEXT NOT NULL,
    error_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY(system_id) REFERENCES insights_systems(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_epochs_system_ts ON insights_epochs(system_id, collected_at);

CREATE TABLE IF NOT EXISTS insights_entities (
    system_id TEXT NOT NULL,
    epoch_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    native_id TEXT,
    display_name TEXT NOT NULL,
    parent_id TEXT,
    status TEXT NOT NULL DEFAULT 'unknown',
    props_json TEXT NOT NULL,
    related_json TEXT,
    first_seen_at TEXT,
    last_seen_at TEXT NOT NULL,
    freshness TEXT NOT NULL DEFAULT 'fresh',
    PRIMARY KEY (system_id, epoch_id, entity_id),
    FOREIGN KEY(system_id) REFERENCES insights_systems(id) ON DELETE CASCADE,
    FOREIGN KEY(epoch_id) REFERENCES insights_epochs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_entities_type ON insights_entities(system_id, epoch_id, entity_type);

CREATE TABLE IF NOT EXISTS insights_metrics (
    system_id TEXT NOT NULL,
    epoch_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value REAL,
    unit TEXT,
    available INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (system_id, epoch_id, entity_id, metric_name),
    FOREIGN KEY(epoch_id) REFERENCES insights_epochs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS insights_findings (
    id TEXT PRIMARY KEY,
    system_id TEXT NOT NULL,
    check_id TEXT NOT NULL,
    entity_id TEXT,
    severity TEXT NOT NULL,
    category TEXT NOT NULL,
    status TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    remediation TEXT,
    first_observed_at TEXT NOT NULL,
    last_observed_at TEXT NOT NULL,
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    muted INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY(system_id) REFERENCES insights_systems(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_findings_system_status ON insights_findings(system_id, status, severity);

CREATE TABLE IF NOT EXISTS insights_finding_events (
    id TEXT PRIMARY KEY,
    finding_id TEXT NOT NULL,
    epoch_id TEXT,
    event_type TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    evidence_json TEXT,
    FOREIGN KEY(finding_id) REFERENCES insights_findings(id) ON DELETE CASCADE
);

CREATE VIEW IF NOT EXISTS v_insights_servers AS
SELECT system_id, epoch_id, entity_id, display_name, status, props_json, last_seen_at
FROM insights_entities WHERE entity_type = 'server';

CREATE VIEW IF NOT EXISTS v_insights_streams AS
SELECT system_id, epoch_id, entity_id, display_name, status, props_json, last_seen_at
FROM insights_entities WHERE entity_type = 'stream';

CREATE VIEW IF NOT EXISTS v_insights_findings_active AS
SELECT * FROM insights_findings WHERE status IN ('new', 'active', 'reopened');

-- +migrate Down
DROP VIEW IF EXISTS v_insights_findings_active;
DROP VIEW IF EXISTS v_insights_streams;
DROP VIEW IF EXISTS v_insights_servers;
DROP TABLE IF EXISTS insights_finding_events;
DROP TABLE IF EXISTS insights_findings;
DROP TABLE IF EXISTS insights_metrics;
DROP TABLE IF EXISTS insights_entities;
DROP TABLE IF EXISTS insights_epochs;
DROP TABLE IF EXISTS insights_systems;
