package mediaext

import "testing"

func TestLists(t *testing.T) {
	m := Lists()
	if len(m["video"]) < 20 || len(m["audio"]) < 20 || len(m["subtitle"]) < 5 {
		t.Fatalf("lists too short: %v", m)
	}
	media := Media()
	if len(media) != 65 {
		t.Fatalf("want 65 media extensions, got %d", len(media))
	}
	for i := 1; i < len(media); i++ {
		if media[i] <= media[i-1] {
			t.Fatal("not sorted/unique")
		}
	}
}
