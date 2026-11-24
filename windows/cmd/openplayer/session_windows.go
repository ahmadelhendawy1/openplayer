//go:build windows

package main

// Remember where you were.
//   - Resuming a video (mpv's saved position) goes back a few seconds
//     (Tools > Options > Playback, default 5 s) so you remember what was
//     happening. A video stopped at its very end starts from the beginning.
//   - Closing OpenPlayer saves the playlist and which item was playing.
//     Starting OpenPlayer without a file opens that item again, PAUSED at the
//     moment you stopped (minus the rewind): one click or Play continues.
//     Opening a file from Explorer always plays that file instead.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

var (
	sessionFile = filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "last-session.m3u8")
	// restorePaused: the next loaded file is the reopened session item and
	// must wait, paused, for the user.
	restorePaused atomic.Bool
	restoreTarget atomic.Value // string: the file that must open paused
)

// saveSession runs once when the window has closed (before mpv shuts down).
func saveSession() {
	stateMu.Lock()
	js := mpvState["playlist"]
	stateMu.Unlock()
	text, n := buildM3U(js)
	idx, _ := currentEntry(js)
	if n == 0 || idx < 0 {
		_ = os.Remove(sessionFile)
		_ = cfg.del("last_index")
		return
	}
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err == nil && os.WriteFile(sessionFile, []byte(text), 0o644) == nil {
		_ = cfg.set("last_index", strconv.Itoa(idx))
		logf("session saved: %d item(s), playing #%d", n, idx)
	}
}

// restoreSession runs at start-up when no file was given. It returns true
// when the last session was reopened.
func restoreSession() bool {
	if cfg.get("restore_session", "yes") != "yes" || cfg.get("mpv.save-position-on-quit", "yes") != "yes" {
		return false
	}
	b, err := os.ReadFile(sessionFile)
	if err != nil {
		return false
	}
	var files []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			files = append(files, l)
		}
	}
	idx, err := strconv.Atoi(cfg.get("last_index", "0"))
	if err != nil || idx < 0 || idx >= len(files) {
		return false
	}
	if !strings.Contains(files[idx], "://") {
		if _, err := os.Stat(files[idx]); err != nil {
			logf("session: %s is gone, not reopening", files[idx])
			return false // the file was moved or the USB drive is not there
		}
	}
	restoreTarget.Store(files[idx])
	restorePaused.Store(true)
	mpv.Async(mp, "set", "pause", "yes")
	mpv.Async(mp, "loadlist", sessionFile, "replace")
	if idx > 0 {
		mpv.Async(mp, "playlist-play-index", strconv.Itoa(idx))
	}
	logf("session: reopening item #%d of %d", idx, len(files))
	return true
}

// currentAF is the sound chain OpenPlayer wants right now (set by applyFX).
var currentAF atomic.Value

// resumeOnLoad runs on the mpv event goroutine when a file has loaded.
func resumeOnLoad() {
	// A file's saved settings from older versions may carry an old sound
	// chain ("af"); put the current equalizer / amplifier back.
	if af, ok := currentAF.Load().(string); ok {
		mpv.Async(mp, "set", "af", af)
	}
	pos, _ := strconv.ParseFloat(mpv.GetString(mp, "time-pos"), 64)
	dur, _ := strconv.ParseFloat(mpv.GetString(mp, "duration"), 64)
	rw, _ := strconv.ParseFloat(cfg.get("resume_rewind", "5"), 64)
	if target, ok := rewindTarget(pos, dur, rw); ok {
		logf("resume: saved at %.1f s, starting at %.1f s", pos, target)
		mpv.Async(mp, "seek", strconv.FormatFloat(target, 'f', 3, 64), "absolute", "exact")
	}
	if restorePaused.Load() {
		// the session item: wait for Play (mpv's saved state may say "playing")
		mpv.Async(mp, "set", "pause", "yes")
		want, _ := restoreTarget.Load().(string)
		if strings.EqualFold(filepath.Clean(mpv.GetString(mp, "path")), filepath.Clean(want)) {
			restorePaused.Store(false) // reached it: later files play normally
		}
	}
}

// cancelRestore: the user opened something else before the session item loaded.
func cancelRestore() { restorePaused.Store(false) }
