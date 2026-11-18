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
