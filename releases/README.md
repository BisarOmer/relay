# Releases

Prebuilt installers live in this folder.

| File | Platform | Notes |
|------|----------|-------|
| [Relay_0.1.0_aarch64.dmg](Relay_0.1.0_aarch64.dmg) | macOS Apple Silicon (M1/M2/M3/M4) | Unsigned — see below |

## macOS install

1. Open the DMG and drag Relay to Applications.
2. Run once:

```bash
xattr -cr /Applications/Relay.app
```

3. Open Relay.

Without that command, Gatekeeper may say the app is **damaged**. It is not — the build is unsigned, so macOS blocks downloads from the web.

Intel Mac, Windows, and Linux builds are not published yet.
