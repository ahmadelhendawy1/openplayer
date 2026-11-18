package main

import (
	"bytes"
	"image/jpeg"
	"testing"
)

func TestEncodeThumb(t *testing.T) {
	w, h := 640, 640
	px := make([]byte, w*h*4)
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+1], px[i+2], px[i+3] = 200, 100, 50, 0 // BGRA, alpha 0
	}
	b := encodeThumb(px, w, h)
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if s := img.Bounds().Size(); s.X != 180 || s.Y != 180 {
		t.Fatalf("size %v", s)
	}
	r, g, bl, _ := img.At(90, 90).RGBA()
	if r>>8 < 40 || r>>8 > 60 || g>>8 < 90 || g>>8 > 110 || bl>>8 < 190 {
		t.Fatalf("colour %d %d %d", r>>8, g>>8, bl>>8)
	}
	if small := encodeThumb(make([]byte, 4*100*50), 100, 50); small == nil {
		t.Fatal("small image failed")
	}
}
