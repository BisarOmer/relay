package monitoring

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/security"
)

type EventEmitter func(event string, payload any)

const (
	MinCollectInterval = time.Second
	MaxCollectInterval = time.Hour
	MaxFailureBackoff  = 2 * time.Minute
)

type Manager struct {
	store *Store
	emit  EventEmitter
	mu    sync.Mutex
	loops map[string]context.CancelFunc
	// collecting serializes collections per system so scheduled and manual
	// runs never overlap.
	collecting sync.Map // systemID -> *sync.Mutex
	wg         sync.WaitGroup
}

func NewManager(store *Store, emit EventEmitter) *Manager {
	return &Manager{store: store, emit: emit, loops: make(map[string]context.CancelFunc)}
}

func (m *Manager) ListSystems() ([]SystemConfig, error)       { return m.store.ListSystems() }
func (m *Manager) GetSystem(id string) (*SystemConfig, error) { return m.store.GetSystem(id) }
func (m *Manager) ListSavedQueries() ([]SavedQuery, error)    { return m.store.ListSavedQueries() }
func (m *Manager) DeleteSavedQuery(id string) error           { return m.store.DeleteSavedQuery(id) }
func (m *Manager) FindingEvents(id string) ([]FindingEvent, error) {
	return m.store.FindingEvents(id, 100)
}

// ValidateSystem normalizes defaults and rejects invalid configuration.
func ValidateSystem(c *SystemConfig) error {
	c.Name = strings.TrimSpace(c.Name)
	c.MonitoringBaseURL = strings.TrimRight(strings.TrimSpace(c.MonitoringBaseURL), "/")
	if c.Name == "" {
		return broker.NewError(broker.ErrValidation, "name is required", false)
	}
	if len(c.Name) > 120 {
		return broker.NewError(broker.ErrValidation, "name must be at most 120 characters", false)
	}
	if !c.Demo {
		if err := security.ValidateEndpointURL(c.MonitoringBaseURL, "http", "https"); err != nil {
			return broker.NewError(broker.ErrValidation, "monitoring URL: "+err.Error(), false)
		}
	}
	if c.CollectIntervalMs == 0 {
		c.CollectIntervalMs = 5000
	}
	if c.RequestTimeoutMs == 0 {
		c.RequestTimeoutMs = 3000
	}
	if c.RetentionEpochs == 0 {
		c.RetentionEpochs = 2880
	}
	if d := time.Duration(c.CollectIntervalMs) * time.Millisecond; d < MinCollectInterval || d > MaxCollectInterval {
		return broker.NewError(broker.ErrValidation, "collection interval must be between 1 second and 1 hour", false)
	}
	if c.RequestTimeoutMs < 250 || c.RequestTimeoutMs > 60000 {
		return broker.NewError(broker.ErrValidation, "request timeout must be between 250 ms and 60 s", false)
	}
	if c.RequestTimeoutMs > c.CollectIntervalMs {
		return broker.NewError(broker.ErrValidation, "request timeout must not exceed the collection interval", false)
	}
	if c.RetentionEpochs < 10 || c.RetentionEpochs > 100000 {
		return broker.NewError(broker.ErrValidation, "retention must be between 10 and 100000 snapshots", false)
	}
	return nil
}

func (m *Manager) UpsertSystem(c SystemConfig) (*SystemConfig, error) {
	if err := ValidateSystem(&c); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
		c.CreatedAt = now
		c.LastStatus = "idle"
	} else {
		existing, err := m.store.GetSystem(c.ID)
		if err != nil {
			return nil, err
		}
		c.CreatedAt = existing.CreatedAt
	}
	c.UpdatedAt = now
	if err := m.store.UpsertSystem(c); err != nil {
		return nil, err
	}
	if c.Enabled {
		m.Start(c.ID)
	} else {
		m.Stop(c.ID)
	}
	return m.store.GetSystem(c.ID)
}

func (m *Manager) DeleteSystem(id string) error {
	m.Stop(id)
	return m.store.DeleteSystem(id)
}

func (m *Manager) Start(systemID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.loops[systemID]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.loops[systemID] = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.loop(ctx, systemID)
	}()
}

func (m *Manager) Stop(systemID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.loops[systemID]; ok {
		cancel()
		delete(m.loops, systemID)
	}
}

// Running reports whether a scheduled collection loop exists for the system.
func (m *Manager) Running(systemID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.loops[systemID]
	return ok
}

