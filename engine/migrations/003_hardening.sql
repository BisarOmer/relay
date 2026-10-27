-- +migrate Up
ALTER TABLE insights_systems ADD COLUMN last_error TEXT;
ALTER TABLE insights_systems ADD COLUMN last_success_at TEXT;

ALTER TABLE insights_findings ADD COLUMN resolved_at TEXT;
ALTER TABLE insights_findings ADD COLUMN acknowledged_at TEXT;
ALTER TABLE insights_findings ADD COLUMN last_epoch_id TEXT;
ALTER TABLE insights_findings ADD COLUMN steps_json TEXT;
ALTER TABLE insights_findings ADD COLUMN resolution TEXT;

CREATE INDEX IF NOT EXISTS idx_finding_events_finding ON insights_finding_events(finding_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_findings_key ON insights_findings(system_id, check_id, entity_id, status);
CREATE INDEX IF NOT EXISTS idx_metrics_series ON insights_metrics(system_id, entity_id, metric_name);

CREATE TABLE IF NOT EXISTS insights_saved_queries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    sql_text TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE VIEW IF NOT EXISTS v_insights_connections AS
SELECT system_id, epoch_id, entity_id, display_name, status, props_json, last_seen_at
FROM insights_entities WHERE entity_type = 'connection';

CREATE VIEW IF NOT EXISTS v_insights_consumers AS
SELECT system_id, epoch_id, entity_id, display_name, status, props_json, last_seen_at
FROM insights_entities WHERE entity_type = 'consumer';

-- +migrate Down
DROP VIEW IF EXISTS v_insights_consumers;
DROP VIEW IF EXISTS v_insights_connections;
DROP TABLE IF EXISTS insights_saved_queries;
DROP INDEX IF EXISTS idx_metrics_series;
DROP INDEX IF EXISTS idx_findings_key;
DROP INDEX IF EXISTS idx_finding_events_finding;
