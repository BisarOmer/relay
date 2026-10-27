# Release

## Build targets

- macOS: `aarch64-apple-darwin`, `x86_64-apple-darwin`
- Windows: `x86_64-pc-windows-msvc`
- Linux: `x86_64-unknown-linux-gnu`

Build the Go sidecar for each target and place it at:

```text
apps/desktop/src-tauri/binaries/relay-engine-<triple>
```

Then:

```bash
make build
```

## Signing and notarization

Not completed for this repository state. Do not distribute installers as signed/notarized until:

1. Platform certificates are configured
2. Notarization (macOS) succeeds
3. Verification steps are recorded here with dates

## Versioning

Application version lives in:

- `apps/desktop/package.json`
- `apps/desktop/src-tauri/tauri.conf.json`
- `apps/desktop/src-tauri/Cargo.toml`
- `engine` `EngineVersion` constant

Keep them aligned for each release.

## Verification checklist

- [ ] `make test` passes
- [ ] `make test-integration` passes against disposable NATS
- [ ] Manual acceptance: connect, subscribe, publish, JetStream list
- [ ] Secrets absent from SQLite and logs
- [ ] Artifacts built for intended OS/arch
