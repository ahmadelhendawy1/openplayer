package main

// Audio effects: 10-band graphic equalizer with preamp, presets and user
// presets, dynamic normalization, and an amplifier up to 500 %. Everything is
// one FFmpeg filter graph that mpv runs as the "@op" audio filter.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// eqFreqs are the band centres (ISO octave bands, like most 10-band EQs).
var eqFreqs = [10]int{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

const (
	eqMaxDB      = 12.0
	ampMin       = 100
	ampMax       = 500
	presetCustom = "Custom"
)

type eqPreset struct {
	Name  string
	Pre   float64
	Bands [10]float64
}

// Built-in presets (dB per band, 31 Hz ... 16 kHz).
var eqPresets = []eqPreset{
	{Name: "Flat", Bands: [10]float64{}},
	{Name: "Bass Boost", Bands: [10]float64{7, 6, 5, 3, 1, 0, 0, 0, 0, 0}},
	{Name: "Treble Boost", Bands: [10]float64{0, 0, 0, 0, 0, 1, 3, 5, 6, 7}},
	{Name: "Bass & Treble", Bands: [10]float64{6, 5, 3, 0, -1, 0, 1, 3, 5, 6}},
	{Name: "Loudness", Bands: [10]float64{6, 4, 1, 0, -1, -1, 0, 2, 4, 5}},
	{Name: "Rock", Bands: [10]float64{5, 4, 3, 1, -1, -1, 1, 3, 4, 5}},
	{Name: "Pop", Bands: [10]float64{-1, 1, 3, 4, 3, 1, -1, -1, -1, -1}},
	{Name: "Jazz", Bands: [10]float64{3, 2, 1, 2, -1, -1, 0, 1, 2, 3}},
	{Name: "Classical", Bands: [10]float64{4, 3, 2, 1, -1, -1, 0, 2, 3, 4}},
	{Name: "Dance", Bands: [10]float64{7, 6, 3, 0, 0, -2, -3, -3, 0, 0}},
	{Name: "Electronic", Bands: [10]float64{4, 4, 1, 0, -2, 2, 1, 1, 3, 4}},
	{Name: "Hip-Hop", Bands: [10]float64{5, 4, 1, 3, -1, -1, 1, -1, 2, 3}},
	{Name: "Acoustic", Bands: [10]float64{4, 4, 3, 1, 2, 1, 3, 3, 3, 2}},
	{Name: "Arabic / Tarab", Bands: [10]float64{2, 3, 2, 1, 0, 2, 3, 2, 1, 0}},
	{Name: "Vocal", Bands: [10]float64{-2, -3, -3, 1, 4, 4, 3, 1, 0, -2}},
	{Name: "Movie / Dialogue", Bands: [10]float64{2, 1, 0, 0, 2, 3, 3, 1, 0, 0}},
	{Name: "Live", Bands: [10]float64{-3, 0, 2, 3, 3, 3, 2, 1, 1, 1}},
	{Name: "Headphones", Bands: [10]float64{4, 3, 1, 0, -1, 0, 1, 2, 3, 3}},
	{Name: "Laptop Speakers", Bands: [10]float64{-3, -1, 1, 2, 3, 2, 2, 2, 3, 2}},
}

type audioFX struct {
	EQ     bool    // equalizer on
	Preset string  // preset name, a user preset name, or "Custom"
	Pre    float64 // preamp dB
	Bands  [10]float64
	Norm   bool // dynamic normalization
	Amp    int  // amplifier, percent (100 = off)
}

func defaultFX() audioFX { return audioFX{Preset: "Flat", Amp: 100} }

func clampDB(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-eqMaxDB, math.Min(eqMaxDB, math.Round(v*10)/10))
}

func clampAmp(a int) int {
	if a < ampMin {
		return ampMin
	}
	if a > ampMax {
		return ampMax
	}
	return a
}

