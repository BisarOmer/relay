# Insights Upgrade Plan

Date: 2026-10-27

## Existing architecture (inspected)

| Layer | Technology | Notes |
|-------|------------|-------|
| Desktop UI | React 19 + TypeScript + Vite + Tailwind | Dark-first IDE shell in `AppShell` |
| Desktop host | Tauri 2 (Rust) | Spawns Go sidecar; `engine_request` / `engine-event` |
| Broker engine | Go | NDJSON IPC stdin/stdout, SQLite (`modernc.org/sqlite`), OS keychain |
| NATS client | `nats.go` | Connect/pub/sub/request/JetStream list/get/GetMsg |
| Persistence | SQLite + migrations | Only `001_initial.sql` today |
| RabbitMQ / Kafka | Not implemented | Empty capability stubs only |

Process boundary remains:

```text
React UI ↔ Tauri ↔ Go engine ↔ NATS client + NATS HTTP monitoring
```

## Implemented capabilities (real)

- Connection CRUD, secure secrets, NATS connect/disconnect
- Live message inspector, publisher, request/reply
- JetStream stream/consumer list + historical GetMsg
- Client-side “monitoring” pane (Relay-observed buffer counts only — **not** broker telemetry)
- Unit + NATS integration tests; IPC smoke path verified

## Placeholders / empty packages

- `engine/internal/monitoring` — empty
- `engine/internal/explorer` — empty
- `engine/internal/history` — empty
- Frontend `features/monitoring` / `history` — unused directories; AppShell shows a thin client-side metrics stub

## Missing for Insights

- HTTP monitoring collector (`/varz`, `/connz`, `/jsz`, …)
- Multi-system Insights configuration (monitoring URL ≠ client URL)
- Normalized topology entities + relationships
- Persisted historical snapshots / epochs
- Diagnostic check registry + finding lifecycle
- Insights workspace UI (overview, topology, findings, history, query)
- Demo simulator for offline exploration
- Read-only SQL explorer over historical views

## Compatibility constraints

- Do **not** rewrite Tauri/Go/IPC stack.
- Do **not** remove IDE publish/subscribe features.
- Insights must be **read-only** against NATS (HTTP monitoring + JetStream inspect only).
- Keep SQLite; add versioned migrations.
- Secrets stay in OS keychain; never log credentials.
- Unavailable monitoring endpoints → explicit unavailable/partial, never fabricated zeros presented as live totals.

## Modules to extend

| Area | Path |
|------|------|
| Collector | `engine/internal/monitoring/` (new) |
| Topology normalize | `engine/internal/monitoring/normalize.go` |
| Snapshots / retention | `engine/internal/monitoring/store.go` + `storage` |
| Diagnostics | `engine/internal/monitoring/diagnostics/` |
| IPC wiring | `engine/internal/app/app.go` |
| Schema | `engine/migrations/002_insights.sql` |
| UI workspace | `apps/desktop/src/features/insights/` |
| Navigation | `apps/desktop/src/app/AppShell.tsx` |
| Types/store | `apps/desktop/src/types`, `stores` |

## Required database migrations

`002_insights.sql`:

- `insights_systems` — monitored systems (name, monitoring base URL, interval, retention, optional `connection_id`)
- `insights_epochs` — collection runs (system, ts, completeness, errors JSON)
- `insights_entities` — latest + historical entity rows keyed by `(system_id, epoch_id, entity_id)`
- `insights_metrics` — metric samples per epoch/entity
- `insights_findings` — diagnostic findings + lifecycle
- `insights_finding_events` — occurrence history
- Read-only SQL views for Query Explorer (later milestone)

## Planned UI changes

- Activity bar: add **Insights** (keep Explorer / Live / Publisher / …)
- Insights sub-nav: Overview, Topology, Servers, Connections, JetStream, Findings, History, Query, Settings
- System selector in Insights header
- Reuse Badge/Button/Input; add simple SVG sparkline/time-series without new heavy chart deps initially
- Entity detail tabs opened in existing workspace tab system

## Testing strategy

- Unit: endpoint parsers, normalizer IDs, diagnostic thresholds, finding dedupe, SQL allowlist (later)
- Integration: Docker NATS `-m 8222 -js` → collect → snapshot → finding
- Frontend: Insights overview/findings smoke tests with mocked IPC where needed
- Preserve existing `go test ./...` and NATS pub/sub integration

## Ordered milestones

1. **Plan + schema** — this doc, migration 002
2. **Collector + current overview** — varz/connz/jsz/healthz/routez
3. **Topology + entity details** — tree + server/stream/connection pages
4. **Historical snapshots + compare** — epochs, retention, diff
5. **Diagnostics + Findings UI** — checks + lifecycle
6. **Query explorer + simulator** — read-only SQL + demo mode
7. **Alerts / optional AI** — only after 2–5 solid
8. **Hardening** — integration, retention, security, build verification

## Vertical slice (first coding target)

```text
Configure system (monitoring URL)
  → Collect /varz+/connz+/jsz+/healthz
  → Normalize entities
  → Persist epoch
  → Run checks (e.g. healthz fail, slow consumers)
  → Show Overview + Topology + Findings in UI
```

## Compatibility note on monitoring URL

Client connections use `nats://host:4222`. Monitoring uses HTTP `http://host:8222`. Insights systems store `monitoringBaseUrl` separately and may optionally reference a Relay connection profile for correlation, not for publish access.
