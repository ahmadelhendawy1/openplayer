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
  # automatic codec updates: where the signed engine packs are published, and the key that must have signed them
  ENGINE_PUB=$(cat "${OPENPLAYER_ENGINE_PUB:-$ROOT/engine/engine.pub}")
  go build -trimpath -ldflags "-s -w -H windowsgui -X main.updateURL=https://github.com/ahmadelhendawy1/openplayer/releases/download/engine -X main.pubKey=$ENGINE_PUB" -o "$STAGE/openplayer.exe" ./cmd/openplayer
  go build -trimpath -ldflags "-s -w" -o "$STAGE/enginectl.exe" ./cmd/enginectl )

# The engine folder (libmpv + patched uosc controls + fonts) is built as a
# signed engine pack, exactly like the automatic codec updates, so the player
# knows which engine it has and only downloads what changes.
ENGINE_KEY="${OPENPLAYER_ENGINE_KEY:-$ROOT/../openplayer-keys/engine.key}"
[ -f "$ENGINE_KEY" ] || { echo "engine signing key not found: $ENGINE_KEY (set OPENPLAYER_ENGINE_KEY)"; exit 1; }
BDATE=$(python3 -c "import json;print(json.load(open('$ROOT/engine/engine.json'))['bundled_date'])")
BGIT=$(python3 -c "import json;print(json.load(open('$ROOT/engine/engine.json'))['bundled_git'])")
PACK="$ROOT/build/engine-pack"
( unset GOOS GOARCH; "$ROOT/scripts/make-engine-pack.sh" "$ENGINE_DLL" "$UOSC" "$BDATE" "$BGIT" "$ENGINE_KEY" "$PACK" )
python3 - "$PACK" "$STAGE/engine" <<'PY'
import json, os, shutil, sys
pack, dst = sys.argv[1], sys.argv[2]
m = json.load(open(os.path.join(pack, "manifest.json")))
for c in m["components"]:
    out = os.path.join(dst, *c["path"].split("/"))
    os.makedirs(os.path.dirname(out), exist_ok=True)
    shutil.copyfile(os.path.join(pack, "files", c["sha256"]), out)
for f in ("manifest.json", "manifest.json.sig"):
    shutil.copyfile(os.path.join(pack, f), os.path.join(dst, f))
print("engine: %s (serial %d, %d files)" % (m["engine_version"], m["serial"], len(m["components"])))
PY
# fonts also in config\fonts as a fallback for older engines
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
