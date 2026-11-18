package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "openplayer.conf")
	os.WriteFile(p, []byte("\uFEFF# written by installer\r\nauto_update=yes\r\n\r\nbad line\r\n"), 0o644)
	s := loadSettings(p)
	if !s.flag("auto_update", false) {
		t.Fatal("auto_update should be yes")
	}
	if s.flag("missing", true) != true || s.get("missing", "d") != "d" {
		t.Fatal("defaults not applied")
	}
	if err := s.set("auto_update", "no"); err != nil {
		t.Fatal(err)
	}
	if err := s.set("last_update_check", "123"); err != nil {
		t.Fatal(err)
	}
	s2 := loadSettings(p)
	if s2.flag("auto_update", true) || s2.get("last_update_check", "") != "123" {
		t.Fatalf("reload mismatch: %v", s2.m)
	}
}

func TestSettingsMissingFile(t *testing.T) {
	s := loadSettings(filepath.Join(t.TempDir(), "none.conf"))
	if !s.flag("auto_update", true) {
		t.Fatal("default should apply when file is missing")
	}
}
