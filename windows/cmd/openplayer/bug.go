package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Bug reports (Help > Report a Bug). The report goes to the OpenPlayer
// server, where only the developer can read it; "Report on GitHub" opens a
// public issue instead, pre-filled without the email or the log.

const (
	bugAPI    = "https://ai4edutech.com/api/openplayer_api.php?action=bug"
	githubURL = "https://github.com/ahmadelhendawy1/openplayer"
)

type bugReport struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Lang    string `json:"lang"`
	What    string `json:"what"`
	Steps   string `json:"steps"`
	Email   string `json:"email"`
	Media   string `json:"media"`
	Log     string `json:"log,omitempty"`
}

// mediaSummary describes the playing file's format from mpv's "track-list"
// without its name, e.g. "h264 1920x1080 · aac 32000 Hz 1 ch".
func mediaSummary(trackListJSON, ext string) string {
	var tracks []struct {
		Type     string  `json:"type"`
		Codec    string  `json:"codec"`
		Selected bool    `json:"selected"`
		W        int     `json:"demux-w"`
		H        int     `json:"demux-h"`
		Rate     int     `json:"demux-samplerate"`
		Channels int     `json:"demux-channel-count"`
		FPS      float64 `json:"demux-fps"`
	}
	if json.Unmarshal([]byte(trackListJSON), &tracks) != nil {
		return ""
	}
	var parts []string
	if ext = strings.ToLower(strings.TrimPrefix(ext, ".")); ext != "" {
		parts = append(parts, ext)
	}
	for _, t := range tracks {
		if !t.Selected {
			continue
		}
		switch t.Type {
		case "video":
			s := t.Codec
			if t.W > 0 && t.H > 0 {
				s += fmt.Sprintf(" %dx%d", t.W, t.H)
			}
			if t.FPS > 0 {
				s += fmt.Sprintf(" %.3gfps", t.FPS)
			}
			parts = append(parts, s)
		case "audio":
			s := t.Codec
			if t.Rate > 0 {
				s += fmt.Sprintf(" %d Hz", t.Rate)
			}
			if t.Channels > 0 {
				s += fmt.Sprintf(" %d ch", t.Channels)
			}
			parts = append(parts, s)
		case "sub":
			parts = append(parts, "subtitles "+t.Codec)
		}
	}
	return strings.Join(parts, " · ")
}

// githubIssueURL opens a pre-filled public issue (no email, no log).
func githubIssueURL(r bugReport) string {
	title := strings.TrimSpace(strings.SplitN(r.What, "\n", 2)[0])
	if len([]rune(title)) > 90 {
		title = string([]rune(title)[:90]) + "…"
	}
	var b strings.Builder
	b.WriteString("**What went wrong**\n" + r.What + "\n\n")
	if strings.TrimSpace(r.Steps) != "" {
		b.WriteString("**Steps to reproduce**\n" + r.Steps + "\n\n")
	}
	b.WriteString("**OpenPlayer version:** " + r.Version + "\n**Windows:** " + r.OS + "\n")
	if r.Media != "" {
		b.WriteString("**Media:** " + r.Media + "\n")
	}
	body := b.String()
	if len(body) > 6000 {
		body = body[:6000]
	}
	return githubURL + "/issues/new?labels=bug&title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(body)
}

// lastLines keeps the end of a log (at most n lines and max bytes).
func lastLines(s string, n, maxBytes int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
	}
	return out
}
