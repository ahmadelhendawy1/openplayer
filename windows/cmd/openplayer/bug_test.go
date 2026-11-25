package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestMediaSummary(t *testing.T) {
	js := `[{"type":"video","codec":"h264","selected":true,"demux-w":1280,"demux-h":720,"demux-fps":30},
	{"type":"audio","codec":"aac","selected":true,"demux-samplerate":32000,"demux-channel-count":1},
	{"type":"audio","codec":"ac3","selected":false}]`
	got := mediaSummary(js, ".MP4")
	want := "mp4 · h264 1280x720 30fps · aac 32000 Hz 1 ch"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if mediaSummary("bad", "mkv") != "" {
		t.Fatal("bad json")
	}
}

func TestGithubIssueURLPrivate(t *testing.T) {
	u := githubIssueURL(bugReport{Version: "0.9.13", OS: "Windows 11", What: "No sound\nmore", Steps: "1. open",
		Email: "me@example.com", Log: "secret-path C:\\Users\\me\\film.mkv", Media: "mp4 · aac"})
	p, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := p.Query()
	if q.Get("title") != "No sound" || !strings.Contains(q.Get("body"), "1. open") || q.Get("labels") != "bug" {
		t.Fatalf("bad issue: %v", q)
	}
	if strings.Contains(u, "example.com") || strings.Contains(u, "secret-path") {
		t.Fatal("email or log must never go to GitHub")
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2, 100); got != "c\nd" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("abcdef", 10, 3); got != "def" {
		t.Fatalf("got %q", got)
	}
}
