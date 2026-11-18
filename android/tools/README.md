# Android build tools

These are not stored in the repository. Put them in this folder before running `../build.sh`.
`TOOLS.sha256` lists the exact files the 0.9.6 release was built with.

| File | Where it comes from |
|---|---|
| `aapt2` | Linux `aapt2` from the Android SDK build-tools, or the prebuilt in the Apktool repository (`brut.apktool/apktool-lib/src/main/resources/prebuilt/linux/aapt2_64`) |
| `android.jar` | Android SDK `platforms/android-35/android.jar` |
| `apksig.jar` | `com.android.tools.build:apksig:8.7.3` (Google Maven) |
| `d8.jar` | Android SDK `build-tools/<version>/lib/d8.jar`, or `com.android.tools.build:builder:8.7.3`, which contains D8 |

Check them with `sha256sum -c TOOLS.sha256`.
