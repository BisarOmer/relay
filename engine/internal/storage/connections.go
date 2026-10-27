package storage

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
)

type ConnectionRepo struct {
	db *DB
}

func NewConnectionRepo(db *DB) *ConnectionRepo {
	return &ConnectionRepo{db: db}
}

func (r *ConnectionRepo) List() ([]broker.ConnectionProfile, error) {
	rows, err := r.db.SQL.Query(`SELECT id, name, broker_type, environment, environment_name, servers_json, auth_json, tls_json,
		timeout_ms, reconnect_json, tags_json, metadata_json, read_only, created_at, updated_at, last_connected_at, last_status
		FROM connections ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []broker.ConnectionProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *ConnectionRepo) Get(id string) (*broker.ConnectionProfile, error) {
	row := r.db.SQL.QueryRow(`SELECT id, name, broker_type, environment, environment_name, servers_json, auth_json, tls_json,
		timeout_ms, reconnect_json, tags_json, metadata_json, read_only, created_at, updated_at, last_connected_at, last_status
		FROM connections WHERE id = ?`, id)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		return nil, broker.NewError(broker.ErrNotFound, "connection not found", false)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ConnectionRepo) Upsert(p broker.ConnectionProfile) error {
	servers, _ := json.Marshal(p.Servers)
	auth := p.Auth
	auth.EphemeralSecret = "" // never persist
	authJSON, _ := json.Marshal(auth)
	tlsJSON, _ := json.Marshal(p.TLS)
	reconnect, _ := json.Marshal(p.Reconnect)
	tags, _ := json.Marshal(p.Tags)
	meta, _ := json.Marshal(p.Metadata)
	var lastConnected any
	if p.LastConnectedAt != nil {
		lastConnected = p.LastConnectedAt.UTC().Format(time.RFC3339Nano)
	}
	readOnly := 0
	if p.ReadOnly {
		readOnly = 1
	}
	_, err := r.db.SQL.Exec(`INSERT INTO connections (
		id, name, broker_type, environment, environment_name, servers_json, auth_json, tls_json,
		timeout_ms, reconnect_json, tags_json, metadata_json, read_only, created_at, updated_at, last_connected_at, last_status
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, broker_type=excluded.broker_type, environment=excluded.environment,
		environment_name=excluded.environment_name, servers_json=excluded.servers_json, auth_json=excluded.auth_json,
		tls_json=excluded.tls_json, timeout_ms=excluded.timeout_ms, reconnect_json=excluded.reconnect_json,
		tags_json=excluded.tags_json, metadata_json=excluded.metadata_json, read_only=excluded.read_only,
		updated_at=excluded.updated_at, last_connected_at=excluded.last_connected_at, last_status=excluded.last_status
	`, p.ID, p.Name, string(p.BrokerType), string(p.Environment), nullStr(p.EnvironmentName),
		string(servers), string(authJSON), string(tlsJSON), p.TimeoutMs, string(reconnect),
		string(tags), string(meta), readOnly,
		p.CreatedAt.UTC().Format(time.RFC3339Nano),
		p.UpdatedAt.UTC().Format(time.RFC3339Nano),
		lastConnected, string(p.LastStatus))
	return err
}

func (r *ConnectionRepo) Delete(id string) error {
	res, err := r.db.SQL.Exec(`DELETE FROM connections WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return broker.NewError(broker.ErrNotFound, "connection not found", false)
	}
	return nil
}

func (r *ConnectionRepo) UpdateStatus(id string, status broker.ConnectionStatus, lastConnected *time.Time) error {
	var lc any
	if lastConnected != nil {
		lc = lastConnected.UTC().Format(time.RFC3339Nano)
	}
	_, err := r.db.SQL.Exec(`UPDATE connections SET last_status = ?, last_connected_at = COALESCE(?, last_connected_at), updated_at = ? WHERE id = ?`,
		string(status), lc, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

// ResetRuntimeStatuses clears statuses left over from a previous engine run;
// no session survives a restart, so anything but "disconnected"/"error" is stale.
func (r *ConnectionRepo) ResetRuntimeStatuses() error {
	_, err := r.db.SQL.Exec(`UPDATE connections SET last_status = 'disconnected'
		WHERE last_status IN ('connecting','connected','reconnecting','disconnecting')`)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (broker.ConnectionProfile, error) {
	var p broker.ConnectionProfile
	var envName sql.NullString
	var serversJSON, authJSON, tlsJSON, reconnectJSON, tagsJSON, metaJSON string
	var readOnly int
	var createdAt, updatedAt string
	var lastConnected sql.NullString
	var lastStatus string
	var brokerType, environment string
	err := row.Scan(&p.ID, &p.Name, &brokerType, &environment, &envName, &serversJSON, &authJSON, &tlsJSON,
		&p.TimeoutMs, &reconnectJSON, &tagsJSON, &metaJSON, &readOnly, &createdAt, &updatedAt, &lastConnected, &lastStatus)
	if err != nil {
		return p, err
	}
	p.BrokerType = broker.BrokerType(brokerType)
	p.Environment = broker.EnvironmentKind(environment)
	if envName.Valid {
		p.EnvironmentName = envName.String
	}
	_ = json.Unmarshal([]byte(serversJSON), &p.Servers)
	_ = json.Unmarshal([]byte(authJSON), &p.Auth)
	_ = json.Unmarshal([]byte(tlsJSON), &p.TLS)
	_ = json.Unmarshal([]byte(reconnectJSON), &p.Reconnect)
	if tagsJSON != "" {
		_ = json.Unmarshal([]byte(tagsJSON), &p.Tags)
	}
	if metaJSON != "" {
		_ = json.Unmarshal([]byte(metaJSON), &p.Metadata)
	}
	p.ReadOnly = readOnly == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if lastConnected.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastConnected.String)
		if err == nil {
			p.LastConnectedAt = &t
		}
	}
	p.LastStatus = broker.ConnectionStatus(lastStatus)
	return p, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
