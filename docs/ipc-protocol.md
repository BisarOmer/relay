# IPC Protocol

Version: **1**

Transport: newline-delimited JSON (NDJSON) over the engine process stdin/stdout.

## Framing

- One JSON object per line.
- UTF-8 encoding.
- Maximum line size: **16 MiB** (oversized frames are rejected).
- Binary payloads are base64-encoded in JSON fields (`payloadBase64`).
- Stderr is reserved for diagnostic logs (no protocol frames).

## Handshake

Engine → Host (first event after start):

```json
{"type":"event","event":"engine.ready","protocolVersion":1,"engineVersion":"0.1.0","ts":"2026-10-27T00:00:00Z"}
```

Host → Engine:

```json
{"type":"request","id":"req-1","method":"hello","params":{"protocolVersion":1,"client":"relay-desktop","clientVersion":"0.1.0"}}
```

Engine → Host:

```json
{"type":"response","id":"req-1","ok":true,"result":{"protocolVersion":1,"capabilities":["nats"]}}
```

If protocol versions are incompatible, the engine responds with an error and exits.

## Message envelope

### Request

```json
{
  "type": "request",
  "id": "uuid-or-opaque",
  "method": "connections.list",
  "params": {},
  "timeoutMs": 30000
}
```

### Response

```json
{
  "type": "response",
  "id": "uuid-or-opaque",
  "ok": true,
  "result": {}
}
```

Error response:

```json
{
  "type": "response",
  "id": "uuid-or-opaque",
  "ok": false,
  "error": {
    "code": "auth_error",
    "message": "authentication failed",
    "retryable": false,
    "details": {}
  }
}
```

### Event

```json
{
  "type": "event",
  "event": "connection.status",
  "correlationId": "optional-related-id",
  "ts": "2026-10-27T00:00:00Z",
  "payload": {}
}
```

### Cancel

```json
{"type":"request","id":"cancel-1","method":"cancel","params":{"targetId":"original-request-or-subscription-id"}}
```

## Error codes

| Code | Category |
|------|----------|
| `validation_error` | Invalid input |
| `auth_error` | Authentication failure |
| `authorization_error` | Permission denied |
| `network_error` | DNS/TCP failures |
| `tls_error` | Certificate / TLS failures |
| `broker_error` | Broker protocol error |
| `timeout` | Deadline exceeded |
| `unsupported` | Capability not available |
| `not_found` | Missing resource |
| `limit_exceeded` | Rate or resource limit |
| `cancelled` | Cancelled by client |
| `internal` | Unexpected engine error |

Secrets must never appear in `message` or `details`.

## Methods (v1)

### Engine

| Method | Description |
|--------|-------------|
| `hello` | Protocol negotiation |
| `ping` | Liveness |
| `cancel` | Cancel in-flight work |
| `shutdown` | Graceful shutdown |

### Connections

| Method | Description |
|--------|-------------|
| `connections.list` | List saved profiles |
| `connections.get` | Get one profile |
| `connections.create` | Create profile (+ optional secrets) |
| `connections.update` | Update profile |
| `connections.delete` | Delete profile and secret refs |
| `connections.duplicate` | Duplicate profile |
| `connections.connect` | Establish session |
| `connections.disconnect` | Close session |
| `connections.test` | Test without persisting session |
| `connections.status` | Runtime status |
| `connections.capabilities` | Declared adapter capabilities |

### Secrets

Secrets are passed only on create/update/connect/test when needed. The host may also resolve secrets and pass ephemeral values; preferred path stores them via OS keychain and passes `secretRef` only.

| Method | Description |
|--------|-------------|
| `secrets.set` | Store secret under ref |
| `secrets.delete` | Remove secret |

### NATS / messaging

| Method | Description |
|--------|-------------|
| `messages.subscribe` | Start subscription |
| `messages.unsubscribe` | Stop subscription |
| `messages.publish` | Publish message |
| `messages.request` | Request/reply |
| `messages.pause` | Pause delivery to UI (engine may still consume depending on mode) |
| `messages.resume` | Resume delivery |

### Explorer (NATS / JetStream)

| Method | Description |
|--------|-------------|
| `explorer.list` | List resources for a connection |
| `jetstream.streams.list` | List streams |
| `jetstream.streams.get` | Stream details |
| `jetstream.consumers.list` | List consumers |
| `jetstream.messages.get` | Historical message by sequence (when authorized) |

### Preferences / templates

| Method | Description |
|--------|-------------|
| `preferences.get` | App preferences |
| `preferences.set` | Update preferences |
| `templates.list` | Message templates |
| `templates.save` | Save template |
| `templates.delete` | Delete template |

### Insights (NATS observability)

| Method | Description |
|--------|-------------|
| `insights.systems.list` | List monitored systems |
| `insights.systems.upsert` | Create/update system |
| `insights.systems.delete` | Delete system + history |
| `insights.collect` | Run one collection cycle |
| `insights.overview` | Current overview aggregates |
| `insights.topology` | Entities for an epoch |
| `insights.entity.get` | Entity + metrics |
| `insights.epochs.list` | Historical epochs |
| `insights.diff` | Compare two epochs |
| `insights.findings.list` | Diagnostic findings |
| `insights.findings.ack` | Acknowledge (does not resolve) |
| `insights.query` | Read-only SQL over Insights views |

Events: `insights.epoch` after a successful collection.

## Events

| Event | Payload summary |
|-------|-----------------|
| `engine.ready` | Version info |
| `engine.error` | Fatal/recoverable engine issues |
| `connection.status` | Connection id + status |
| `connection.event` | Reconnect/disconnect diagnostics |
| `messages.inbound` | Batch of inbound messages |
| `messages.dropped` | Drop/sampling counts |
| `subscription.status` | Active/paused/stopped/error |
| `monitoring.sample` | Metric samples |

## Payload limits

| Item | Default limit |
|------|---------------|
| Single message payload in event | 256 KiB inline; larger marked truncated |
| Messages per `messages.inbound` batch | 50 |
| Live buffer per subscription | 2,000 messages |
| Subscription channel capacity | 256 |

Truncation sets `truncated: true` and preserves `sizeBytes`.

## Versioning

- `protocolVersion` is an integer.
- Additive fields are allowed within a version.
- Removing/renaming fields or changing semantics requires a new version.
- Engine supports one primary version at a time for MVP.

## Cancellation and timeouts

- Every request may include `timeoutMs` (default 30s for RPCs; subscriptions are long-lived and use cancel).
- Cancel targets the original request `id` or a subscription id returned by `messages.subscribe`.
- On shutdown, all subscriptions are cancelled and connections closed.
