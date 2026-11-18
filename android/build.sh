#!/usr/bin/env bash
# Builds the OpenPlayer Android APK without Gradle:
# aapt2 (resources) -> javac -> d8 (dex) -> zip -> apksig (align + sign + verify)
set -euo pipefail
cd "$(dirname "$0")"
T=tools; B=build
KS=${KS:-keystore/openplayer-release.p12}; KP=${KP:-keystore/password.txt}; ALIAS=openplayer
VN=$(grep -o 'versionName="[^"]*"' AndroidManifest.xml | cut -d'"' -f2)
VC=$(grep -o 'versionCode="[^"]*"' AndroidManifest.xml | cut -d'"' -f2)
grep -q "VERSION = \"$VN\"" src/com/ai4edutech/openplayer/BuildInfo.java || { echo "BuildInfo VERSION != $VN"; exit 1; }
grep -q "CODE = $VC;" src/com/ai4edutech/openplayer/BuildInfo.java || { echo "BuildInfo CODE != $VC"; exit 1; }
[ -f "$T/d8.jar" ] || { echo "tools/d8.jar missing (copy it from Android SDK build-tools/<ver>/lib/d8.jar)"; exit 1; }
python3 i18n/make_lang.py . "${WIN_LANG:-../windows/cmd/openplayer/lang_ar.json}"
rm -rf $B && mkdir -p $B/gen $B/classes $B/dex
$T/aapt2 compile --dir res -o $B/res.zip
$T/aapt2 link -o $B/unsigned.apk -I $T/android.jar --manifest AndroidManifest.xml -A assets --java $B/gen $B/res.zip \
  --min-sdk-version 24 --target-sdk-version 35 --version-code "$VC" --version-name "$VN"
javac -nowarn -Xlint:-options -source 11 -target 11 -encoding UTF-8 -cp $T/android.jar -d $B/classes $(find src $B/gen -name '*.java')
java -cp $T/d8.jar com.android.tools.r8.D8 --release --min-api 24 --lib $T/android.jar --output $B/dex $(find $B/classes -name '*.class')
python3 - "$B/unsigned.apk" "$B/dex/classes.dex" <<'PY'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "a", zipfile.ZIP_DEFLATED) as z:
    z.write(sys.argv[2], "classes.dex")
PY
mkdir -p $B/sign && javac -nowarn -cp $T/apksig.jar -d $B/sign buildtools/Signer.java
OUT=dist/OpenPlayer-Android-$VN.apk; mkdir -p dist
java -cp $T/apksig.jar:$B/sign Signer $B/unsigned.apk "$OUT" "$KS" "$KP" $ALIAS
sha256sum "$OUT"; ls -la "$OUT"
