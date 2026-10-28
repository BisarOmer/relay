# Implementation Status

Last updated: 2026-10-27. The verified checklist, evidence and blockers are in
[production-readiness.md](production-readiness.md); this page summarizes scope.

## Implemented

### Message Broker IDE (NATS)
- Tauri + React desktop, Go sidecar over NDJSON stdio IPC, SQLite with
  embedded migrations, OS keychain for secrets
- Connections: create, edit, duplicate, delete (typed confirmation for
  production), test before save, read-only mode, live status with
  reconnecting/reconnected states
- Live messages: core subscriptions with wildcards and queue groups, pause,
  bounded in-memory buffer, structured inspector (JSON, headers, copy)
- Publisher: publish and request/reply with headers, JSON validation,
  confirmation on production connections
- JetStream: streams, consumers, read messages by sequence

### Insights (NATS monitoring)
- Monitored systems: add, edit, enable/disable, delete, collect now; URL
  validation rejects credentials and non-HTTP schemes
- Collector for `/healthz`, `/varz`, `/connz`, `/jsz`, `/routez`, `/gatewayz`,
  `/leafz`, `/subsz`, `/accountz` with per-endpoint outcomes, retries,
  failure backoff and retention
- Topology entities: system, cluster, server, connection, account, stream,
  consumer, route, gateway, leaf node
- 13 diagnostic checks with a full findings lifecycle (see
  [diagnostics.md](diagnostics.md))
- Overview with metric charts (counter rates, gaps for unknown data),
  topology tree with entity details, findings triage, snapshot history diff,
  read-only SQL query explorer with CSV export
- Demo system (simulated data, labeled as such wherever it appears)

### Shell
- Command palette, keyboard shortcuts, settings persisted in SQLite (theme
  including light, density, inspector buffer size), error boundaries around
  the app, pages and tabs, engine status and restart

## Not implemented

- RabbitMQ and Kafka adapters (types only; not shown in the UI)
- JetStream stream/consumer create, edit, delete
- Alerts, webhooks, AI assistance
- Subject catalogue for core NATS
- Code signing and notarization

## Tests executed (2026-10-27)

| Suite | Result |
|-------|--------|
| `make lint` (go vet, gofmt, tsc) | PASS |
| `make test` (`go test -race ./...`, tsc, Vitest 41 tests) | PASS |
| `make test-integration-local` against local `nats-server` | PASS |
| `cargo check`, `cargo clippy -- -D warnings` | PASS |
| `vite build` | PASS (main chunk 512 kB, warning) |
| `make build` (`tauri build`: `.app` and `.dmg`) | PASS — bundled engine started and migrated a temporary database; the packaged window was not visually verified |
| Manual UI walkthrough in a browser against the real engine and real NATS servers | Done; see production-readiness.md |
| `make test-integration` (Docker) | NOT RUN — the Docker CLI is installed but the daemon was not running |

## Known limitations

- The monitoring URL is configured separately from the client `nats://` URL.
- A monitored system is a single server's monitoring endpoint; peers appear
  only as references unless added as their own systems.
- `/jsz` stream and consumer detail depends on the server returning account
  details.
- The UI does not schema-validate engine responses at runtime.
