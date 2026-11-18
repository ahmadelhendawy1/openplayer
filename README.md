# OpenPlayer

A fast, light, open-source media player for **Windows 10/11** and **Android**. It has a glass
interface, 11 themes and a full Arabic interface, with no ads, no tracking and no bundled extras.

**Download:** https://ai4edutech.com/apps/openplayer/

## Features
- Plays almost any audio or video file (libmpv / FFmpeg engine on Windows)
- Playlist with thumbnails, drag and drop, save and load (.m3u8)
- 10-band equalizer with 19 presets and your own presets
- Volume normalize and an amplifier up to 500 %
- Subtitles, audio tracks, playback speed and A-B repeat (plus a sleep timer on Android)
- One click on the video pauses or plays; a double-click enters or leaves full screen
- Remembers where you stopped: reopens your last video paused at that moment, going back 5 seconds so you remember what was happening
- Time display shows time played, time left and total length (for example `00:23` … `-00:19 / 00:42`)
- English and Arabic (right-to-left) interface
- Built-in update check that shows what's new; the download is verified with SHA-256 before it installs

## Repository
| Folder | |
|---|---|
| [`windows/`](windows/) | Windows player and installer (Go, WebView2, libmpv) |
| [`android/`](android/) | Android app (Java, WebView interface, Android MediaPlayer) |

Each folder has its own README with build steps.

## License
OpenPlayer is free software under the **GNU General Public License v3.0**. See [LICENSE](LICENSE).

Third-party parts keep their own licenses: mpv/libmpv and FFmpeg (LGPL-2.1+), uosc (LGPL-2.1),
go-webview2 and the other vendored Go modules (see `windows/third_party/`). The full per-library
notice for the Windows engine is still being completed (`windows/installer/NOTICE.txt`).