func (m *Manager) StartEnabled() {
	systems, err := m.store.ListSystems()
	if err != nil {
		return
	}
	for _, s := range systems {
		if s.Enabled {
			m.Start(s.ID)
		}
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	for id, cancel := range m.loops {
		cancel()
		delete(m.loops, id)
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (m *Manager) loop(ctx context.Context, systemID string) {
	failures := 0
	for {
		sys, err := m.store.GetSystem(systemID)
		if err != nil || !sys.Enabled {
			m.mu.Lock()
			if ctx.Err() == nil {
				delete(m.loops, systemID)
			}
			m.mu.Unlock()
			return
		}
		ep, err := m.CollectOnce(ctx, systemID)
		if ctx.Err() != nil {
			return
		}
		if err != nil || ep == nil || ep.Completeness == 0 {
			failures++
		} else {
			failures = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(nextDelay(time.Duration(sys.CollectIntervalMs)*time.Millisecond, failures)):
		}
	}
}

// nextDelay backs off exponentially while the target is unreachable so a
// down server is not polled at full rate.
func nextDelay(interval time.Duration, failures int) time.Duration {
	if failures <= 1 {
		return interval
	}
	d := interval
	for i := 1; i < failures && d < MaxFailureBackoff; i++ {
		d *= 2
	}
	if d > MaxFailureBackoff && interval < MaxFailureBackoff {
		d = MaxFailureBackoff
	}
	if d < interval {
		d = interval
	}
	return d
}

func (m *Manager) lockFor(systemID string) *sync.Mutex {
	v, _ := m.collecting.LoadOrStore(systemID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *Manager) CollectOnce(ctx context.Context, systemID string) (*Epoch, error) {
	lock := m.lockFor(systemID)
	lock.Lock()
	defer lock.Unlock()

	sys, err := m.store.GetSystem(systemID)
	if err != nil {
		return nil, err
	}
	_ = m.store.SetStatus(sys.ID, "collecting", sys.LastError)
	m.emitStatus(sys.ID, "collecting", "")

	var ep *Epoch
	if sys.Demo {
		ep = SimulateEpoch(*sys)
	} else {
		timeout := time.Duration(sys.RequestTimeoutMs) * time.Millisecond
		col := NewCollector(timeout)
		cctx, cancel := context.WithTimeout(ctx, 4*timeout+2*time.Second)
		raw, err := col.Collect(cctx, sys.MonitoringBaseURL)
		cancel()
		if err != nil {
			_ = m.store.SetStatus(sys.ID, "error", err.Error())
			m.emitStatus(sys.ID, "error", err.Error())
			return nil, broker.Wrap(broker.ErrValidation, "collection failed", false, err)
		}
		ep = Normalize(*sys, raw)
	}

	var prev *Epoch
	if sys.LastEpochID != "" {
		prev, _ = m.store.LoadEpoch(sys.ID, sys.LastEpochID, true)
	}
	carryFirstSeen(ep, prev)

	if err := m.store.SaveEpoch(ep); err != nil {
		_ = m.store.SetStatus(sys.ID, "error", "failed to save snapshot: "+err.Error())
		return nil, broker.Wrap(broker.ErrInternal, "failed to save snapshot", false, err)
	}
	_ = m.store.TrimEpochs(sys.ID, sys.RetentionEpochs)

	open, _ := m.store.ListFindings(sys.ID, false)
	findings, evaluated := EvaluateAll(EvalContext{Epoch: ep, Previous: prev, Open: open})
	active := map[string]bool{}
	for _, f := range findings {
		active[f.CheckID+"|"+f.EntityID] = true
	}
	known := map[string]bool{}
	for _, c := range Registry() {
		known[c.ID] = true
	}
	if err := m.store.UpsertFindings(findings, ep.ID); err != nil {
		return nil, err
	}
	if err := m.store.ResolveCleared(sys.ID, ep.ID, active, evaluated, known); err != nil {
		return nil, err
	}

	status, lastErr := EpochStatus(ep)
	if m.emit != nil {
		m.emit("insights.epoch", map[string]any{
			"systemId": sys.ID, "epochId": ep.ID, "completeness": ep.Completeness,
			"collectedAt": ep.CollectedAt, "status": status, "error": lastErr,
		})
	}
	return ep, nil
}

func (m *Manager) emitStatus(systemID, status, detail string) {
	if m.emit != nil {
		m.emit("insights.status", map[string]any{"systemId": systemID, "status": status, "error": detail})
	}
}

// carryFirstSeen preserves when each entity was first observed.
func carryFirstSeen(ep, prev *Epoch) {
	firstSeen := map[string]time.Time{}
	if prev != nil {
		for _, e := range prev.Entities {
			if e.FirstSeenAt != nil {
				firstSeen[e.ID] = *e.FirstSeenAt
			} else {
				firstSeen[e.ID] = e.LastSeenAt
			}
		}
	}
	for i := range ep.Entities {
		t, ok := firstSeen[ep.Entities[i].ID]
		if !ok {
			t = ep.CollectedAt
		}
		ep.Entities[i].FirstSeenAt = &t
	}
}

func (m *Manager) Overview(systemID string) (*Overview, error) {
	sys, err := m.store.GetSystem(systemID)
	if err != nil {
		return nil, err
	}
	ov := &Overview{
		SystemID: systemID, SystemName: sys.Name, Demo: sys.Demo, Notes: []string{},
		IntervalMs: sys.CollectIntervalMs, Enabled: sys.Enabled, Status: sys.LastStatus,
		LastError: sys.LastError, LastSuccess: sys.LastSuccessAt, JetStream: "unknown",
		EndpointStatus: []EndpointStatus{},
	}
	if sys.LastEpochID == "" {
		return ov, nil
	}
	ep, err := m.store.LoadEpoch(systemID, sys.LastEpochID, true)
	if err != nil {
		return nil, err
	}
	ov.EpochID = ep.ID
	t := ep.CollectedAt
	ov.CollectedAt = &t
	ov.Completeness = ep.Completeness
	ov.EndpointStatus = ep.EndpointStatus
	ov.CollectionErrors = ep.ErrorCount
	ov.Partial = ep.Completeness > 0 && ep.Completeness < 0.99
	if ov.Partial {
		ov.Notes = append(ov.Notes, "This snapshot is partial: values from failed endpoints are shown as unavailable, not zero.")
	}

	sysEntityID := "system:" + systemID
	count := func(t EntityType) int {
		n := 0
		for _, e := range ep.Entities {
			if e.Type == t {
				n++
			}
		}
		return n
	}
	intPtr := func(v *float64) *int {
		if v == nil {
			return nil
		}
		n := int(*v)
		return &n
	}
	if ep.EndpointOK("/varz") {
		n := count(EntityServer)
		ov.Servers = &n
		if srv := firstEntity(ep, EntityServer); srv != nil {
			ov.ServerEntityID = srv.ID
			ov.Version = str(srv.Props["version"])
			ov.Connections = intPtr(metricValue(ep, srv.ID, "connections"))
			ov.SlowConsumers = intPtr(metricValue(ep, srv.ID, "slow_consumers"))
			ov.InMsgs = metricValue(ep, srv.ID, "in_msgs")
			ov.OutMsgs = metricValue(ep, srv.ID, "out_msgs")
			ov.CPU = metricValue(ep, srv.ID, "cpu")
			ov.MemBytes = metricValue(ep, srv.ID, "mem")
			ov.Subscriptions = metricValue(ep, srv.ID, "subscriptions")
		}
	}
	if sysEnt := entityByID(ep, sysEntityID); sysEnt != nil {
		if js := str(sysEnt.Props["jetstream"]); js != "" {
			ov.JetStream = js
		}
		if t, _ := sysEnt.Props["connections_truncated"].(bool); t {
			ov.Notes = append(ov.Notes, fmt.Sprintf("Connection list truncated: %v of %v connections listed.", sysEnt.Props["connections_listed"], sysEnt.Props["connections_total"]))
		}
	}
	if srv := firstEntity(ep, EntityServer); srv != nil {
		if t, _ := srv.Props["connections_truncated"].(bool); t {
			ov.Notes = append(ov.Notes, fmt.Sprintf("Connection list truncated: %v of %v connections listed.", srv.Props["connections_listed"], srv.Props["connections_total"]))
		}
	}
	if ov.JetStream == "enabled" {
		ov.Streams = intPtr(metricValue(ep, sysEntityID, "js_streams"))
		ov.Consumers = intPtr(metricValue(ep, sysEntityID, "js_consumers"))
	} else if ov.JetStream == "disabled" {
		ov.Notes = append(ov.Notes, "JetStream is disabled on this server.")
	}
	if ep.Completeness == 0 {
		ov.Notes = append(ov.Notes, "The latest collection could not reach any monitoring endpoint; values are unavailable.")
	}

	findings, _ := m.store.ListFindings(systemID, false)
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			ov.CriticalFindings++
		case "warning":
			ov.WarningFindings++
		default:
			ov.InfoFindings++
		}
	}
	return ov, nil
}

func (m *Manager) resolveEpoch(systemID, epochID string) (string, error) {
	sys, err := m.store.GetSystem(systemID)
	if err != nil {
		return "", err
	}
	if epochID == "" {
		epochID = sys.LastEpochID
	}
	return epochID, nil
}

func (m *Manager) Topology(systemID, epochID string) (*Epoch, error) {
	epochID, err := m.resolveEpoch(systemID, epochID)
	if err != nil {
		return nil, err
	}
	if epochID == "" {
		return &Epoch{SystemID: systemID, Entities: []Entity{}, EndpointStatus: []EndpointStatus{}}, nil
	}
	ep, err := m.store.LoadEpoch(systemID, epochID, true)
	if err != nil {
		return nil, err
	}
	if ep.Entities == nil {
		ep.Entities = []Entity{}
	}
	return ep, nil
}

type EntityDetail struct {
	Entity   *Entity        `json:"entity"`
	Metrics  []MetricSample `json:"metrics"`
	Children []Entity       `json:"children"`
	Related  []RelatedRef   `json:"related"`
	Path     []Entity       `json:"path"`
	Findings []Finding      `json:"findings"`
	EpochID  string         `json:"epochId"`
	At       time.Time      `json:"collectedAt"`
}

// RelatedRef is a relationship reported by the server. InSnapshot is false
// when the target was not collected in this snapshot (e.g. a route peer that
// is monitored separately or not at all).
type RelatedRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Type        string `json:"type,omitempty"`
	InSnapshot  bool   `json:"inSnapshot"`
}

func (m *Manager) GetEntity(systemID, epochID, entityID string) (*EntityDetail, error) {
	epochID, err := m.resolveEpoch(systemID, epochID)
	if err != nil {
		return nil, err
	}
	if epochID == "" {
		return nil, broker.NewError(broker.ErrNotFound, "no snapshot collected yet", false)
	}
	ep, err := m.store.LoadEpoch(systemID, epochID, true)
	if err != nil {
		return nil, err
	}
	ent := entityByID(ep, entityID)
	if ent == nil {
		return nil, broker.NewError(broker.ErrNotFound, "entity not present in this snapshot", false)
	}
	d := &EntityDetail{Entity: ent, Metrics: []MetricSample{}, Children: []Entity{}, Related: []RelatedRef{}, Path: []Entity{}, Findings: []Finding{}, EpochID: ep.ID, At: ep.CollectedAt}
	for _, mt := range ep.Metrics {
		if mt.EntityID == entityID {
			d.Metrics = append(d.Metrics, mt)
		}
	}
	for _, e := range ep.Entities {
		if e.ParentID == entityID {
			d.Children = append(d.Children, e)
		}
		for _, r := range e.Related {
			if r == entityID {
				d.Related = append(d.Related, RelatedRef{ID: e.ID, DisplayName: e.DisplayName, Type: string(e.Type), InSnapshot: true})
			}
		}
	}
	for _, r := range ent.Related {
		ref := RelatedRef{ID: r}
		if t := entityByID(ep, r); t != nil {
			ref.DisplayName, ref.Type, ref.InSnapshot = t.DisplayName, string(t.Type), true
		}
		d.Related = append(d.Related, ref)
	}
	seen := map[string]bool{entityID: true}
	for p := entityByID(ep, ent.ParentID); p != nil && !seen[p.ID]; p = entityByID(ep, p.ParentID) {
		seen[p.ID] = true
		d.Path = append([]Entity{*p}, d.Path...)
	}
	findings, _ := m.store.ListFindings(systemID, false)
	for _, f := range findings {
		if f.EntityID == entityID {
			d.Findings = append(d.Findings, f)
		}
	}
	return d, nil
}

func (m *Manager) ListEpochs(systemID string, limit int) ([]Epoch, error) {
	if _, err := m.store.GetSystem(systemID); err != nil {
		return nil, err
	}
	return m.store.ListEpochs(systemID, limit)
}

func (m *Manager) Series(systemID, entityID, metric string, limit int) ([]SeriesPoint, error) {
	if entityID == "" || metric == "" {
		return nil, broker.NewError(broker.ErrValidation, "entityId and metric are required", false)
	}
	return m.store.Series(systemID, entityID, metric, limit)
}

var diffMetrics = map[EntityType][]string{
	EntityServer:   {"connections", "slow_consumers", "subscriptions"},
	EntityStream:   {"messages", "bytes", "consumer_count"},
	EntityConsumer: {"num_pending", "num_ack_pending", "num_redelivered"},
	EntityRoute:    {"pooled_connections"},
}

func (m *Manager) Diff(systemID, fromEpoch, toEpoch string) (*EpochDiff, error) {
	if fromEpoch == "" || toEpoch == "" {
		return nil, broker.NewError(broker.ErrValidation, "fromEpoch and toEpoch are required", false)
	}
	from, err := m.store.LoadEpoch(systemID, fromEpoch, true)
	if err != nil {
		return nil, err
	}
	to, err := m.store.LoadEpoch(systemID, toEpoch, true)
	if err != nil {
		return nil, err
	}
	if from.CollectedAt.After(to.CollectedAt) {
		from, to = to, from
	}
	diff := &EpochDiff{SystemID: systemID, FromEpoch: from.ID, ToEpoch: to.ID, FromAt: from.CollectedAt, ToAt: to.CollectedAt, Changes: []DiffChange{}, Notes: []string{}}
	missingEndpoints := map[string]bool{}
	for _, s := range from.EndpointStatus {
		if s.Available && (!s.OK || !to.EndpointOK(s.Endpoint)) {
			missingEndpoints[s.Endpoint] = true
		}
	}
	if len(missingEndpoints) > 0 {
		diff.Incomplete = true
		names := make([]string, 0, len(missingEndpoints))
		for n := range missingEndpoints {
			names = append(names, n)
		}
		diff.Notes = append(diff.Notes, fmt.Sprintf("Endpoints %s were not collected in both snapshots; entities that depend on them may appear removed only because data is missing.", strings.Join(names, ", ")))
	}
	fromMap := map[string]Entity{}
	for _, e := range from.Entities {
		fromMap[e.ID] = e
	}
	toMap := map[string]Entity{}
	for _, e := range to.Entities {
		toMap[e.ID] = e
	}
	for _, e := range to.Entities {
		if _, ok := fromMap[e.ID]; !ok {
			diff.Changes = append(diff.Changes, DiffChange{EntityID: e.ID, EntityType: string(e.Type), DisplayName: e.DisplayName, Kind: "added"})
		}
	}
	for _, e := range from.Entities {
		t, ok := toMap[e.ID]
		if !ok {
			kind := "removed"
			if diff.Incomplete {
				kind = "missing_or_removed"
			}
			diff.Changes = append(diff.Changes, DiffChange{EntityID: e.ID, EntityType: string(e.Type), DisplayName: e.DisplayName, Kind: kind})
			continue
		}
		if t.Status != e.Status {
			diff.Changes = append(diff.Changes, DiffChange{EntityID: e.ID, EntityType: string(e.Type), DisplayName: e.DisplayName,
				Kind: "changed", Field: "status", Before: e.Status, After: t.Status})
		}
		for _, name := range diffMetrics[e.Type] {
			a, b := metricValue(from, e.ID, name), metricValue(to, e.ID, name)
			if a != nil && b != nil && *a != *b {
				diff.Changes = append(diff.Changes, DiffChange{EntityID: e.ID, EntityType: string(e.Type), DisplayName: e.DisplayName,
					Kind: "changed", Field: name, Before: *a, After: *b})
			}
		}
	}
	return diff, nil
}

func (m *Manager) ListFindings(systemID string, includeResolved bool) ([]Finding, error) {
	return m.store.ListFindings(systemID, includeResolved)
}

func (m *Manager) SetAcknowledged(id string, ack bool) error { return m.store.SetAcknowledged(id, ack) }
func (m *Manager) ResolveFinding(id string) error            { return m.store.ResolveManually(id) }

func (m *Manager) Query(ctx context.Context, systemID, sqlText string, limit, timeoutMs int) (*QueryResult, error) {
	return m.store.Query(ctx, systemID, sqlText, limit, time.Duration(timeoutMs)*time.Millisecond)
}

func (m *Manager) SaveQuery(q SavedQuery) (*SavedQuery, error) {
	q.Name = strings.TrimSpace(q.Name)
	if q.Name == "" || len(q.Name) > 120 {
		return nil, broker.NewError(broker.ErrValidation, "query name is required (max 120 characters)", false)
	}
	if _, err := normalizeQuery(q.SQL); err != nil {
		return nil, err
	}
	return m.store.SaveQuery(q)
}
