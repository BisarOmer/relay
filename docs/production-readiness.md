# Production Readiness — Relay Insights

Living checklist. Every "Fixed" item names the evidence: a command, an
automated test, or behavior observed against a real server. Items without
evidence are not claimed. "Verified in UI" means the screen was driven in a
browser against the real engine through `scripts/dev-bridge.mjs` and inspected
(DOM state and screenshots), not just read in source.

Last updated: 2026-10-28

**Overall verdict: not production-ready yet.** The NATS IDE and Insights paths
are functional and verified against real servers, but the items under
[Remaining blockers](#remaining-blockers) are open, and the packaged desktop
app's UI was not visually inspected (only its engine was smoke-tested).

## Environment used for verification

| Item | Value |
|------|-------|
| OS | macOS (darwin 27, arm64) |
| Go | 1.27.3 (module declares `go 1.26.0`) |
| Node / npm | npm 10.9.2 |
| Rust | 1.99.1 (Homebrew) |
| NATS | real `nats-server v2.15.2` from `.tools/` (no Docker) |
| Topologies | standalone + JetStream (`:4222`/`:8222`); 2-node cluster `C1` (a `:8223`, b `:8224`) with a leaf node (edge `:8225`); gateways EAST/WEST (gx `:8226`, gy `:8227`); user/password server (`:4230`) |
| Docker | daemon **not running**; Docker-based targets (`make brokers-up`, `make test-integration`) not executed |
| RabbitMQ / Kafka | **no adapters exist**; not offered in the UI |

## Commands run (latest pass)

| Command | Result |
|---------|--------|
| `make lint` (go vet, gofmt gate, tsc) | exit 0 |
| `make test` (`go test -race ./...`, tsc, vitest) | exit 0 |
| `go test -race -count=1 ./...` (uncached) | all 7 packages ok, no races |
| `make test-integration-local` (real nats-server `:8222`) | all packages ok |
| `npx vitest run` | 5 files, 41 tests passed |
| `npx vite build` | ok; main chunk 512 kB (155 kB gzip) + lazy Insights chunk 62 kB |
| `cargo check`, `cargo clippy -- -D warnings` | ok, no warnings |
| `make build` (`tauri build`) | ok: `Relay.app` (24 MiB) and `Relay_0.1.0_aarch64.dmg` (12.5 MiB) |
| Bundled `Relay.app/Contents/MacOS/relay-engine` with an empty data dir outside the repo | `engine.ready`, `hello` reports schema v3, `insights.schema` answers |

## Audit findings (prioritized)

Severity: **P0** = security, data integrity, or app-breaking. **P1** = broken or
misleading feature. **P2** = polish / quality.

### P0

| # | Finding | Status | Evidence |
|---|---------|--------|----------|
| 1 | Query explorer could read any table (`connections`, `sqlite_master`, `preferences`). | Fixed | Table/view allowlist. Bridge probes: `connections`, `preferences` (also via subquery), `sqlite_master` → `authorization_error`; `PRAGMA` and multi-statement (`; ATTACH …`) rejected. Read-only connection pool (`TestReadOnlyPoolRejectsWrites`). |
| 2 | A runaway query froze the engine. | Fixed | Recursive CTE with 1 s limit → `timeout` ("interrupted"); the next request answered immediately. UI Cancel stopped a running query within ~100 ms; engine CPU back to 0 %. |
| 3 | Credentials in server URLs were stored and shown. | Fixed | Engine rejects `nats://u:p@…` and `http://u:p@…` monitoring URLs, error text shows `[REDACTED]`. UI rejects both before sending (unit tests + observed in form). |
| 4 | Migrations loaded from the source tree; packaged builds could not start. | Fixed | `go:embed` migrations; bundled engine from `Relay.app` migrated a fresh data dir to v3. |
| 5 | Engine exit left requests hanging, no restart path. | Fixed | Host fails pending requests on exit/stop, emits `engine.exited`, has `engine_restart`; UI Restart observed: "starting → ready", connections/subscriptions reset. (Exercised through the dev bridge, which mirrors the host; not through the Tauri window.) |
| 6 | Bogus `cluster:map[]` entity on standalone servers. | Fixed | Query against the real standalone system: 0 `cluster:%` entities. |
| 7 | Broker loss never reflected; `maxAttempts: 0` disabled reconnect. | Fixed | Killed the real `:4222` server with a live subscription: status bar "0/1 connected · 1 reconnecting", output log "connection lost: EOF"; restarted it: "reconnected to nats://127.0.0.1:4222", and a message published afterwards arrived on the same subscription. `TestReconnectAfterServerRestart`. |
| 8 | Disconnect leaked subscriptions. | Fixed | `TestDisconnectWithoutReconnectEndsSessionAndSubscriptions`, `TestUnsubscribeUnderLoadDoesNotPanic`. |
| 9 | `messages.request` bypassed read-only. | Fixed | Real read-only connection: `messages.publish` and `messages.request` → `authorization_error: connection is read-only`; `messages.subscribe` still works. |
| 10 | Findings auto-resolved when their input data was missing. | Fixed | `TestFindingLifecycleAcrossPartialAndUnreachableCollections`, `TestUnavailableMetricSkipped`, `TestChecksNotEvaluableWithoutInputs`. |
| 11 | Collection clobbered system edits, overlapped, and reported `ok` when everything failed. | Fixed | `TestCollectOnceIsSingleFlight`, `TestUpsertDoesNotClobberCollectionStatus`. Observed: stopped servers → `unreachable` / "connection refused"; restarted → `ok` without user action. |
| 12 | Error causes dropped at the IPC boundary. | Fixed | Causes are returned with secrets redacted (`TestErrorCausesAreRedacted`). Observed: "query failed: no such column: name", "authentication failed: nats: Authorization Violation". |
| 30 | **New:** `insights.metrics.series` returned newest-first while charts and rate derivation assume oldest-first; every counter rate came out negative (shown as gaps) and chart time axes were reversed. | Fixed | Store returns chronological order; integration test asserts ordering against the real server. Observed rates 0.4–0.8 msg/s on the standalone server after the fix. |
| 31 | **New:** Connection details page crashed the whole app (blank window) after "Save and connect" — a zustand selector returned a new array on every read ("Maximum update depth exceeded"). No error boundary existed. | Fixed | Selector memoized; observed page renders. Error boundaries added at app, page and tab level (`ErrorBoundary` test). |

### P1

| # | Finding | Status | Evidence |
|---|---------|--------|----------|
| 13 | Preferences were fake; Settings was a placeholder. | Fixed | Settings page persists theme/density/timezone/max messages; light theme survived a reload (verified in UI). |
| 14 | Placeholder "History" and "Settings" activities. | Fixed | Placeholders removed from navigation; Settings is real; History lives in Insights (verified in UI). |
| 15 | Connections could only be created. | Fixed | Verified in UI: create, test (wrong and right password), save and connect, edit (environment → production), duplicate, delete with typed-name confirmation for production. |
| 16 | Insights ignored live epoch events; no freshness. | Fixed | Overview/topology refresh on `insights.epoch`; "Updated just now" and stale/unreachable banners observed. |
| 17 | Monitored systems could only be created. | Fixed | Verified in UI: add (with URL validation), enable toggle, edit, remove with confirmation. |
| 18 | Topology was a flat list. | Fixed | Tree (system → cluster → server → route/leaf/connection, accounts), type filters, search, entity panel with metrics chart, properties and related entities — verified against cluster `C1`. |
| 19 | Query results were raw JSON, no export. | Fixed | Table with row count/duration, ⌘/Ctrl+Enter, Cancel, CSV/JSON export (captured CSV: header + 75 rows). |
| 20 | Findings had no history or resolution view. | Fixed | Real route-loss finding (stopped server b): opened → acknowledged → auto-resolved ("condition cleared") when b returned; timeline, evidence and remediation shown. |
| 21 | `window.confirm` for production publish; no delete confirmations. | Fixed | In-app confirm dialog; Cancel sends nothing, Publish sends (verified on a production-marked connection). |
| 22 | Lifetime counters without rates; unknown shown as `0`. | Fixed | Counter charts show rates with gap markers; unknown stats render "—" with a reason (unit tests + UI). |
| 23 | Subscription drops silently discarded. | Fixed | `messages.dropped` events surface as per-subscription dropped counts. Not reproduced live (needs a slow consumer). |
| 24 | Unused shell plugin and `shell:allow-open` capability. | Fixed | `capabilities/default.json` grants only `core:default`; no shell/fs/http plugins in `Cargo.toml`. |
| 25 | `make lint` passed with unformatted Go files. | Fixed | gofmt gate fails the target; currently exit 0 with no files listed. |
| 32 | **New:** Opening a live/publish tab also created a stray "choose a connection" tab; the highlighted activity did not follow tab switches. | Fixed | `app-store.test.ts`; verified in UI (exactly one tab opens; sidebar follows the active tab). |
| 33 | **New:** NATS adapter advertised "Metrics" and "History" capabilities that nothing implements. | Fixed | Removed from `Capabilities()`; only implemented capabilities are listed. |
| 34 | **New:** "Add and collect" triggered a duplicate collection in the same second (the scheduler already collects on save). | Fixed | Manual collect only for systems saved with automatic collection off. |
| 35 | **New:** Unreachable banner claimed "Showing the last good snapshot" while the overview showed the failed one; toolbar said "Never collected" after failed attempts. | Fixed | Text now states the last successful snapshot time (or that none succeeded). |
| 36 | **New:** Topology treated the engine's `fresh` freshness as abnormal (`current` expected), so every entity got a warning badge. | Fixed | UI matches the engine contract `fresh | partial | stale`. |

### P2

| # | Finding | Status | Evidence |
|---|---------|--------|----------|
| 26 | Inconsistent visual system, no light theme. | Fixed | Token-based dark/light themes, density, shared Dialog/Confirm/Toast/EmptyState/Segmented primitives; screenshots in both themes. |
| 27 | No collapsible sidebar, breadcrumbs or shortcuts. | Fixed | ⌘K palette (fuzzy search, verified), ⌘B, ⌘J, ⌘N, ⌘1–5, ⌘,, `?` dialog (verified). ⌘W intentionally not bound (conflicts with the native Close Window accelerator). |
| 28 | Stream view showed raw JSON. | Fixed | Structured state/limits/cluster tables, consumers, message browser by sequence (verified on `ORDERS`). |
| 29 | Frontend tests covered only base64 helpers. | Fixed | 41 tests: formatting/unknown handling, counter rates, staleness, CSV escaping, subject/form validation, palette ranking, Stat, ConfirmHost, LineChart gaps, ErrorBoundary, tab/activity store. |
| 37 | **New:** Layout problems at the 960×640 minimum window: finding details wrapped character by character; topology type labels truncated; flat charts printed "1 1 1"; endpoints rendered as `//varz`. | Fixed | Verified at 960×640 (no horizontal overflow; screenshots `08-findings-960`, `09-topology-960`, `10-entity-960`). |

## Security checks performed

- Password canaries: a user/password server on `:4230` was tested with a wrong
  and a right password through the UI. Neither value appeared in the DOM, the
  SQLite database (including WAL), the engine/bridge output or the Vite output.
  The secret was present in the macOS keychain (`relay-ide` service) while the
  connection existed and was removed when the connection was deleted.
- The dev bridge listens on `127.0.0.1` only, requires a bearer token
  (401 without it), restricts CORS to the Vite origin, and refuses the
  host-only `shutdown` method.
- Tauri CSP: `default-src 'self'; script-src 'self'; connect-src 'self' ipc: http://ipc.localhost`.

## Remaining blockers

1. **Packaged desktop UI not visually verified.** `screencapture` is not
   permitted in this environment, so the Tauri window itself (WKWebView) was
   not inspected; all UI verification used Chromium through the dev bridge.
2. **File downloads in WKWebView.** CSV/JSON export uses a blob URL and
   `<a download>`. This works in Chromium; WKWebView commonly ignores it.
   Needs a Tauri save-dialog path (`tauri-plugin-dialog` + a scoped write
   command) and a test in the packaged app.
3. **Docker-based targets unverified** (`make brokers-up`, `make test-integration`):
   the Docker daemon was not running.
4. **No code signing / notarization.**
5. **Zod schemas are not used for runtime validation** of IPC responses; the
   UI trusts engine response shapes (engine-side validation is in place).
6. **Slow-consumer and pending-bytes findings** are covered by unit tests only;
   not reproduced against a real server.
7. **Main bundle 512 kB** (above Vite's warning threshold); acceptable for a
   local desktop app but not split further.

## Not present in the product (not added to navigation)

- Alerts / notification channels — no implementation exists.
- AI assistance — no implementation exists.
- Simulator beyond Insights demo systems (clearly labeled "simulated").
- RabbitMQ / Kafka adapters — engine returns `unsupported`; UI only offers NATS.
