# Automatic engine (codec) updates

OpenPlayer's playback engine — `libmpv-2.dll` (mpv with FFmpeg and all the
codecs inside) plus the on-screen controls (uosc) — updates by itself.

## How it works
1. **Weekly robot** (`.github/workflows/engine-robot.yml`, Mondays 03:17 UTC,
   or Actions › engine-robot › Run workflow): finds the newest LGPL 64-bit
   libmpv build from the builder in `engine.json`.
2. **Licence gate**: refuses GPL/non-free builds (`scripts/license-gate.sh`).
3. **Test on Windows** (`scripts/engine-test.py`): every file in `testmedia/`
   (H.264/AAC, 32 kHz phone audio, HEVC/AC3 5.1, VP9/Opus, AV1/Opus, MP3,
   16 kHz FLAC) must open, play and give real sound through OpenPlayer's full
   audio chain (normalize + equalizer incl. 16 kHz + amplifier + limiter), and
   the controls must load without errors.
4. **Sign** with the engine key (repository secret `ENGINE_SIGNING_KEY`) and
   **publish** to the GitHub release tagged `engine`.
5. **Every OpenPlayer** checks that release once a day, downloads only the
   files that changed, verifies the signature and hashes, and installs the new
   engine the next time it starts. The previous engine is kept for rollback.

If anything fails nothing is published, users keep their working engine, and
the robot opens an issue.

## Files
| File | Purpose |
|---|---|
| `engine.json` | builder repo + file pattern, pinned uosc commit, `uosc_rev`, the engine bundled in the installer |
| `engine.pub` | public key built into the player (safe to publish) |
| `testmedia/` | small test clips the robot plays |

## Serial numbers
`serial = YYYYMMDD (libmpv build date) × 100 + uosc_rev`. A newer libmpv, or
raising `uosc_rev` after changing the pinned uosc commit, always publishes a
higher serial; players never accept a lower one.

## Changing the controls (uosc)
Update `uosc_commit`, add 1 to `uosc_rev`, then run the robot with **force**.
