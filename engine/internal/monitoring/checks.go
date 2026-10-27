package monitoring

import (
	"fmt"
	"sort"
	"strings"
)

// EvalContext is the input to a check: the current epoch and, when one
// exists, the epoch collected immediately before it.
type EvalContext struct {
	Epoch    *Epoch
	Previous *Epoch
	// Open holds the system's currently open findings, for rules whose
	// condition persists beyond the snapshot in which it was first detected.
	Open []Finding
}

// Check is a diagnostic rule. Requires lists the monitoring endpoints that
// must have been collected (in the current epoch, and in the previous one when
// NeedsPrevious is set) for the rule's verdict to be meaningful. When they
// were not collected the rule is "not evaluable": it emits nothing and any
// open findings it owns are left untouched instead of being auto-resolved.
type Check struct {
	ID            string
	Name          string
	Description   string
	Category      string
	Severity      string
	Threshold     string
	Requires      []string
	NeedsPrevious bool
	Evaluate      func(c EvalContext) []Finding
}

// Documented thresholds. See docs/diagnostics.md.
const (
	PendingBytesThreshold      = 1 << 20 // 1 MiB of unsent data buffered for one client
	AckPendingAbsolute         = 1000
	AckPendingRatio            = 0.8
	ConnectionCapacityWarning  = 0.8
	ConnectionCapacityCritical = 0.95
	StorageWarning             = 0.8
	StorageCritical            = 0.95
	BacklogGrowthMin           = 10000
)

func Registry() []Check {
	return []Check{
		monitoringUnreachable(),
		healthzFailure(),
		partialCollection(),
		permissionDenied(),
		serverRestarted(),
		slowConsumerIncrease(),
		highPendingBytes(),
		connectionCapacity(),
		routeLost(),
		gatewayDegraded(),
		ackPendingBuildup(),
		consumerBacklogGrowing(),
		jetstreamResourcePressure(),
	}
}

// Evaluable reports whether a check's inputs were collected.
func (c Check) Evaluable(ctx EvalContext) bool {
	if c.NeedsPrevious && ctx.Previous == nil {
		return false
	}
	for _, ep := range c.Requires {
		if !ctx.Epoch.EndpointOK(ep) {
			return false
		}
		if c.NeedsPrevious && !ctx.Previous.EndpointOK(ep) {
			return false
		}
		if ep == "/connz" && connzTruncated(ctx.Epoch) {
			return false
		}
	}
	return true
}

// EvaluateAll runs every evaluable check. It returns the findings and the set
// of check IDs whose verdict is authoritative for this epoch.
func EvaluateAll(ctx EvalContext) ([]Finding, map[string]bool) {
	evaluated := map[string]bool{}
	var out []Finding
	for _, c := range Registry() {
		if !c.Evaluable(ctx) {
			continue
		}
		evaluated[c.ID] = true
		for _, f := range c.Evaluate(ctx) {
			f.SystemID = ctx.Epoch.SystemID
			f.CheckID = c.ID
			f.Category = c.Category
			if f.Severity == "" {
				f.Severity = c.Severity
			}
			if f.Evidence == nil {
				f.Evidence = map[string]any{}
			}
			if c.Threshold != "" {
				f.Evidence["threshold"] = c.Threshold
			}
			f.Evidence["epochId"] = ctx.Epoch.ID
			f.FirstObservedAt = ctx.Epoch.CollectedAt
			f.LastObservedAt = ctx.Epoch.CollectedAt
			f.LastEpochID = ctx.Epoch.ID
			out = append(out, f)
		}
	}
	return out, evaluated
}

func monitoringUnreachable() Check {
	return Check{
		ID: "availability.monitoring_unreachable", Name: "Monitoring endpoint unreachable",
		Description: "No monitoring endpoint answered during collection.",
		Category:    "availability", Severity: "critical",
		Evaluate: func(c EvalContext) []Finding {
			ep := c.Epoch
			if ep.Completeness > 0 || len(ep.EndpointStatus) == 0 {
				return nil
			}
			errs := endpointErrors(ep)
			return []Finding{{
				EntityID:    "system:" + ep.SystemID,
				Title:       "Monitoring endpoint unreachable",
				Summary:     fmt.Sprintf("None of %d monitoring endpoints responded (%s).", len(errs), firstError(errs)),
				Evidence:    map[string]any{"endpointErrors": errs},
				Remediation: "Confirm the server is running and that its HTTP monitoring port is reachable from this machine.",
				Steps: []string{
					"Check that the NATS server process is running.",
					"Verify the monitoring URL (usually http://host:8222) and that the server was started with -m / http_port.",
					"Test reachability from this machine, e.g. curl <monitoring-url>/healthz.",
					"Check firewalls or proxies between this machine and the server.",
				},
			}}
		},
	}
}

