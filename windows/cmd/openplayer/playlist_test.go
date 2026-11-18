package main

import "testing"

func TestBuildM3U(t *testing.T) {
	js := `[{"filename":"C:\\Music\\فيروز - أنا لحبيبي.mp3","title":"Ana La Habibi"},{"filename":"D:\\v\\film.mkv","current":true},{"filename":""},{"filename":"bad\nname"}]`
	got, n := buildM3U(js)
	want := "#EXTM3U\r\n#EXTINF:-1,Ana La Habibi\r\nC:\\Music\\فيروز - أنا لحبيبي.mp3\r\nD:\\v\\film.mkv\r\n"
	if n != 2 || got != want {
		t.Fatalf("n=%d\n%q\nwant\n%q", n, got, want)
	}
	if _, n := buildM3U("not json"); n != 0 {
		t.Fatal("bad json should give 0")
	}
}

func TestCurrentEntry(t *testing.T) {
	js := `[{"filename":"C:\\a.mp4"},{"filename":""},{"filename":"C:\\b.mp4","current":true,"playing":true}]`
	if i, f := currentEntry(js); i != 1 || f != `C:\b.mp4` {
		t.Fatalf("got %d %q", i, f)
	}
	if i, _ := currentEntry(`[{"filename":"x"}]`); i != -1 {
		t.Fatalf("nothing current: %d", i)
	}
	if i, _ := currentEntry(`bad`); i != -1 {
		t.Fatal("bad json")
	}
}

func TestRewindTarget(t *testing.T) {
	cases := []struct {
		pos, dur, rw, want float64
		ok                 bool
	}{
		{0, 600, 5, 0, false},     // new file
		{120, 600, 5, 115, true},  // resumed: go back 5 s
		{3, 600, 5, 0, true},      // never below 0
		{120, 600, 0, 0, false},   // rewind off
		{595, 600, 5, 0, true},    // stopped at the end: from the start
		{595, 600, 0, 0, true},    // even with rewind off
		{120, 0, 5, 115, true},    // unknown length
	}
	for _, c := range cases {
		got, ok := rewindTarget(c.pos, c.dur, c.rw)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("rewindTarget(%v,%v,%v) = %v,%v want %v,%v", c.pos, c.dur, c.rw, got, ok, c.want, c.ok)
		}
	}
}
