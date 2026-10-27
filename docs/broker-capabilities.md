# Broker Capabilities

The UI enables features from the capabilities each adapter reports; it does
not show actions an adapter does not support.

## NATS (implemented)

| Capability | Status | Notes |
|------------|--------|-------|
| Connect / disconnect | Yes | Status events, including reconnecting / reconnected |
| TLS | Yes | Skip-verify rejected |
| Auth: none, user/pass, token, nkey, creds file | Yes | Secrets via OS keystore |
| Publish | Yes | Headers supported |
| Subscribe (core) | Yes | Wildcards `*` and `>` |
| Queue groups | Yes | |
| Request/reply | Yes | No-responders reported as an error |
| Subject catalog | No | Manual / observed / JetStream-discovered only |
| JetStream stream list/get | Yes | When JetStream is available |
| JetStream consumers list | Yes | |
| JetStream read message | Yes | Direct get by sequence; no durable side effects |
| Stream/consumer create/delete | No | |
| Metrics / history capabilities | No | Not advertised by the adapter; monitoring lives in Insights |

### Read-only connections

A connection marked read-only refuses publish and request/reply in the
engine. Subscribing and browsing JetStream remain available.

### Consumption / ack semantics

- Live inspector subscriptions use **observe** mode: messages are not acknowledged.
- Pausing stops rendering; the broker may still deliver, and drops are
  reported explicitly.

## Insights monitoring (NATS HTTP)

| Capability | Status | Notes |
|------------|--------|-------|
| `/healthz` `/varz` `/connz` `/jsz` `/routez` | Yes | Collected when available |
| `/gatewayz` `/leafz` `/subsz` `/accountz` | Yes | Shown as not available if the server doesn't serve them |
| Snapshots / epoch history | Yes | SQLite, retention by count |
| Diagnostics / findings | Yes | See [diagnostics.md](diagnostics.md) |
| Read-only SQL explorer | Yes | Allowlisted tables, read-only connection |
| Demo system | Yes | Simulated, explicitly labeled |

Insights never publishes, deletes streams, or changes consumers.

## RabbitMQ / Kafka

Not implemented. Types exist in the engine, but the UI does not offer them.
