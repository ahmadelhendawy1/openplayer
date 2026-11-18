package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
)

// encodeThumb turns BGRA pixels into a JPEG no larger than 320x180,
// ignoring alpha (Windows thumbnails often leave it at 0 although opaque).
func encodeThumb(bgra []byte, w, h int) []byte {
	scale := 1.0
	if s := min(320/float64(w), 180/float64(h)); s < 1 {
		scale = s
	}
	tw, th := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	img := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := y*h/th, max((y+1)*h/th, y*h/th+1)
		for x := 0; x < tw; x++ {
			x0, x1 := x*w/tw, max((x+1)*w/tw, x*w/tw+1)
			var r, g, b, n int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					i := (sy*w + sx) * 4
					b += int(bgra[i])
					g += int(bgra[i+1])
					r += int(bgra[i+2])
					n++
				}
			}
			img.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, img, &jpeg.Options{Quality: 82}) != nil {
		return nil
	}
	return out.Bytes()
}
