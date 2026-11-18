package main

// New-version notice. The download page publishes a small latest.json next to
// the installer; the player reads it at most once a day and tells the user
// when a newer OpenPlayer is out. Nothing is downloaded or installed
// automatically: the user is sent to the download page.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var (
	installerName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}\.exe$`)
	sha256Hex     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// canSelfUpdate: the player may download and start the installer itself
// only when latest.json names it and gives its SHA-256.
func (l latestInfo) canSelfUpdate() bool { return l.File != "" && l.SHA256 != "" }

// installerURL is the installer's address: the folder of latest.json + file.
func installerURL(latest, file string) string {
	i := strings.LastIndex(latest, "/")
	if i < 0 || file == "" {
		return ""
	}
	return latest[:i+1] + file
}

// notesList splits release notes into bullet points: one per line, or one
// per sentence when the notes are a single paragraph.
func notesList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var parts []string
	if strings.Contains(s, "\n") {
		parts = strings.Split(s, "\n")
	} else {
		parts = regexp.MustCompile(`([.!؟?])\s+`).Split(s, -1)
	}
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(p), "-•*·"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// latestURL can be changed at build time with -X main.latestURL=...
var latestURL = "https://ai4edutech.com/apps/openplayer/latest.json"

type latestInfo struct {
	Version string  `json:"version"`
	URL     string  `json:"url"`     // download page
	File    string  `json:"file"`    // installer, next to latest.json
	SHA256  string  `json:"sha256"`  // of the installer (required for "Update now")
	SizeMB  float64 `json:"size_mb"` // shown to the user
	Date    string  `json:"date"`    // YYYY-MM-DD
	NotesEN string  `json:"notes_en"`
	NotesAR string  `json:"notes_ar"`
	Force   bool    `json:"force"` // important update: ask at every start ("Later" lasts one session)
}

// parseLatest validates latest.json. The link must be https and the notes
// short, so a damaged file can't do anything surprising.
func parseLatest(b []byte) (latestInfo, bool) {
	var l latestInfo
	if json.Unmarshal(b, &l) != nil || versionParts(l.Version) == nil {
		return latestInfo{}, false
	}
	if !strings.HasPrefix(l.URL, "https://") || len(l.URL) > 300 || strings.ContainsAny(l.URL, " \"<>\r\n") {
		return latestInfo{}, false
	}
	l.NotesEN, l.NotesAR = clip(l.NotesEN, 3000), clip(l.NotesAR, 3000)
	if !installerName.MatchString(l.File) {
		l.File = ""
	}
	if !sha256Hex.MatchString(strings.ToLower(l.SHA256)) {
		l.SHA256 = ""
	}
	l.SHA256 = strings.ToLower(l.SHA256)
	return l, true
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func versionParts(v string) []int {
	p := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(p) < 2 || len(p) > 4 {
		return nil
	}
	out := make([]int, len(p))
	for i, s := range p {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// isNewer reports whether version a is newer than b ("0.10.0" > "0.9.2").
func isNewer(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}
