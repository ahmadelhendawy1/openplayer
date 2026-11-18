//go:build windows

package main

// End of a video (keep-open): instead of freezing on the last frame, where
// Play does nothing, OpenPlayer rewinds to the beginning and waits, paused.
// Pressing Play (button, click, Space, menu) then plays it from the start.
// A video watched to the end also forgets its saved position, so opening it
// again (for example from Explorer) starts at the beginning, not at the end.
// Runs on the mpv event goroutine.

import (
	"sync"
	"time"
)

var (
	eofMu       sync.Mutex
	eofAtEnd    bool      // eof-reached is "yes"
	eofLoadedAt time.Time // when the current file finished loading
	eofUsed     bool      // the "opened at its very end" fix ran for this file
)

func onFileLoaded() {
	eofMu.Lock()
	eofLoadedAt, eofUsed = time.Now(), false
	eofMu.Unlock()
	resumeOnLoad()
}

func onEOFReached(v string) {
	eofMu.Lock()
	at := v == "yes"
	was := eofAtEnd
	eofAtEnd = at
	// an old saved position (from an earlier version) opened the file at its end
	justOpened := at && !eofUsed && !eofLoadedAt.IsZero() && time.Since(eofLoadedAt) < 2*time.Second
	if justOpened {
		eofUsed = true
	}
	eofMu.Unlock()
	if !at || was {
		return
	}
	mpv.Async(mp, "delete-watch-later-config") // forget the "resume at the end" position
	mpv.Async(mp, "seek", "0", "absolute", "exact")
	if justOpened && !restorePaused.Load() {
		mpv.Async(mp, "set", "pause", "no") // opened from Explorer: just play it
	} else {
		mpv.Async(mp, "set", "pause", "yes") // finished: wait at the start for Play
	}
}