func healthzFailure() Check {
	return Check{
		ID: "availability.healthz_failure", Name: "Health check failing",
		Description: "The server's /healthz endpoint answered but reported a problem.",
		Category:    "availability", Severity: "critical",
		Evaluate: func(c EvalContext) []Finding {
			ep := c.Epoch
			if ep.Completeness == 0 {
				return nil // covered by monitoring_unreachable
			}
			for _, s := range ep.EndpointStatus {
				if s.Endpoint != "/healthz" || !s.Available || s.OK || s.PermissionDenied {
					continue
				}
				entity := "system:" + ep.SystemID
				if srv := firstEntity(ep, EntityServer); srv != nil {
					entity = srv.ID
				}
				return []Finding{{
					EntityID:    entity,
					Title:       "Health check failing",
					Summary:     fmt.Sprintf("/healthz reported: %s.", nonEmptyStr(s.Error, fmt.Sprintf("HTTP %d", s.HTTPCode))),
					Evidence:    map[string]any{"endpoint": s},
					Remediation: "Inspect the server log for the failing subsystem (often JetStream storage or meta-leader election).",
					Steps: []string{
						"Read the /healthz error text above; it names the failing subsystem.",
						"Check the NATS server log around the detection time.",
						"If JetStream is involved, verify disk space and that a meta leader exists (nats server report jetstream).",
					},
				}}
			}
			return nil
		},
	}
}

func partialCollection() Check {
	return Check{
		ID: "infrastructure.partial_collection", Name: "Partial monitoring collection",
		Description: "Some monitoring endpoints failed, so parts of this snapshot are missing.",
		Category:    "infrastructure", Severity: "warning",
		Evaluate: func(c EvalContext) []Finding {
			ep := c.Epoch
			if ep.Completeness == 0 || ep.Completeness >= 0.99 {
				return nil
			}
			errs := endpointErrors(ep)
			if len(errs) == 0 {
				return nil
			}
			names := make([]string, 0, len(errs))
			for n := range errs {
				names = append(names, n)
			}
			sort.Strings(names)
			return []Finding{{
				EntityID:    "system:" + ep.SystemID,
				Title:       "Partial monitoring data",
				Summary:     fmt.Sprintf("%s failed; data from these endpoints is missing (not zero).", strings.Join(names, ", ")),
				Evidence:    map[string]any{"completeness": ep.Completeness, "endpointErrors": errs},
				Remediation: "Investigate why the listed endpoints fail; diagnostics that depend on them are paused.",
				Steps: []string{
					"Open the endpoint coverage list on the Overview to see each error.",
					"Large deployments can exceed the request timeout on /connz or /jsz; raise the timeout in system settings.",
				},
			}}
		},
	}
}

func permissionDenied() Check {
	return Check{
		ID: "infrastructure.permission_denied", Name: "Monitoring permission denied",
		Description: "An endpoint answered 401/403, typically from a proxy in front of the monitoring port.",
		Category:    "infrastructure", Severity: "warning",
		Evaluate: func(c EvalContext) []Finding {
			var denied []string
			for _, s := range c.Epoch.EndpointStatus {
				if s.PermissionDenied {
					denied = append(denied, s.Endpoint)
				}
			}
			if len(denied) == 0 {
				return nil
			}
			return []Finding{{
				EntityID:    "system:" + c.Epoch.SystemID,
				Title:       "Monitoring access denied",
				Summary:     fmt.Sprintf("Access was denied for %s.", strings.Join(denied, ", ")),
				Evidence:    map[string]any{"endpoints": denied},
				Remediation: "Grant read access to these monitoring paths, or point Insights at an unauthenticated monitoring listener.",
			}}
		},
	}
}

func serverRestarted() Check {
	return Check{
		ID: "availability.server_restarted", Name: "Server restarted",
		Description: "The server start time changed between consecutive snapshots.",
		Category:    "availability", Severity: "info",
		Requires: []string{"/varz"}, NeedsPrevious: true,
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, cur := range entitiesOf(c.Epoch, EntityServer) {
				prev := entityByID(c.Previous, cur.ID)
				if prev == nil {
					continue
				}
				a, b := str(prev.Props["start"]), str(cur.Props["start"])
				if a == "" || b == "" || a == b {
					continue
				}
				out = append(out, Finding{
					EntityID: cur.ID,
					Title:    "Server restarted",
					Summary:  fmt.Sprintf("%s restarted (start time %s → %s). Lifetime counters were reset.", cur.DisplayName, a, b),
					Evidence: map[string]any{"previousStart": a, "currentStart": b, "previousEpochId": c.Previous.ID},
					Steps:    []string{"Check whether the restart was planned (deploy, config reload with restart).", "Review the server log for crash output before the new start time."},
				})
			}
			return out
		},
	}
}

