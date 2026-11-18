# OpenPlayer for Windows 10/11

Go program with a WebView2 interface on top of libmpv. The on-screen controls are
[uosc](https://github.com/tomasklaen/uosc).

| Folder | What it is |
|---|---|
| `cmd/openplayer` | the player |
| `cmd/openplayer-setup` | the installer (the payload is created by the build script) |
| `cmd/enginectl`, `internal/` | signed engine-pack updater (see `ENGINE-UPDATER.md`) |
| `config/` | key bindings, uosc settings, Arabic uosc text, shaders |
| `third_party/uosc-patches/` | OpenPlayer's small changes to uosc, applied by the build script |
| `third_party/` | vendored Go dependencies, so builds work offline |

## Build (on Linux, cross-compiled)

```
go test ./...
scripts/build-installer.sh <libmpv-2.dll> <uosc-checkout> <version>
```

The installer appears in `installer/OpenPlayer-Setup-<version>.exe`. Releases use the libmpv build from
[zhongfly/mpv-winbuild](https://github.com/zhongfly/mpv-winbuild) (LGPL configuration, see `installer/NOTICE.txt`).

## Updates
The player reads `https://ai4edutech.com/apps/openplayer/latest.json` at startup and every 6 hours.
To publish a release, upload the new installer and a `latest.json` with its version, file name, SHA-256 and notes.
