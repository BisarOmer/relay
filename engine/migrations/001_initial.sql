-- +migrate Up
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    broker_type TEXT NOT NULL,
    environment TEXT NOT NULL,
    environment_name TEXT,
    servers_json TEXT NOT NULL,
    auth_json TEXT NOT NULL,
    tls_json TEXT NOT NULL,
    timeout_ms INTEGER NOT NULL DEFAULT 10000,
    reconnect_json TEXT NOT NULL,
    tags_json TEXT,
    metadata_json TEXT,
    read_only INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_connected_at TEXT,
    last_status TEXT NOT NULL DEFAULT 'disconnected'
);

CREATE TABLE IF NOT EXISTS preferences (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS message_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    broker_type TEXT NOT NULL,
    connection_id TEXT,
    subject TEXT,
    headers_json TEXT,
    payload_base64 TEXT NOT NULL,
    content_type TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS saved_subscriptions (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    queue_group TEXT,
    mode TEXT NOT NULL DEFAULT 'observe',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_connections_broker ON connections(broker_type);
CREATE INDEX IF NOT EXISTS idx_templates_broker ON message_templates(broker_type);

-- +migrate Down
DROP TABLE IF EXISTS saved_subscriptions;
DROP TABLE IF EXISTS message_templates;
DROP TABLE IF EXISTS preferences;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS schema_migrations;
