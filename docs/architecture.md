# Relay Architecture

## Overview

Relay is a local-first desktop IDE for message brokers. The process boundary is:

```text
React UI  ↔  Tauri native host  ↔  Go broker engine  ↔  Message brokers
```

- **React UI**: presentation, workspace state, validation of user input.
- **Tauri host**: application lifecycle, windowing, OS credential store, sidecar process management.
- **Go engine**: broker adapters, subscriptions, publishing, discovery, SQLite persistence, metrics.

The UI never talks to brokers directly and never embeds Go internals.

## Process model

### Sidecar lifecycle

1. Tauri starts `relay-engine` as a local sidecar.
2. Engine announces readiness with a handshake (`hello` / `ready`) and negotiates IPC protocol version.
3. Requests and events flow over **newline-delimited JSON (NDJSON)** on stdin/stdout.
4. On app quit or sidecar crash, Tauri cancels in-flight work, tears down the process, and can restart the engine.

There is **no** unauthenticated network listener for IPC.

### IPC transport

| Direction | Transport | Format |
|-----------|-----------|--------|
| UI → Host | Tauri commands/events | Typed Rust ↔ TypeScript |
| Host → Engine | stdin/stdout NDJSON | Versioned `IPCRequest` / `IPCResponse` / `IPCEvent` |

Correlation uses `id` on requests/responses and `correlationId` on related events. Cancellation uses `cancel` requests keyed by the original request/subscription id. Timeouts are enforced on both sides.

See [ipc-protocol.md](./ipc-protocol.md).

## Package boundaries

```text
apps/desktop/          Tauri + React application
engine/                Go sidecar (broker engine)
shared/protocol/       Shared IPC schema notes / generated types (when present)
docs/                  Architecture and product docs
docker/                Local broker stacks for development and integration tests
scripts/               Build and packaging helpers
```

Inside the engine:

| Package | Responsibility |
|---------|----------------|
| `internal/ipc` | Framing, routing, validation, backpressure |
| `internal/app` | Engine lifecycle and service wiring |
| `internal/adapters/*` | Broker-specific clients |
| `internal/broker` | Shared adapter interfaces and capability declarations |
| `internal/connections` | Connection profiles and runtime sessions |
| `internal/storage` | SQLite schema and repositories |
| `internal/security` | Secret references, redaction |
| `internal/messages` | Normalization, bounded buffers, filters |
| `internal/explorer` | Resource tree aggregation |
| `internal/monitoring` | Client-side and broker-reported metrics |
| `internal/history` | Opt-in local message history |

## Connection lifecycle

1. User creates a `ConnectionProfile` (non-secret fields in SQLite).
2. Secrets are stored in the OS credential store; SQLite holds only a `secretRef`.
3. `connections.connect` resolves secrets, calls the adapter, and emits status events.
4. Reconnection uses bounded exponential backoff with jitter. Publish retries are never automatic for non-idempotent sends.
5. Disconnect cancels subscriptions, closes the client, and updates persisted last-known status (not transient runtime fields as authority).

## Streaming and backpressure

- Subscriptions produce bounded channels (configurable capacity).
- When the UI or IPC consumer is slow, the engine applies backpressure and may **drop or sample** with an explicit `messages.dropped` / sampling event.
- Pause in the UI means pause **rendering** by default; broker consumption state is shown separately.
- In-memory live buffers are capped; excess is discarded with a visible indicator.

## Persistence

SQLite (owned by the Go engine) stores:

- Connection metadata and environment profiles
- Preferences and workspace layout
- Saved subscriptions, filters, and message templates
- Optional message history (disabled by default)

Schema migrations live in `engine/migrations` and are versioned.

Secrets are **never** stored as plaintext in SQLite.

## Extension points

New brokers implement:

1. `BrokerAdapter` (+ optional capability interfaces).
2. Capability declaration used by the UI to disable unsupported actions.
3. Error mapping into shared error categories.

Adding a broker must not require rewriting existing adapters or unrelated UI modules.

## Security notes

- Restrictive Tauri CSP and minimal capabilities.
- No unrestricted shell/filesystem APIs exposed to the frontend.
- Secrets redacted from logs by default.
- Production environments are visually distinguished; destructive actions require confirmation.

See [security.md](./security.md).

## Trade-offs

| Decision | Rationale |
|----------|-----------|
| Go sidecar over pure Rust brokers | Mature official clients (especially NATS); faster path to correct broker semantics |
| NDJSON IPC | Debuggable, typed, no open port; good enough throughput for IDE workloads |
| SQLite in Go | Single owner of persistence next to broker sessions |
| NATS first | Spec priority; deep JetStream support before RabbitMQ/Kafka |
