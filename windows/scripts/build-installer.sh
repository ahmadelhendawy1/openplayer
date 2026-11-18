#!/usr/bin/env bash
# Build installer/OpenPlayer-Setup-<version>.exe (glass WebView2 installer).
#   scripts/build-installer.sh <libmpv-2.dll> <uosc-checkout-dir> [version]
# Needs: go, python3. Works offline (dependencies are vendored in third_party/).
set -euo pipefail
ENGINE_DLL="$(realpath "${1:?usage: build-installer.sh <libmpv-2.dll> <uosc-dir> [version]}")"
UOSC="$(realpath "${2:?uosc checkout (github.com/tomasklaen/uosc)}")"
VERSION="${3:-0.8.0}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STAGE="$ROOT/build/stage"
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-mod=mod GOPROXY=off

rm -rf "$STAGE"; mkdir -p "$STAGE/engine/skin/fonts" "$STAGE/config/fonts" "$STAGE/config/script-opts" "$STAGE/licenses"
( cd "$ROOT"
  go build -trimpath -ldflags "-s -w -H windowsgui" -o "$STAGE/openplayer.exe" ./cmd/openplayer
  go build -trimpath -ldflags "-s -w" -o "$STAGE/enginectl.exe" ./cmd/enginectl )

cp "$ENGINE_DLL" "$STAGE/engine/libmpv-2.dll"
# On-screen controls live with the engine so the updater can refresh them;
# the fonts are also copied to config\fonts as a fallback for older engines.
cp -r "$UOSC/src/uosc" "$STAGE/engine/skin/uosc"
cp "$UOSC"/src/fonts/* "$STAGE/engine/skin/fonts/"
cp "$UOSC"/src/fonts/* "$STAGE/config/fonts/"
cp "$ROOT/config/input.conf" "$ROOT/config/openplayer-text.json" "$ROOT/config/uosc-ar.json" "$STAGE/config/"
cp "$ROOT/config/script-opts/uosc.conf" "$STAGE/config/script-opts/"
cp -r "$ROOT/config/shaders" "$ROOT/config/covers" "$STAGE/config/"
cp "$UOSC/LICENSE.LGPL" "$STAGE/licenses/uosc-LGPL-2.1.txt"
cp "$ROOT/installer/NOTICE.txt" "$STAGE/licenses/NOTICE.txt"

# Payload for the installer (settings file openplayer.conf is written at install time).
SETUP="$ROOT/cmd/openplayer-setup"
( cd "$STAGE" && rm -f "$SETUP/payload.zip" && python3 -c "
import os, zipfile
z = zipfile.ZipFile('$SETUP/payload.zip', 'w', zipfile.ZIP_DEFLATED, compresslevel=9)
for d, _, fs in os.walk('.'):
    for f in sorted(fs):
        p = os.path.join(d, f); z.write(p, os.path.relpath(p, '.').replace(os.sep, '/'))
z.close()" )
cp "$ROOT/assets/icon-256.png" "$SETUP/icon.png"
( cd "$ROOT" && go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$VERSION" -o "$ROOT/installer/OpenPlayer-Setup-$VERSION.exe" ./cmd/openplayer-setup )
# Restore the small placeholder so the source tree stays light.
python3 -c "import zipfile; zipfile.ZipFile('$SETUP/payload.zip','w').writestr('README.txt','placeholder payload; built by scripts/build-installer.sh')"
echo "built: $ROOT/installer/OpenPlayer-Setup-$VERSION.exe"
