# OpenPlayer engine pack & updater (Windows first)

Signed, individually-updatable engine components (libmpv, FFmpeg, libass,
dav1d…) built from **upstream source** — no K-Lite, no third-party codec
installers, no telemetry, no extras. Android comes in a later stage.

## What exists and what has been tested

| Part | Status |
|---|---|
| Manifest format, strict validation, ed25519 signing | tested (unit tests) |
| Updater: check / download / apply / rollback / verify | tested on Linux (14 tests, race detector) |
| `enginectl` CLI end-to-end (keygen, pack, update, rollback, tamper, downgrade) | tested on Linux |
| `enginectl.exe` Windows cross-build | compiles; **never run on Windows** |
| Windows directory swap while DLLs are loaded | **untested** (apply must run before the player loads the engine) |
| `scripts/license-gate.sh` | tested on dummy files |
| `.github/workflows/build-engine.yml` | **draft, never run** |

## Security rules the updater enforces
- Manifest must verify against the embedded ed25519 public key.
- `serial` must never decrease (blocks replay of old signed releases).
- Files are content-addressed (`files/<sha256>`), size- and hash-checked while streaming.
- Install paths: relative, `/` only, `.dll`/`.txt` only, no reserved Windows names.
- HTTPS only. Live engine is never touched until a fully verified update is applied.
- Previous engine kept in `engine.prev`; `rollback` swaps back; interrupted swaps self-repair.

## Usage
    enginectl keygen --out release
    enginectl pack --in dist --out release-dir --spec build/spec.json --serial 1 --version 2026.11.1 --key release.key
    enginectl update --root %LOCALAPPDATA%\OpenPlayer --pubkey release.pub --source https://example.org/engine
    enginectl rollback ... ; enginectl verify ...

Build: `go test ./... && go build ./cmd/enginectl`
Windows: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" ./cmd/enginectl`

## Legal notes
Components must stay LGPL (dynamic linking, credits, license texts shipped).
Codec patents (HEVC, AAC…) are separate from copyright — get legal advice before wide distribution.