func slowConsumerIncrease() Check {
	return Check{
		ID: "connections.slow_consumers", Name: "Slow consumer disconnects",
		Description: "The server's slow_consumers counter increased since the previous snapshot.",
		Category:    "connections", Severity: "warning",
		Threshold: "any increase between consecutive snapshots",
		Requires:  []string{"/varz"}, NeedsPrevious: true,
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, srv := range entitiesOf(c.Epoch, EntityServer) {
				cur := metricValue(c.Epoch, srv.ID, "slow_consumers")
				prev := metricValue(c.Previous, srv.ID, "slow_consumers")
				if cur == nil || prev == nil || *cur <= *prev {
					continue // no increase, or counter reset on restart
				}
				delta := *cur - *prev
				window := c.Epoch.CollectedAt.Sub(c.Previous.CollectedAt).Seconds()
				out = append(out, Finding{
					EntityID:    srv.ID,
					Title:       "Slow consumers disconnected",
					Summary:     fmt.Sprintf("%.0f client(s) were disconnected as slow consumers in the last %.0fs (lifetime total %.0f).", delta, window, *cur),
					Evidence:    map[string]any{"previous": *prev, "current": *cur, "delta": delta, "windowSeconds": window},
					Remediation: "Find clients that cannot keep up with their subscriptions and scale them or reduce fan-out.",
					Steps: []string{
						"Sort Connections by pending bytes to find clients close to the limit.",
						"Compare in/out message rates for the server on the Overview chart.",
						"Consider queue groups to spread load, or JetStream for consumers that need buffering.",
					},
				})
			}
			return out
		},
	}
}

func highPendingBytes() Check {
	return Check{
		ID: "connections.high_pending_bytes", Name: "High pending bytes",
		Description: "A client connection has a large outbound buffer of data it has not yet read.",
		Category:    "connections", Severity: "warning",
		Threshold: "pending_bytes ≥ 1 MiB",
		Requires:  []string{"/connz"},
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, m := range c.Epoch.Metrics {
				if m.Name != "pending_bytes" || !strings.HasPrefix(m.EntityID, "connection:") || m.Value == nil || *m.Value < PendingBytesThreshold {
					continue
				}
				name := m.EntityID
				if e := entityByID(c.Epoch, m.EntityID); e != nil {
					name = e.DisplayName
				}
				out = append(out, Finding{
					EntityID:    m.EntityID,
					Title:       "Client falling behind",
					Summary:     fmt.Sprintf("%s has %s pending delivery.", name, humanBytes(*m.Value)),
					Evidence:    map[string]any{"pending_bytes": *m.Value},
					Remediation: "The client is not reading fast enough; if the buffer reaches max_pending it will be disconnected as a slow consumer.",
					Steps:       []string{"Check the client's processing latency and CPU.", "Check network RTT for the connection.", "Reduce subscription fan-out or use a queue group."},
				})
			}
			return out
		},
	}
}

func connectionCapacity() Check {
	return Check{
		ID: "connections.capacity", Name: "Connection capacity",
		Description: "Active connections approach the server's max_connections limit.",
		Category:    "connections", Severity: "warning",
		Threshold: "≥ 80% of max_connections (critical ≥ 95%)",
		Requires:  []string{"/varz"},
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, srv := range entitiesOf(c.Epoch, EntityServer) {
				cur := metricValue(c.Epoch, srv.ID, "connections")
				max := metricValue(c.Epoch, srv.ID, "max_connections")
				if cur == nil || max == nil || *max <= 0 {
					continue
				}
				ratio := *cur / *max
				if ratio < ConnectionCapacityWarning {
					continue
				}
				sev := "warning"
				if ratio >= ConnectionCapacityCritical {
					sev = "critical"
				}
				out = append(out, Finding{
					EntityID: srv.ID, Severity: sev,
					Title:       "Connection limit nearly reached",
					Summary:     fmt.Sprintf("%s has %.0f of %.0f allowed connections (%.0f%%).", srv.DisplayName, *cur, *max, ratio*100),
					Evidence:    map[string]any{"connections": *cur, "max_connections": *max, "ratio": ratio},
					Remediation: "New clients will be rejected at the limit. Raise max_connections or add servers.",
				})
			}
			return out
		},
	}
}

