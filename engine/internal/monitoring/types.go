package monitoring

import "time"

type EntityType string

const (
	EntitySystem     EntityType = "system"
	EntityCluster    EntityType = "cluster"
	EntityServer     EntityType = "server"
	EntityConnection EntityType = "connection"
	EntityAccount    EntityType = "account"
	EntityStream     EntityType = "stream"
	EntityConsumer   EntityType = "consumer"
	EntityRoute      EntityType = "route"
	EntityGateway    EntityType = "gateway"
	EntityLeaf       EntityType = "leaf"
)

type EndpointStatus struct {
	Endpoint   string `json:"endpoint"`
	OK         bool   `json:"ok"`
	HTTPCode   int    `json:"httpCode,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs"`
	// Available is false when the server does not expose the endpoint (404);
	// such endpoints do not count against completeness.
	Available        bool `json:"available"`
	PermissionDenied bool `json:"permissionDenied,omitempty"`
}

type SystemConfig struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	MonitoringBaseURL string     `json:"monitoringBaseUrl"`
	ConnectionID      string     `json:"connectionId,omitempty"`
	CollectIntervalMs int        `json:"collectIntervalMs"`
	RequestTimeoutMs  int        `json:"requestTimeoutMs"`
	RetentionEpochs   int        `json:"retentionEpochs"`
	Enabled           bool       `json:"enabled"`
	Demo              bool       `json:"demo"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	LastEpochID       string     `json:"lastEpochId,omitempty"`
	LastCollectedAt   *time.Time `json:"lastCollectedAt,omitempty"`
	LastSuccessAt     *time.Time `json:"lastSuccessAt,omitempty"`
	LastStatus        string     `json:"lastStatus"` // idle | collecting | ok | partial | unreachable | error
	LastError         string     `json:"lastError,omitempty"`
	// Running is runtime-only: whether a scheduled collection loop is active.
	Running bool `json:"running"`
}

type Entity struct {
	ID          string         `json:"id"`
	Type        EntityType     `json:"type"`
	SystemID    string         `json:"systemId"`
	NativeID    string         `json:"nativeId,omitempty"`
	DisplayName string         `json:"displayName"`
	ParentID    string         `json:"parentId,omitempty"`
	Status      string         `json:"status"`
	Props       map[string]any `json:"props"`
	Related     []string       `json:"related,omitempty"`
	FirstSeenAt *time.Time     `json:"firstSeenAt,omitempty"`
	LastSeenAt  time.Time      `json:"lastSeenAt"`
	Freshness   string         `json:"freshness"` // fresh | partial | stale
}

type MetricSample struct {
	EntityID  string   `json:"entityId"`
	Name      string   `json:"name"`
	Value     *float64 `json:"value"` // nil = unavailable
	Unit      string   `json:"unit,omitempty"`
	Available bool     `json:"available"`
}

type Epoch struct {
	ID             string           `json:"id"`
	SystemID       string           `json:"systemId"`
	CollectedAt    time.Time        `json:"collectedAt"`
	Completeness   float64          `json:"completeness"` // 0..1 of attempted endpoints that succeeded
	EndpointStatus []EndpointStatus `json:"endpointStatus"`
	ErrorCount     int              `json:"errorCount"`
	Entities       []Entity         `json:"entities,omitempty"`
	Metrics        []MetricSample   `json:"metrics,omitempty"`
}

// EndpointOK reports whether the endpoint was collected successfully in this epoch.
func (e *Epoch) EndpointOK(endpoint string) bool {
	for _, s := range e.EndpointStatus {
		if s.Endpoint == endpoint {
			return s.OK
		}
	}
	return false
}

type FindingStatus string

const (
	FindingNew      FindingStatus = "new"
	FindingActive   FindingStatus = "active"
	FindingResolved FindingStatus = "resolved"
	FindingReopened FindingStatus = "reopened"
)

type Finding struct {
	ID              string         `json:"id"`
	SystemID        string         `json:"systemId"`
	CheckID         string         `json:"checkId"`
	EntityID        string         `json:"entityId,omitempty"`
	Severity        string         `json:"severity"` // critical | warning | info
	Category        string         `json:"category"`
	Status          FindingStatus  `json:"status"`
	Title           string         `json:"title"`
	Summary         string         `json:"summary"`
	Evidence        map[string]any `json:"evidence"`
	Remediation     string         `json:"remediation,omitempty"`
	Steps           []string       `json:"steps,omitempty"`
	FirstObservedAt time.Time      `json:"firstObservedAt"`
	LastObservedAt  time.Time      `json:"lastObservedAt"`
	LastEpochID     string         `json:"lastEpochId,omitempty"`
	OccurrenceCount int            `json:"occurrenceCount"`
	Acknowledged    bool           `json:"acknowledged"`
	AcknowledgedAt  *time.Time     `json:"acknowledgedAt,omitempty"`
	ResolvedAt      *time.Time     `json:"resolvedAt,omitempty"`
	// Resolution explains how a finding was resolved: "condition_cleared" or "manual".
	Resolution string `json:"resolution,omitempty"`
	Muted      bool   `json:"muted"`
}

type FindingEvent struct {
	ID         string         `json:"id"`
	FindingID  string         `json:"findingId"`
	EpochID    string         `json:"epochId,omitempty"`
	EventType  string         `json:"eventType"`
	ObservedAt time.Time      `json:"observedAt"`
	Evidence   map[string]any `json:"evidence,omitempty"`
}

type Overview struct {
	SystemID     string     `json:"systemId"`
	SystemName   string     `json:"systemName"`
	EpochID      string     `json:"epochId,omitempty"`
	CollectedAt  *time.Time `json:"collectedAt,omitempty"`
	LastSuccess  *time.Time `json:"lastSuccessAt,omitempty"`
	IntervalMs   int        `json:"intervalMs"`
	Enabled      bool       `json:"enabled"`
	Status       string     `json:"status"`
	LastError    string     `json:"lastError,omitempty"`
	Completeness float64    `json:"completeness"`
	// Counts are nil when the endpoint that reports them was not collected.
	Servers          *int             `json:"servers"`
	Connections      *int             `json:"connections"`
	Streams          *int             `json:"streams"`
	Consumers        *int             `json:"consumers"`
	SlowConsumers    *int             `json:"slowConsumers"`
	Subscriptions    *float64         `json:"subscriptions"`
	InMsgs           *float64         `json:"inMsgs"`
	OutMsgs          *float64         `json:"outMsgs"`
	CPU              *float64         `json:"cpu"`
	MemBytes         *float64         `json:"memBytes"`
	JetStream        string           `json:"jetstream"` // enabled | disabled | unknown
	ServerEntityID   string           `json:"serverEntityId,omitempty"`
	Version          string           `json:"version,omitempty"`
	CriticalFindings int              `json:"criticalFindings"`
	WarningFindings  int              `json:"warningFindings"`
	InfoFindings     int              `json:"infoFindings"`
	CollectionErrors int              `json:"collectionErrors"`
	EndpointStatus   []EndpointStatus `json:"endpointStatus"`
	Partial          bool             `json:"partial"`
	Demo             bool             `json:"demo"`
	Notes            []string         `json:"notes"`
}

type DiffChange struct {
	EntityID    string `json:"entityId"`
	EntityType  string `json:"entityType"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind"` // added | removed | missing_or_removed | changed
	Field       string `json:"field,omitempty"`
	Before      any    `json:"before,omitempty"`
	After       any    `json:"after,omitempty"`
}

type EpochDiff struct {
	SystemID   string       `json:"systemId"`
	FromEpoch  string       `json:"fromEpoch"`
	ToEpoch    string       `json:"toEpoch"`
	FromAt     time.Time    `json:"fromAt"`
	ToAt       time.Time    `json:"toAt"`
	Changes    []DiffChange `json:"changes"`
	Incomplete bool         `json:"incomplete"`
	Notes      []string     `json:"notes"`
}

type SeriesPoint struct {
	EpochID     string    `json:"epochId"`
	CollectedAt time.Time `json:"collectedAt"`
	Value       *float64  `json:"value"`
}

type SavedQuery struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SQL       string    `json:"sql"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