// filter returns the value for mpv's "af" property ("" = no processing, so
// audio stays bit-exact when every effect is off).
func (f audioFX) filter() string {
	var parts []string
	boost := false
	if f.Norm {
		parts = append(parts, "dynaudnorm@norm=f=150:g=15:p=0.95:m=10")
	}
	if f.EQ {
		eq := false
		for _, g := range f.Bands {
			eq = eq || g != 0
		}
		if eq {
			// An equalizer band at or above half the sample rate silences
			// the audio (the 16 kHz band on 32 kHz audio, the 8 kHz band on
			// 16 kHz voice recordings). Resampling to 48 kHz first keeps
			// every band valid for any file.
			parts = append(parts, "aresample@sr=48000")
		}
		if f.Pre != 0 {
			parts = append(parts, fmt.Sprintf("volume@pre=volume=%sdB", fnum(f.Pre)))
			boost = boost || f.Pre > 0
		}
		for i, g := range f.Bands {
			if g == 0 {
				continue
			}
			parts = append(parts, fmt.Sprintf("equalizer@b%d=f=%d:t=o:w=1:g=%s", i, eqFreqs[i], fnum(g)))
			boost = boost || g > 0
		}
	}
	if a := clampAmp(f.Amp); a != 100 {
		parts = append(parts, fmt.Sprintf("volume@amp=volume=%s", fnum(float64(a)/100)))
		boost = true
	}
	if len(parts) == 0 {
		return ""
	}
	if boost { // keep boosted audio from clipping
		parts = append(parts, "alimiter@lim=limit=0.98:attack=5:release=50:level=disabled")
	}
	return "@op:lavfi=[" + strings.Join(parts, ",") + "]"
}

func fnum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// encode/decode the state for the settings file.
func (f audioFX) encode() string {
	return fmt.Sprintf("eq=%d;preset=%s;pre=%s;bands=%s;norm=%d;amp=%d",
		b2i(f.EQ), strings.NewReplacer(";", "", "=", "").Replace(f.Preset), fnum(f.Pre), encodeBands(f.Bands), b2i(f.Norm), f.Amp)
}

func decodeFX(s string) audioFX {
	f := defaultFX()
	for _, kv := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "eq":
			f.EQ = v == "1"
		case "preset":
			f.Preset = v
		case "pre":
			p, _ := strconv.ParseFloat(v, 64)
			f.Pre = clampDB(p)
		case "bands":
			f.Bands, _ = decodeBands(v)
		case "norm":
			f.Norm = v == "1"
		case "amp":
			a, _ := strconv.Atoi(v)
			f.Amp = clampAmp(a)
		}
	}
	return f
}

func encodeBands(b [10]float64) string {
	s := make([]string, len(b))
	for i, v := range b {
		s[i] = fnum(v)
	}
	return strings.Join(s, ",")
}

func decodeBands(s string) ([10]float64, bool) {
	var b [10]float64
	p := strings.Split(s, ",")
	if len(p) != 10 {
		return b, false
	}
	for i, x := range p {
		v, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return [10]float64{}, false
		}
		b[i] = clampDB(v)
	}
	return b, true
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// User presets are stored as settings keys "eqp.<name>" = "pre|b1,...,b10".
const userPresetPrefix = "eqp."

func cleanPresetName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("=;|\\/", r) {
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(n)
	if len([]rune(n)) > 40 {
		n = string([]rune(n)[:40])
	}
	return n
}

func encodePreset(p eqPreset) string { return fnum(p.Pre) + "|" + encodeBands(p.Bands) }

func builtinPreset(name string) (eqPreset, bool) {
	for _, p := range eqPresets {
		if p.Name == name {
			return p, true
		}
	}
	return eqPreset{}, false
}

// userPresets reads "eqp.*" entries from a settings map.
func userPresets(all map[string]string) []eqPreset {
	var out []eqPreset
	for k, v := range all {
		if !strings.HasPrefix(k, userPresetPrefix) {
			continue
		}
		pre, bands, _ := strings.Cut(v, "|")
		b, ok := decodeBands(bands)
		if !ok {
			continue
		}
		pv, _ := strconv.ParseFloat(pre, 64)
		out = append(out, eqPreset{Name: strings.TrimPrefix(k, userPresetPrefix), Pre: clampDB(pv), Bands: b})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
