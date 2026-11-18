package main

import (
	"strings"
	"testing"
	"unsafe"
)

func TestCstrLong(t *testing.T) {
	want := strings.Repeat("تحميل اغنية - عمرو دياب - MP3.mp3 / ", 2000) // ~90 KB
	buf := append([]byte(want), 0)
	if got := cstr(uintptr(unsafe.Pointer(&buf[0]))); got != want {
		t.Fatalf("got %d bytes, want %d", len(got), len(want))
	}
	if cstr(0) != "" {
		t.Fatal("nil")
	}
}
