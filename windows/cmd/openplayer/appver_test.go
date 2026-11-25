package main

import "testing"

func TestIsNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.9.3", "0.9.2", true}, {"0.10.0", "0.9.2", true}, {"1.0", "0.9.9", true}, {"0.9.2", "0.9.2", false},
		{"0.9.1", "0.9.2", false}, {"0.9.2.1", "0.9.2", true}, {"x", "0.9.2", false}, {"0.9", "0.9.0", false}} {
		if got := isNewer(c.a, c.b); got != c.want {
			t.Errorf("isNewer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

func TestParseLatest(t *testing.T) {
	full := `{"version":"0.9.5","file":"OpenPlayer-Setup-0.9.5.exe","size_mb":51.3,"sha256":"e0","date":"2026-11-06","url":"https://ai4edutech.com/apps/openplayer/","notes_en":"x","notes_ar":"y"}`
	if l, ok := parseLatest([]byte(full)); !ok || l.Version != "0.9.5" || l.NotesAR != "y" {
		t.Fatal("site latest.json rejected")
	}
	if _, ok := parseLatest([]byte(`{"version":"0.9.3","url":"https://ai4edutech.com/apps/openplayer/"}`)); !ok {
		t.Fatal("valid file rejected")
	}
	for _, bad := range []string{`{"version":"0.9.3","url":"http://x"}`, `{"version":"abc","url":"https://x"}`, `nope`, `{"version":"1.0","url":"https://x\"y"}`} {
		if _, ok := parseLatest([]byte(bad)); ok {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestReleaseFields(t *testing.T) {
	l, ok := parseLatest([]byte(`{"version":"0.9.6","url":"https://a.b/x/","file":"OpenPlayer-Setup-0.9.6.exe","sha256":"E042E7E4DE16484E70B7C1D8C22D5CCE71889F23C4C254AF2632E3367F37803F"}`))
	if !ok || !l.canSelfUpdate() || l.SHA256[0] != 'e' {
		t.Fatalf("%+v", l)
	}
	bad, _ := parseLatest([]byte(`{"version":"0.9.6","url":"https://a.b/x/","file":"../evil.exe","sha256":"zz"}`))
	if bad.canSelfUpdate() || bad.File != "" {
		t.Fatalf("unsafe file accepted: %+v", bad)
	}
	if u := installerURL("https://ai4edutech.com/apps/openplayer/latest.json", "OpenPlayer-Setup-0.9.6.exe"); u != "https://ai4edutech.com/apps/openplayer/OpenPlayer-Setup-0.9.6.exe" {
		t.Fatal(u)
	}
	if n := notesList("A fixed. B added. C faster."); len(n) != 3 || n[0] != "A fixed" {
		t.Fatalf("%q", n)
	}
	if n := notesList("- one\n• two\n\nthree"); len(n) != 3 || n[1] != "two" {
		t.Fatalf("%q", n)
	}
}

func TestForceFlag(t *testing.T) {
	l, ok := parseLatest([]byte(`{"version":"1.0.0","url":"https://a.b/","force":true}`))
	if !ok || !l.Force {
		t.Fatal("force not read")
	}
	l, _ = parseLatest([]byte(`{"version":"1.0.0","url":"https://a.b/"}`))
	if l.Force {
		t.Fatal("force should default to false")
	}
}

func TestMinSupported(t *testing.T) {
	l, ok := parseLatest([]byte(`{"version":"0.9.14","url":"https://ai4edutech.com/apps/openplayer/","min_supported":"0.9.10"}`))
	if !ok || l.MinSupported != "0.9.10" {
		t.Fatalf("parse: %v %+v", ok, l)
	}
	if !l.obsolete("0.9.9") || l.obsolete("0.9.10") || l.obsolete("0.9.13") {
		t.Fatal("obsolete check wrong")
	}
	l2, _ := parseLatest([]byte(`{"version":"0.9.14","url":"https://x.y/","min_supported":"soon"}`))
	if l2.obsolete("0.1.0") {
		t.Fatal("a bad min_supported must be ignored")
	}
}
