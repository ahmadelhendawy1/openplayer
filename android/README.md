# OpenPlayer for Android

Same features and look as the Windows player: library, playlist, 10-band equalizer with presets,
normalize, amplifier up to 500 %, subtitles, video gestures, 11 themes, Arabic interface and
in-app updates. It uses Android's built-in media engine (MediaPlayer), so which formats play depends
on the phone.

- Java, Android framework only (no AndroidX), minSdk 24, targetSdk 35
- Interface: `assets/ui/index.html` (WebView on top of the video surface)
- Arabic strings: `i18n/android_ar.json` plus the Windows player's `lang_ar.json`

## Build (Linux, no Gradle)

1. Put the tools in `tools/` (see `tools/README.md`).
2. Create a release key once and keep it private:
   ```
   mkdir -p keystore
   python3 -c "import secrets;print(secrets.token_urlsafe(24))" > keystore/password.txt
   keytool -genkeypair -keystore keystore/openplayer-release.p12 -storetype PKCS12 -alias openplayer \
     -keyalg RSA -keysize 4096 -validity 10000 -storepass "$(cat keystore/password.txt)" \
     -keypass "$(cat keystore/password.txt)" -dname "CN=OpenPlayer"
   ```
   Official releases are signed with the project's own key. A build signed with a different key
   cannot update an installed official copy.
3. Run `./build.sh` to make `dist/OpenPlayer-Android-<version>.apk`.

`build.sh` runs aapt2, javac, d8, then apksig, which aligns, signs (v2 and v3) and verifies the APK.
