# Security

## Secret storage

- Broker passwords, tokens and NKey seeds are stored in the OS credential store
  (macOS Keychain, Windows Credential Manager, Linux Secret Service) under the
  service name `relay-ide`.
- SQLite stores `secretRef` identifiers only, never secret values.
- Deleting a connection deletes its stored secret. Changing a connection to an
  auth method without a secret clears the stored one.
- Secrets are not passed on process command lines.
- **File fallback:** when no OS credential store is available (headless CI),
  the engine falls back to files with owner-only permissions under the data
  directory. The UI shows a warning in the engine dialog and on the Settings
  page whenever this fallback is active. It is not intended for workstations.

## URLs and files

- Server URLs and monitoring URLs must not contain credentials
  (`nats://user:pass@…` is rejected by both the UI and the engine; errors show
  `[REDACTED]` in place of the user info).
- Allowed schemes: `nats`, `tls`, `ws`, `wss` for brokers; `http`, `https` for
  monitoring endpoints.
- TLS certificate, key and creds-file paths must be regular files of at most
  1 MiB; a client certificate requires its key and vice versa.

## TLS

- Certificate validation is always enforced; `InsecureSkipVerify` is rejected.
- There is no silent fallback to plaintext after a TLS failure.

## IPC

- The desktop app talks to the engine over NDJSON on stdin/stdout only. There
  is no network listener in the packaged app.
- The host refuses engine methods reserved for itself (`shutdown`) and clamps
  request timeouts.
- Inputs are validated by the engine (profiles, systems, preferences, queries).
  The UI validates forms before sending but does not schema-validate engine
  responses at runtime.
- `scripts/dev-bridge.mjs` (development only) exposes the same IPC over HTTP on
  `127.0.0.1`, protected by a random bearer token written to
  `apps/desktop/.env.development.local` (mode 0600, gitignored). It is never
  bundled.

## Insights query explorer

- Only single `SELECT`/`WITH` statements over an allowlist of Insights tables
  and views; other tables (`connections`, `preferences`, `sqlite_master`) are
  refused.
- Runs on a read-only connection with a row limit (≤ 5000) and a timeout
  (≤ 30 s); queries can be cancelled.

## Logging and redaction

- Diagnostic logs and error causes returned over IPC redact credentials,
  tokens and URL passwords.
- Message payloads are not written to logs.

## Message data

- Relay does not persist message bodies. The live inspector keeps received
  messages in memory only, bounded by the "Messages kept in the inspector"
  preference, and they are discarded when the app closes.
- JetStream messages are read from the server on demand (direct get by
  sequence) and are not stored locally.
- Insights snapshots contain monitoring data only, never payloads.

## Production safety

- Monitoring is read-only: Insights only issues HTTP GETs against monitoring
  endpoints and never publishes or changes server state.
- Production connections are visually marked. Publishing or requesting on one
  requires an explicit confirmation; deleting one requires typing its name.
- Read-only connections refuse publish and request/reply in the engine.

## Tauri

- CSP: `default-src 'self'; script-src 'self'; connect-src 'self' ipc: http://ipc.localhost`.
- Capabilities: `core:default` only — no shell, filesystem or HTTP plugins.
- Message content is rendered as text, never as HTML.
