#!/usr/bin/env bash
# Builds a signed OpenPlayer engine pack: libmpv-2.dll + the patched uosc
# on-screen controls, signed with the engine key.
#
#   make-engine-pack.sh <libmpv-2.dll> <uosc-checkout> <libmpv-date YYYYMMDD> <libmpv-git> <key-file> <out-dir>
#
# The serial is YYYYMMDD*100 + uosc_rev (engine/engine.json), so a newer libmpv
# build, or a newer pinned uosc, always gets a higher serial.
set -euo pipefail
DLL=${1:?libmpv-2.dll}; UOSC=${2:?uosc checkout}; DATE=${3:?YYYYMMDD}; GIT=${4:?git hash}; KEY=${5:?key file}; OUT=${6:?out dir}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
[[ $DATE =~ ^[0-9]{8}$ ]] || { echo "bad date $DATE"; exit 1; }
REV=$(python3 -c "import json,sys;print(int(json.load(open(sys.argv[1]))['uosc_rev']))" "$ROOT/engine/engine.json")
UOSC_COMMIT=$(git -C "$UOSC" rev-parse --short=8 HEAD)
SERIAL=$((10#$DATE * 100 + REV))
VERSION="libmpv ${DATE:0:4}-${DATE:4:2}-${DATE:6:2} (${GIT:0:9}) + uosc ${UOSC_COMMIT}"

WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/in/skin"
cp "$DLL" "$WORK/in/libmpv-2.dll"
cp -r "$UOSC/src/uosc" "$WORK/in/skin/uosc"
cp -r "$UOSC/src/fonts" "$WORK/in/skin/fonts"
for p in "$ROOT"/third_party/uosc-patches/*.patch; do
  ( cd "$WORK/in/skin" && patch -s -p2 < "$p" ) || { echo "uosc patch failed: $p"; exit 1; }
done
find "$WORK/in" -name '*.orig' -delete
"$ROOT/scripts/license-gate.sh" "$WORK/in"
cat > "$WORK/spec.json" <<JSON
[
  {"name":"libmpv","version":"${DATE}-${GIT:0:9}","file":"libmpv-2.dll","license":"LGPL-2.1-or-later"},
  {"name":"uosc","version":"${UOSC_COMMIT}","file":"skin/uosc","path":"skin/uosc","license":"LGPL-2.1-or-later"},
  {"name":"uosc-fonts","version":"${UOSC_COMMIT}","file":"skin/fonts","path":"skin/fonts","license":"LGPL-2.1-or-later"}
]
JSON
rm -rf "$OUT"
( cd "$ROOT" && go run ./cmd/enginectl pack --in "$WORK/in" --out "$OUT" --spec "$WORK/spec.json" \
    --serial "$SERIAL" --version "$VERSION" --key "$KEY" )
echo "serial=$SERIAL"
echo "version=$VERSION"
