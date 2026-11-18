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
