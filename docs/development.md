# Development

## Setup

```bash
make setup
```

This installs desktop npm dependencies and builds `relay-engine` into
`apps/desktop/src-tauri/binaries/relay-engine-<target-triple>`.

`make dev` / `make build` set `CARGO_HOME` to `.cargo-home/` in the repo so Cargo
does not need a writable `~/.cargo` (useful when that directory is root-owned).

## Local brokers

```bash
make brokers-up
```

Starts NATS with JetStream (`4222`) and monitoring (`8222`) in Docker.

Without Docker, run a local server binary:

```bash
go install github.com/nats-io/nats-server/v2@latest
nats-server -js -m 8222
```

Example connection (no secrets):

```text
Name: Local NATS
Servers: nats://127.0.0.1:4222
Auth: none
TLS: off
Environment: local
```

## Running the app

```bash
make dev
```

The Tauri host spawns the Go sidecar, negotiates IPC protocol v1, and loads SQLite under the app data directory.

### In a browser

```bash
make dev-browser
```

`scripts/dev-bridge.mjs` runs the real engine (data in `.tools/dev-data`) and
serves it on `127.0.0.1:1422` behind a bearer token written to
`apps/desktop/.env.development.local`; Vite then serves the UI on
`http://localhost:1420`. Use this for UI work and browser automation; it is
development-only and not part of any build. After rebuilding the engine
(`make engine`), restart it from the engine status dialog in the UI.

## Tests

```bash
make test                     # go test -race, tsc, Vitest
make test-integration         # starts Docker NATS, runs -tags=integration
make test-integration-local   # same tests against an already running nats-server
```

Frontend tests live next to the code (`*.test.ts(x)`) and run in jsdom
(`apps/desktop/src/test/setup.ts`). Integration tests use the `integration`
build tag; set `RELAY_NATS_MONITOR_URL` if your server's monitoring port is not
`8222`.

## Debugging

- Engine diagnostic logs go to stderr (visible in the Tauri/dev console).
- Secrets and payloads are redacted from structured logs by default.
- UI output panel shows connection and subscription events.

## Contribution conventions

- Prefer extending adapter interfaces over leaking broker types into the UI.
- Do not fake unsupported capabilities.
- Keep documentation aligned with actual behavior.
- Update `docs/implementation-status.md` when completing a phase.
