//go:build windows

package main

// One volume for every video. mpv normally remembers volume per file (with
// the resume position), so a new video started at 100 % again after you had
// turned it down. OpenPlayer now keeps a single volume: whatever you set is
// used for the next video and the next time OpenPlayer starts.
// Runs on the mpv event goroutine.

import (
	"strconv"
	"sync"
	"time"
)

var (
	volMu      sync.Mutex
	volLoading bool        // between start-file and file-loaded: mpv may apply an old per-file volume
	volSaved   string      // the player-wide volume, e.g. "45"
	volTimer   *time.Timer // saves to the settings file a moment after the last change
)

// startVolume is the engine option value at start-up.
func startVolume() string {
	v := cfg.get("volume", "100")
	if f, err := strconv.ParseFloat(v, 64); err != nil || f < 0 || f > 1000 {
		v = "100"
	}
	volMu.Lock()
	volSaved = v
	volMu.Unlock()
	return v
}

func onStartFile() {
	volMu.Lock()
	volLoading = true
	volMu.Unlock()
}

// onFileLoadedVolume puts the player-wide volume back, replacing any volume
// an older version saved with this file.
func onFileLoadedVolume() {
	volMu.Lock()
	v := volSaved
	volLoading = false
	volMu.Unlock()
	if v != "" {
		mpv.Async(mp, "set", "volume", v)
	}
	mpv.Async(mp, "set", "mute", "no") // a file never opens silently because of an old saved mute
}

func onVolumeProp(v string) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return
	}
	v = strconv.FormatFloat(f, 'f', -1, 64)
	volMu.Lock()
	defer volMu.Unlock()
	if volLoading || v == volSaved {
		return
	}
	volSaved = v
	if volTimer != nil {
		volTimer.Stop()
	}
	volTimer = time.AfterFunc(time.Second, func() {
		volMu.Lock()
		s := volSaved
		volMu.Unlock()
		_ = cfg.set("volume", s)
	})
}

// saveVolumeNow runs at shutdown so a change in the last second is kept.
func saveVolumeNow() {
	volMu.Lock()
	s := volSaved
	if volTimer != nil {
		volTimer.Stop()
	}
	volMu.Unlock()
	if s != "" {
		_ = cfg.set("volume", s)
	}
}