func routeLost() Check {
	return Check{
		ID: "cluster.route_lost", Name: "Cluster route lost",
		Description: "A route to a cluster peer present in the previous snapshot is gone.",
		Category:    "cluster", Severity: "warning",
		Requires: []string{"/routez"}, NeedsPrevious: true,
		Evaluate: func(c EvalContext) []Finding {
			steps := []string{"Check whether the peer server is running.", "Check network connectivity on the cluster port.", "Look for route errors in both servers' logs."}
			var out []Finding
			seen := map[string]bool{}
			for _, prev := range entitiesOf(c.Previous, EntityRoute) {
				if entityByID(c.Epoch, prev.ID) != nil {
					continue
				}
				seen[prev.ID] = true
				out = append(out, Finding{
					EntityID: prev.ID,
					Title:    "Route to cluster peer lost",
					Summary:  fmt.Sprintf("%s was present at %s and is missing now.", prev.DisplayName, c.Previous.CollectedAt.UTC().Format("15:04:05Z")),
					Evidence: map[string]any{"route": prev.Props, "lastSeenEpochId": c.Previous.ID, "lastSeenAt": c.Previous.CollectedAt},
					Steps:    steps,
				})
			}
			// Keep a loss open until the route comes back, not just for one cycle.
			for _, f := range c.Open {
				if f.CheckID != "cluster.route_lost" || seen[f.EntityID] || entityByID(c.Epoch, f.EntityID) != nil {
					continue
				}
				out = append(out, Finding{EntityID: f.EntityID, Title: f.Title, Summary: f.Summary, Evidence: f.Evidence, Steps: steps})
			}
			return out
		},
	}
}

func gatewayDegraded() Check {
	return Check{
		ID: "cluster.gateway_degraded", Name: "Gateway one-directional",
		Description: "A gateway peer has only inbound or only outbound connections.",
		Category:    "cluster", Severity: "warning",
		Requires: []string{"/gatewayz"},
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, g := range entitiesOf(c.Epoch, EntityGateway) {
				if g.Status != "degraded" {
					continue
				}
				out = append(out, Finding{
					EntityID: g.ID,
					Title:    "Gateway connection is one-directional",
					Summary:  fmt.Sprintf("%s has %v outbound and %v inbound connections; gateways need both.", g.DisplayName, g.Props["outbound_connections"], g.Props["inbound_connections"]),
					Evidence: map[string]any{"gateway": g.Props},
					Steps:    []string{"Verify the remote cluster can reach this server's gateway listener.", "Check gateway URLs and authorization on both sides."},
				})
			}
			return out
		},
	}
}

func ackPendingBuildup() Check {
	return Check{
		ID: "jetstream.ack_pending_buildup", Name: "Consumer ack pending buildup",
		Description: "A JetStream consumer has many delivered-but-unacknowledged messages.",
		Category:    "jetstream", Severity: "warning",
		Threshold: "num_ack_pending ≥ 80% of max_ack_pending, or ≥ 1000 when unlimited",
		Requires:  []string{"/jsz"},
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, e := range entitiesOf(c.Epoch, EntityConsumer) {
				v := metricValue(c.Epoch, e.ID, "num_ack_pending")
				if v == nil {
					continue
				}
				limit := 0.0
				if cfg, ok := e.Props["config"].(map[string]any); ok {
					if m := numPtr(cfg["max_ack_pending"]); m != nil && *m > 0 {
						limit = *m
					}
				}
				fire := false
				if limit > 0 {
					fire = *v >= limit*AckPendingRatio
				} else {
					fire = *v >= AckPendingAbsolute
				}
				if !fire {
					continue
				}
				ev := map[string]any{"num_ack_pending": *v}
				summary := fmt.Sprintf("%s has %.0f unacknowledged messages.", e.DisplayName, *v)
				if limit > 0 {
					ev["max_ack_pending"] = limit
					summary = fmt.Sprintf("%s has %.0f of %.0f allowed unacknowledged messages; delivery stops at the limit.", e.DisplayName, *v, limit)
				}
				out = append(out, Finding{
					EntityID:    e.ID,
					Title:       "Ack pending buildup",
					Summary:     summary,
					Evidence:    ev,
					Remediation: "Consumers are receiving messages faster than they acknowledge them.",
					Steps:       []string{"Check consumer processing latency and errors.", "Confirm the application acks messages (explicit ack policy).", "Scale out consumer instances."},
				})
			}
			return out
		},
	}
}

