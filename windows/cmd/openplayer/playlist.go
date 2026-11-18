package main

// Playlist files: M3U8 (UTF-8, so Arabic names stay intact), one absolute
// path per line, with the title when mpv knows one.

import (
	"encoding/json"
	"strings"
)

type plEntry struct {
	Filename string `json:"filename"`
	Title    string `json:"title"`
	Current  bool   `json:"current"`
}

// currentEntry returns the playing entry's index and file name in mpv's
// "playlist" property (-1, "" when nothing is playing).
func currentEntry(playlistJSON string) (int, string) {
	var pl []plEntry
	if json.Unmarshal([]byte(playlistJSON), &pl) != nil {
		return -1, ""
	}
	n := 0
	for _, e := range pl {
		f := strings.TrimSpace(e.Filename)
		if f == "" || strings.ContainsAny(f, "\r\n") {
			continue // buildM3U skips these too, so indexes match the saved list
		}
		if e.Current {
			return n, f
		}
		n++
	}
	return -1, ""
}

// rewindTarget decides where a resumed file should start: a few seconds
// before the saved moment (so the viewer remembers what was happening), or
// at the beginning when it was stopped at the very end. ok=false: leave it.
func rewindTarget(pos, dur, rewind float64) (float64, bool) {
	if pos < 1 { // not resumed (a new file starts at 0)
		return 0, false
	}
	if dur > 0 && dur-pos < 10 {
		return 0, true // stopped at the end: watch it again from the start
	}
	if rewind <= 0 {
		return 0, false
	}
	return max(pos-rewind, 0), true
}

// buildM3U turns mpv's "playlist" property (JSON) into M3U8 text.
// It returns the text and the number of entries written.
func buildM3U(playlistJSON string) (string, int) {
	var pl []plEntry
	if json.Unmarshal([]byte(playlistJSON), &pl) != nil {
		return "", 0
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\r\n")
	n := 0
	for _, e := range pl {
		f := strings.TrimSpace(e.Filename)
		if f == "" || strings.ContainsAny(f, "\r\n") {
			continue
		}
		if t := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(e.Title)); t != "" {
			b.WriteString("#EXTINF:-1," + t + "\r\n")
		}
		b.WriteString(f + "\r\n")
		n++
	}
	return b.String(), n
}
