# Insights diagnostics

Insights polls a NATS server's HTTP monitoring endpoints (`/healthz`, `/varz`,
`/connz`, `/jsz`, `/routez`, `/gatewayz`, `/leafz`, `/subsz`, `/accountz`),
normalizes the responses into a snapshot (an *epoch*), and runs the checks
below on every snapshot. It only issues HTTP GETs; it never connects as a
client or changes server state.

## Collection

- One collection loop per enabled system, at the configured interval
  (1 s – 1 h). Saving an enabled system starts collecting immediately.
- Each endpoint has its own outcome (`ok`, `forbidden`, not available,
  failed) shown in the Overview's endpoint coverage table. Transient 5xx
  responses are retried with jitter.
- *Completeness* is the share of attempted endpoints that answered.
  A snapshot where nothing answered is `unreachable`; some answered is
  `partial`.
- While a system is unreachable the loop backs off exponentially (doubling
  per failure, capped at 2 minutes) and returns to the normal interval on the
  first success.
- Snapshots beyond the retention count (10 – 100000) are deleted, oldest first.

## Unknown is not zero

A metric that was not reported (endpoint failed, field absent, JetStream
disabled) is stored as unavailable, never as `0`. The UI shows "—" with the
reason, charts leave a visible gap, and checks that need the metric are
skipped rather than passed.

Counters (`in_msgs`, `out_msgs`, `in_bytes`, `out_bytes`, `total_connections`,
`slow_consumers`, `num_redelivered`) are charted as per-second rates between
consecutive snapshots. A decrease (server restart / counter reset) becomes a
gap, not a negative rate.

## Findings lifecycle

| Event | Meaning |
|-------|---------|
| opened | The check failed for an entity for the first time. |
| reopened | A resolved finding failed again. |
| resolved (`condition_cleared`) | The check ran with complete inputs and passed. |
| resolved_manually | A user resolved it; it reopens if the condition persists. |
| acknowledged / unacknowledged | User triage state; does not resolve anything. |
| severity_changed | The same finding moved between warning and critical. |

A finding is **not** resolved when the inputs needed to evaluate it are
missing (for example `/connz` failed); it stays open until a snapshot can
actually show the condition is gone.

## Checks

| ID | Severity | Fires when | Needs |
|----|----------|-----------|-------|
| `availability.monitoring_unreachable` | critical | No monitoring endpoint answered. | — |
| `availability.healthz_failure` | critical | `/healthz` reports a non-ok status. | `/healthz` |
| `infrastructure.partial_collection` | warning | Some endpoints failed in this snapshot. | — |
| `infrastructure.permission_denied` | warning | An endpoint returned 401/403. | — |
| `availability.server_restarted` | info | Server start time changed between snapshots. | `/varz`, previous snapshot |
| `connections.slow_consumers` | warning | `slow_consumers` increased between consecutive snapshots. | `/varz`, previous snapshot |
| `connections.high_pending_bytes` | warning | A client has ≥ 1 MiB of unsent data buffered. | `/connz` |
| `connections.capacity` | warning / critical | Connections ≥ 80 % (critical ≥ 95 %) of `max_connections`. | `/varz` |
| `cluster.route_lost` | warning | A route present in an earlier snapshot is missing now. | `/routez` |
| `cluster.gateway_degraded` | warning | A gateway is connected in only one direction. | `/gatewayz` |
| `jetstream.ack_pending_buildup` | warning | `num_ack_pending` ≥ 80 % of `max_ack_pending`, or ≥ 1000 when unlimited. | `/jsz` with consumers |
| `jetstream.backlog_growing` | warning | `num_pending` ≥ 10000 and increasing. | `/jsz`, previous snapshot |
| `jetstream.resource_pressure` | warning / critical | Storage or memory ≥ 80 % (critical ≥ 95 %) of the JetStream limit. | `/jsz` |

Thresholds are constants in `engine/internal/monitoring/checks.go`.

### Verified against real servers

- `availability.monitoring_unreachable`: opened when the topology servers were
  stopped, resolved automatically when they came back.
- `cluster.route_lost`: opened on server `a` when cluster peer `b` was stopped,
  acknowledged in the UI, resolved automatically when `b` restarted; the
  History diff showed the route as removed and server `a`'s subscription count
  dropping from 119 to 66.

The remaining checks are covered by unit tests in
`engine/internal/monitoring/checks_test.go` and `lifecycle_test.go` but were
not reproduced against a live server.

## Scope of a monitored system

A system is one monitoring endpoint. Topology shows that server and what it
reports about its peers (routes, gateways, leaf nodes); peer servers appear as
references, not with their own metrics, unless they are added as separate
systems. Overview numbers are for the monitored server only.

## History and query

- **History** compares any two snapshots: entities added, removed, missing
  (when the later snapshot was incomplete) and changed fields/metrics.
- **Query** runs read-only SQL over the Insights tables and views listed in
  its schema sidebar (`:system_id` binds the selected system). See
  [security.md](security.md#insights-query-explorer) for its limits.