func consumerBacklogGrowing() Check {
	return Check{
		ID: "jetstream.backlog_growing", Name: "Consumer backlog growing",
		Description: "A consumer's unprocessed backlog is large and grew since the previous snapshot.",
		Category:    "jetstream", Severity: "warning",
		Threshold: "num_pending ≥ 10000 and increasing",
		Requires:  []string{"/jsz"}, NeedsPrevious: true,
		Evaluate: func(c EvalContext) []Finding {
			var out []Finding
			for _, e := range entitiesOf(c.Epoch, EntityConsumer) {
				cur := metricValue(c.Epoch, e.ID, "num_pending")
				prev := metricValue(c.Previous, e.ID, "num_pending")
				if cur == nil || prev == nil || *cur < BacklogGrowthMin || *cur <= *prev {
					continue
				}
				out = append(out, Finding{
					EntityID: e.ID,
					Title:    "Consumer backlog growing",
					Summary:  fmt.Sprintf("%s backlog grew from %.0f to %.0f messages.", e.DisplayName, *prev, *cur),
					Evidence: map[string]any{"previous": *prev, "current": *cur, "previousEpochId": c.Previous.ID},
					Steps:    []string{"Compare the stream's inbound rate with the consumer's delivery rate.", "Check whether consumer instances are running."},
				})
			}
			return out
		},
	}
}

func jetstreamResourcePressure() Check {
	return Check{
		ID: "jetstream.resource_pressure", Name: "JetStream resource pressure",
		Description: "JetStream storage or memory usage approaches the server's configured limit.",
		Category:    "jetstream", Severity: "warning",
		Threshold: "≥ 80% of max_storage / max_memory (critical ≥ 95%)",
		Requires:  []string{"/jsz"},
		Evaluate: func(c EvalContext) []Finding {
			sysID := "system:" + c.Epoch.SystemID
			var out []Finding
			for _, kind := range []struct{ used, max, label string }{
				{"js_storage", "js_max_storage", "storage"},
				{"js_memory", "js_max_memory", "memory"},
			} {
				used := metricValue(c.Epoch, sysID, kind.used)
				max := metricValue(c.Epoch, sysID, kind.max)
				if used == nil || max == nil || *max <= 0 {
					continue
				}
				ratio := *used / *max
				if ratio < StorageWarning {
					continue
				}
				sev := "warning"
				if ratio >= StorageCritical {
					sev = "critical"
				}
				out = append(out, Finding{
					EntityID: sysID, Severity: sev,
					Title:    "JetStream " + kind.label + " nearly full",
					Summary:  fmt.Sprintf("JetStream %s uses %s of %s (%.0f%%).", kind.label, humanBytes(*used), humanBytes(*max), ratio*100),
					Evidence: map[string]any{"used": *used, "max": *max, "ratio": ratio, "resource": kind.label},
					Steps:    []string{"Find the largest streams in the JetStream view.", "Review stream retention limits (max_age, max_bytes).", "Increase the server's JetStream limits or add storage."},
				})
			}
			return out
		},
	}
}

func endpointErrors(ep *Epoch) map[string]string {
	out := map[string]string{}
	for _, s := range ep.EndpointStatus {
		if s.Available && !s.OK {
			out[s.Endpoint] = nonEmptyStr(s.Error, "failed")
		}
	}
	return out
}

func firstError(errs map[string]string) string {
	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		return errs[k]
	}
	return "no response"
}

func entitiesOf(ep *Epoch, t EntityType) []Entity {
	var out []Entity
	for _, e := range ep.Entities {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func firstEntity(ep *Epoch, t EntityType) *Entity {
	for i := range ep.Entities {
		if ep.Entities[i].Type == t {
			return &ep.Entities[i]
		}
	}
	return nil
}

func entityByID(ep *Epoch, id string) *Entity {
	if ep == nil {
		return nil
	}
	for i := range ep.Entities {
		if ep.Entities[i].ID == id {
			return &ep.Entities[i]
		}
	}
	return nil
}

func metricValue(ep *Epoch, entityID, name string) *float64 {
	if ep == nil {
		return nil
	}
	for _, m := range ep.Metrics {
		if m.EntityID == entityID && m.Name == name && m.Available {
			return m.Value
		}
	}
	return nil
}

func connzTruncated(ep *Epoch) bool {
	for _, e := range ep.Entities {
		if t, _ := e.Props["connections_truncated"].(bool); t {
			return true
		}
	}
	return false
}

func nonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func humanBytes(v float64) string {
	const unit = 1024.0
	if v < unit {
		return fmt.Sprintf("%.0f B", v)
	}
	div, exp := unit, 0
	for n := v / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", v/div, "KMGTP"[exp])
}
