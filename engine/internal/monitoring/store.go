package monitoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/storage"
)

type Store struct {
	db *storage.DB
}

func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

const systemColumns = `id, name, monitoring_base_url, connection_id, collect_interval_ms, request_timeout_ms,
	retention_epochs, enabled, demo, created_at, updated_at, last_epoch_id, last_collected_at, last_status,
	last_error, last_success_at`

func (s *Store) ListSystems() ([]SystemConfig, error) {
	rows, err := s.db.SQL.Query(`SELECT ` + systemColumns + ` FROM insights_systems ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SystemConfig
	for rows.Next() {
		c, err := scanSystem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetSystem(id string) (*SystemConfig, error) {
	row := s.db.SQL.QueryRow(`SELECT `+systemColumns+` FROM insights_systems WHERE id = ?`, id)
	c, err := scanSystem(row)
	if err == sql.ErrNoRows {
		return nil, broker.NewError(broker.ErrNotFound, "insights system not found", false)
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertSystem writes user-editable configuration only. Collection state
// (last_* columns) is owned by SetStatus/SaveEpoch so a config edit can never
// overwrite fresher collection results, and vice versa.
func (s *Store) UpsertSystem(c SystemConfig) error {
	_, err := s.db.SQL.Exec(`INSERT INTO insights_systems (
		id, name, monitoring_base_url, connection_id, collect_interval_ms, request_timeout_ms,
		retention_epochs, enabled, demo, created_at, updated_at, last_status
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, monitoring_base_url=excluded.monitoring_base_url, connection_id=excluded.connection_id,
		collect_interval_ms=excluded.collect_interval_ms, request_timeout_ms=excluded.request_timeout_ms,
		retention_epochs=excluded.retention_epochs, enabled=excluded.enabled, demo=excluded.demo,
		updated_at=excluded.updated_at`,
		c.ID, c.Name, c.MonitoringBaseURL, nullEmpty(c.ConnectionID), c.CollectIntervalMs, c.RequestTimeoutMs,
		c.RetentionEpochs, boolInt(c.Enabled), boolInt(c.Demo),
		c.CreatedAt.UTC().Format(time.RFC3339Nano), c.UpdatedAt.UTC().Format(time.RFC3339Nano), nonEmptyStr(c.LastStatus, "idle"),
	)
	return err
}

// SetStatus updates only the collection status columns.
func (s *Store) SetStatus(id, status, lastErr string) error {
	_, err := s.db.SQL.Exec(`UPDATE insights_systems SET last_status = ?, last_error = ? WHERE id = ?`,
		status, nullEmpty(lastErr), id)
	return err
}

func (s *Store) DeleteSystem(id string) error {
	res, err := s.db.SQL.Exec(`DELETE FROM insights_systems WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return broker.NewError(broker.ErrNotFound, "insights system not found", false)
	}
	return nil
}

// EpochStatus summarizes an epoch for the system row.
func EpochStatus(ep *Epoch) (status, lastErr string) {
	switch {
	case ep.Completeness == 0:
		return "unreachable", firstError(endpointErrors(ep))
	case ep.Completeness < 0.99:
		return "partial", ""
	default:
		return "ok", ""
	}
}

// SaveEpoch persists an epoch atomically together with the system's
// collection status.
func (s *Store) SaveEpoch(ep *Epoch) error {
	statusJSON, _ := json.Marshal(ep.EndpointStatus)
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`INSERT INTO insights_epochs (id, system_id, collected_at, completeness, endpoint_status_json, error_count)
		VALUES (?,?,?,?,?,?)`, ep.ID, ep.SystemID, ep.CollectedAt.UTC().Format(time.RFC3339Nano),
		ep.Completeness, string(statusJSON), ep.ErrorCount); err != nil {
		return err
	}
	entStmt, err := tx.Prepare(`INSERT INTO insights_entities (
		system_id, epoch_id, entity_id, entity_type, native_id, display_name, parent_id, status,
		props_json, related_json, first_seen_at, last_seen_at, freshness
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer entStmt.Close()
	for _, e := range ep.Entities {
		props, _ := json.Marshal(e.Props)
		related, _ := json.Marshal(e.Related)
		var first any
		if e.FirstSeenAt != nil {
			first = e.FirstSeenAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err := entStmt.Exec(e.SystemID, ep.ID, e.ID, string(e.Type), nullEmpty(e.NativeID), e.DisplayName, nullEmpty(e.ParentID),
			e.Status, string(props), string(related), first, e.LastSeenAt.UTC().Format(time.RFC3339Nano), e.Freshness,
		); err != nil {
			return err
		}
	}
	metStmt, err := tx.Prepare(`INSERT INTO insights_metrics (system_id, epoch_id, entity_id, metric_name, metric_value, unit, available)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	defer metStmt.Close()
	for _, m := range ep.Metrics {
		var val any
		if m.Value != nil {
			val = *m.Value
		}
		if _, err := metStmt.Exec(ep.SystemID, ep.ID, m.EntityID, m.Name, val, m.Unit, boolInt(m.Available)); err != nil {
			return err
		}
	}
	now := ep.CollectedAt.UTC().Format(time.RFC3339Nano)
	status, lastErr := EpochStatus(ep)
	if _, err := tx.Exec(`UPDATE insights_systems SET last_epoch_id=?, last_collected_at=?, last_status=?, last_error=?,
		last_success_at = CASE WHEN ? > 0 THEN ? ELSE last_success_at END WHERE id=?`,
		ep.ID, now, status, nullEmpty(lastErr), ep.Completeness, now, ep.SystemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TrimEpochs(systemID string, keep int) error {
	if keep <= 0 {
		keep = 2880
	}
	_, err := s.db.SQL.Exec(`DELETE FROM insights_epochs WHERE system_id = ? AND id NOT IN (
		SELECT id FROM insights_epochs WHERE system_id = ? ORDER BY collected_at DESC LIMIT ?
	)`, systemID, systemID, keep)
	return err
}

func (s *Store) ListEpochs(systemID string, limit int) ([]Epoch, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := s.db.SQL.Query(`SELECT id, system_id, collected_at, completeness, endpoint_status_json, error_count
		FROM insights_epochs WHERE system_id = ? ORDER BY collected_at DESC LIMIT ?`, systemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Epoch
	for rows.Next() {
		var e Epoch
		var ts, statusJSON string
		if err := rows.Scan(&e.ID, &e.SystemID, &ts, &e.Completeness, &statusJSON, &e.ErrorCount); err != nil {
			return nil, err
		}
		e.CollectedAt, _ = time.Parse(time.RFC3339Nano, ts)
		_ = json.Unmarshal([]byte(statusJSON), &e.EndpointStatus)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PreviousEpochID returns the epoch collected immediately before epochID.
func (s *Store) PreviousEpochID(systemID, epochID string) (string, error) {
	var id string
	err := s.db.SQL.QueryRow(`SELECT id FROM insights_epochs WHERE system_id = ? AND collected_at <
		(SELECT collected_at FROM insights_epochs WHERE id = ?) ORDER BY collected_at DESC LIMIT 1`, systemID, epochID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Store) LoadEpoch(systemID, epochID string, withEntities bool) (*Epoch, error) {
	row := s.db.SQL.QueryRow(`SELECT id, system_id, collected_at, completeness, endpoint_status_json, error_count
		FROM insights_epochs WHERE system_id = ? AND id = ?`, systemID, epochID)
	var e Epoch
	var ts, statusJSON string
	if err := row.Scan(&e.ID, &e.SystemID, &ts, &e.Completeness, &statusJSON, &e.ErrorCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, broker.NewError(broker.ErrNotFound, "snapshot not found (it may have been removed by retention)", false)
		}
		return nil, err
	}
	e.CollectedAt, _ = time.Parse(time.RFC3339Nano, ts)
	_ = json.Unmarshal([]byte(statusJSON), &e.EndpointStatus)
	if !withEntities {
		return &e, nil
	}
	erows, err := s.db.SQL.Query(`SELECT entity_id, entity_type, native_id, display_name, parent_id, status, props_json, related_json, first_seen_at, last_seen_at, freshness
		FROM insights_entities WHERE system_id = ? AND epoch_id = ?`, systemID, epochID)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var ent Entity
		var native, parent, first, related sql.NullString
		var propsJSON, last string
		var etype string
		if err := erows.Scan(&ent.ID, &etype, &native, &ent.DisplayName, &parent, &ent.Status, &propsJSON, &related, &first, &last, &ent.Freshness); err != nil {
			return nil, err
		}
		ent.Type = EntityType(etype)
		ent.SystemID = systemID
		ent.NativeID = native.String
		ent.ParentID = parent.String
		if json.Unmarshal([]byte(propsJSON), &ent.Props) != nil || ent.Props == nil {
			ent.Props = map[string]any{}
		}
		if related.Valid {
			_ = json.Unmarshal([]byte(related.String), &ent.Related)
		}
		ent.LastSeenAt, _ = time.Parse(time.RFC3339Nano, last)
		if first.Valid {
			if t, err := time.Parse(time.RFC3339Nano, first.String); err == nil {
				ent.FirstSeenAt = &t
			}
		}
		e.Entities = append(e.Entities, ent)
	}
	if err := erows.Err(); err != nil {
		return nil, err
	}
	mrows, err := s.db.SQL.Query(`SELECT entity_id, metric_name, metric_value, unit, available FROM insights_metrics WHERE system_id=? AND epoch_id=?`, systemID, epochID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var m MetricSample
		var val sql.NullFloat64
		var unit sql.NullString
		var avail int
		if err := mrows.Scan(&m.EntityID, &m.Name, &val, &unit, &avail); err != nil {
			return nil, err
		}
		m.Unit = unit.String
		m.Available = avail == 1
		if val.Valid {
			v := val.Float64
			m.Value = &v
		}
		e.Metrics = append(e.Metrics, m)
	}
	return &e, mrows.Err()
}

// Series returns one point per epoch for the most recent epochs, oldest
// first, including epochs where the metric was not collected (Value nil) so
// gaps stay visible.
func (s *Store) Series(systemID, entityID, metric string, limit int) ([]SeriesPoint, error) {
	if limit <= 0 || limit > 2000 {
		limit = 120
	}
	rows, err := s.db.SQL.Query(`SELECT e.id, e.collected_at, m.metric_value, IFNULL(m.available, 0)
		FROM insights_epochs e
		LEFT JOIN insights_metrics m ON m.system_id = e.system_id AND m.epoch_id = e.id AND m.entity_id = ? AND m.metric_name = ?
		WHERE e.system_id = ? ORDER BY e.collected_at DESC LIMIT ?`, entityID, metric, systemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeriesPoint{}
	for rows.Next() {
		var p SeriesPoint
		var ts string
		var val sql.NullFloat64
		var avail int
		if err := rows.Scan(&p.EpochID, &ts, &val, &avail); err != nil {
			return nil, err
		}
		p.CollectedAt, _ = time.Parse(time.RFC3339Nano, ts)
		if val.Valid && avail == 1 {
			v := val.Float64
			p.Value = &v
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Reverse(out)
	return out, nil
}

const findingColumns = `id, system_id, check_id, entity_id, severity, category, status, title, summary, evidence_json, remediation,
	first_observed_at, last_observed_at, occurrence_count, acknowledged, muted, resolved_at, acknowledged_at, last_epoch_id, steps_json, resolution`

func findingKeyQuery(statusClause string) string {
	return `SELECT id, status, occurrence_count, severity FROM insights_findings
		WHERE system_id=? AND check_id=? AND IFNULL(entity_id,'')=IFNULL(?, '') AND ` + statusClause + `
		ORDER BY last_observed_at DESC LIMIT 1`
}

// UpsertFindings records the findings emitted for an epoch. An open finding
// with the same (check, entity) key is updated in place; a previously resolved
// one is reopened (preserving its history); otherwise a new finding opens.
// Only lifecycle transitions are written to the event log.
func (s *Store) UpsertFindings(findings []Finding, epochID string) error {
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, f := range findings {
		evidence, _ := json.Marshal(f.Evidence)
		steps, _ := json.Marshal(f.Steps)
		observed := f.LastObservedAt.UTC().Format(time.RFC3339Nano)

		var id, status, severity string
		var occ int
		err := tx.QueryRow(findingKeyQuery(`status IN ('new','active','reopened')`), f.SystemID, f.CheckID, nullEmpty(f.EntityID)).
			Scan(&id, &status, &occ, &severity)
		switch {
		case err == nil:
			if _, err := tx.Exec(`UPDATE insights_findings SET status=?, last_observed_at=?, occurrence_count=?, evidence_json=?,
				summary=?, title=?, severity=?, remediation=?, steps_json=?, last_epoch_id=? WHERE id=?`,
				nextOpenStatus(FindingStatus(status)), observed, occ+1, string(evidence), f.Summary, f.Title, f.Severity,
				f.Remediation, string(steps), epochID, id); err != nil {
				return err
			}
			if severity != f.Severity {
				if err := insertEvent(tx, id, epochID, "severity_changed", observed, map[string]any{"from": severity, "to": f.Severity}); err != nil {
					return err
				}
			}
			continue
		case err != sql.ErrNoRows:
			return err
		}

		err = tx.QueryRow(findingKeyQuery(`status = 'resolved'`), f.SystemID, f.CheckID, nullEmpty(f.EntityID)).
			Scan(&id, &status, &occ, &severity)
		switch {
		case err == nil:
			if _, err := tx.Exec(`UPDATE insights_findings SET status=?, last_observed_at=?, occurrence_count=?, evidence_json=?,
				summary=?, title=?, severity=?, remediation=?, steps_json=?, last_epoch_id=?, resolved_at=NULL, resolution=NULL,
				acknowledged=0, acknowledged_at=NULL WHERE id=?`,
				string(FindingReopened), observed, occ+1, string(evidence), f.Summary, f.Title, f.Severity,
				f.Remediation, string(steps), epochID, id); err != nil {
				return err
			}
			if err := insertEvent(tx, id, epochID, "reopened", observed, f.Evidence); err != nil {
				return err
			}
			continue
		case err != sql.ErrNoRows:
			return err
		}

		id = uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO insights_findings (
			id, system_id, check_id, entity_id, severity, category, status, title, summary, evidence_json, remediation,
			first_observed_at, last_observed_at, occurrence_count, acknowledged, muted, last_epoch_id, steps_json
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,1,0,0,?,?)`,
			id, f.SystemID, f.CheckID, nullEmpty(f.EntityID), f.Severity, f.Category, string(FindingNew),
			f.Title, f.Summary, string(evidence), f.Remediation,
			f.FirstObservedAt.UTC().Format(time.RFC3339Nano), observed, epochID, string(steps),
		); err != nil {
			return err
		}
		if err := insertEvent(tx, id, epochID, "opened", observed, f.Evidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nextOpenStatus(cur FindingStatus) FindingStatus {
	if cur == FindingNew {
		return FindingActive
	}
	return cur
}

func insertEvent(tx *sql.Tx, findingID, epochID, eventType, observed string, evidence map[string]any) error {
	var ev any
	if evidence != nil {
		b, _ := json.Marshal(evidence)
		ev = string(b)
	}
	_, err := tx.Exec(`INSERT INTO insights_finding_events (id, finding_id, epoch_id, event_type, observed_at, evidence_json)
		VALUES (?,?,?,?,?,?)`, uuid.NewString(), findingID, nullEmpty(epochID), eventType, observed, ev)
	return err
}

// ResolveCleared resolves open findings that were not re-emitted, but only
// for checks whose inputs were collected in this epoch (evaluated). Findings
// from checks that no longer exist are retired.
func (s *Store) ResolveCleared(systemID, epochID string, active map[string]bool, evaluated map[string]bool, known map[string]bool) error {
	rows, err := s.db.SQL.Query(`SELECT id, check_id, IFNULL(entity_id,'') FROM insights_findings
		WHERE system_id=? AND status IN ('new','active','reopened')`, systemID)
	if err != nil {
		return err
	}
	type row struct{ id, checkID, entityID, resolution string }
	var toResolve []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.checkID, &r.entityID); err != nil {
			rows.Close()
			return err
		}
		switch {
		case !known[r.checkID]:
			r.resolution = "check_retired"
		case evaluated[r.checkID] && !active[r.checkID+"|"+r.entityID]:
			r.resolution = "condition_cleared"
		default:
			continue
		}
		toResolve = append(toResolve, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(toResolve) == 0 {
		return nil
	}
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, r := range toResolve {
		if _, err := tx.Exec(`UPDATE insights_findings SET status=?, resolved_at=?, resolution=? WHERE id=?`,
			string(FindingResolved), now, r.resolution, r.id); err != nil {
			return err
		}
		if err := insertEvent(tx, r.id, epochID, "resolved", now, map[string]any{"resolution": r.resolution}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListFindings(systemID string, includeResolved bool) ([]Finding, error) {
	q := `SELECT ` + findingColumns + ` FROM insights_findings WHERE system_id=?`
	if !includeResolved {
		q += ` AND status IN ('new','active','reopened')`
	}
	q += ` ORDER BY CASE WHEN status='resolved' THEN 1 ELSE 0 END,
		CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, last_observed_at DESC LIMIT 1000`
	rows, err := s.db.SQL.Query(q, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetFinding(id string) (*Finding, error) {
	f, err := scanFinding(s.db.SQL.QueryRow(`SELECT `+findingColumns+` FROM insights_findings WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, broker.NewError(broker.ErrNotFound, "finding not found", false)
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func scanFinding(row scannable) (Finding, error) {
	var f Finding
	var entity, remediation, resolvedAt, ackAt, lastEpoch, steps, resolution sql.NullString
	var evidence, first, last, status string
	var ack, muted int
	if err := row.Scan(&f.ID, &f.SystemID, &f.CheckID, &entity, &f.Severity, &f.Category, &status, &f.Title, &f.Summary,
		&evidence, &remediation, &first, &last, &f.OccurrenceCount, &ack, &muted, &resolvedAt, &ackAt, &lastEpoch, &steps, &resolution); err != nil {
		return f, err
	}
	f.Status = FindingStatus(status)
	f.EntityID = entity.String
	f.Remediation = remediation.String
	f.LastEpochID = lastEpoch.String
	f.Resolution = resolution.String
	_ = json.Unmarshal([]byte(evidence), &f.Evidence)
	if steps.Valid {
		_ = json.Unmarshal([]byte(steps.String), &f.Steps)
	}
	f.FirstObservedAt, _ = time.Parse(time.RFC3339Nano, first)
	f.LastObservedAt, _ = time.Parse(time.RFC3339Nano, last)
	f.ResolvedAt = parseTimePtr(resolvedAt)
	f.AcknowledgedAt = parseTimePtr(ackAt)
	f.Acknowledged = ack == 1
	f.Muted = muted == 1
	return f, nil
}

func (s *Store) SetAcknowledged(id string, ack bool) error {
	var at any
	event := "unacknowledged"
	if ack {
		at = time.Now().UTC().Format(time.RFC3339Nano)
		event = "acknowledged"
	}
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE insights_findings SET acknowledged=?, acknowledged_at=? WHERE id=?`, boolInt(ack), at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return broker.NewError(broker.ErrNotFound, "finding not found", false)
	}
	if err := insertEvent(tx, id, "", event, time.Now().UTC().Format(time.RFC3339Nano), nil); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveManually marks an open finding resolved. If the condition is still
// present at the next collection it is reopened automatically.
func (s *Store) ResolveManually(id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE insights_findings SET status='resolved', resolved_at=?, resolution='manual'
		WHERE id=? AND status IN ('new','active','reopened')`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return broker.NewError(broker.ErrValidation, "finding is not open", false)
	}
	if err := insertEvent(tx, id, "", "resolved_manually", now, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FindingEvents(findingID string, limit int) ([]FindingEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.SQL.Query(`SELECT id, finding_id, epoch_id, event_type, observed_at, evidence_json
		FROM insights_finding_events WHERE finding_id=? ORDER BY observed_at DESC LIMIT ?`, findingID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FindingEvent{}
	for rows.Next() {
		var e FindingEvent
		var epoch, ev sql.NullString
		var ts string
		if err := rows.Scan(&e.ID, &e.FindingID, &epoch, &e.EventType, &ts, &ev); err != nil {
			return nil, err
		}
		e.EpochID = epoch.String
		e.ObservedAt, _ = time.Parse(time.RFC3339Nano, ts)
		if ev.Valid {
			_ = json.Unmarshal([]byte(ev.String), &e.Evidence)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Query(ctx context.Context, systemID, sqlText string, limit int, timeout time.Duration) (*QueryResult, error) {
	return RunReadOnlyQuery(ctx, s.db.ReadOnly, systemID, sqlText, limit, timeout)
}

func (s *Store) ListSavedQueries() ([]SavedQuery, error) {
	rows, err := s.db.SQL.Query(`SELECT id, name, sql_text, created_at, updated_at FROM insights_saved_queries ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedQuery{}
	for rows.Next() {
		var q SavedQuery
		var c, u string
		if err := rows.Scan(&q.ID, &q.Name, &q.SQL, &c, &u); err != nil {
			return nil, err
		}
		q.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		q.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) SaveQuery(q SavedQuery) (*SavedQuery, error) {
	now := time.Now().UTC()
	if q.ID == "" {
		q.ID = uuid.NewString()
		q.CreatedAt = now
	}
	q.UpdatedAt = now
	_, err := s.db.SQL.Exec(`INSERT INTO insights_saved_queries (id, name, sql_text, created_at, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, sql_text=excluded.sql_text, updated_at=excluded.updated_at`,
		q.ID, q.Name, q.SQL, q.CreatedAt.Format(time.RFC3339Nano), q.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func (s *Store) DeleteSavedQuery(id string) error {
	res, err := s.db.SQL.Exec(`DELETE FROM insights_saved_queries WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return broker.NewError(broker.ErrNotFound, "saved query not found", false)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSystem(row scannable) (SystemConfig, error) {
	var c SystemConfig
	var connID, lastEpoch, lastCollected, lastErr, lastSuccess sql.NullString
	var created, updated string
	var enabled, demo int
	err := row.Scan(&c.ID, &c.Name, &c.MonitoringBaseURL, &connID, &c.CollectIntervalMs, &c.RequestTimeoutMs,
		&c.RetentionEpochs, &enabled, &demo, &created, &updated, &lastEpoch, &lastCollected, &c.LastStatus, &lastErr, &lastSuccess)
	if err != nil {
		return c, err
	}
	c.Enabled = enabled == 1
	c.Demo = demo == 1
	c.ConnectionID = connID.String
	c.LastEpochID = lastEpoch.String
	c.LastError = lastErr.String
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	c.LastCollectedAt = parseTimePtr(lastCollected)
	c.LastSuccessAt = parseTimePtr(lastSuccess)
	return c, nil
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
