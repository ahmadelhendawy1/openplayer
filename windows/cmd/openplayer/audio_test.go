package main

import (
	"strings"
	"testing"
)

func TestFilterOffIsEmpty(t *testing.T) {
	if f := defaultFX().filter(); f != "" {
		t.Fatalf("default should not filter, got %q", f)
	}
	f := defaultFX()
	f.Bands[3] = 6 // EQ off: bands ignored
	if f.filter() != "" {
		t.Fatal("bands applied while EQ off")
	}
}

func TestFilterChain(t *testing.T) {
	f := audioFX{EQ: true, Pre: -3, Norm: true, Amp: 500}
	f.Bands[0], f.Bands[9] = 6, -2.5
	got := f.filter()
	want := "@op:lavfi=[dynaudnorm@norm=f=150:g=15:p=0.95:m=10,aresample@sr=48000,volume@pre=volume=-3dB,equalizer@b0=f=31:t=o:w=1:g=6,equalizer@b9=f=16000:t=o:w=1:g=-2.5,volume@amp=volume=5,alimiter@lim=limit=0.98:attack=5:release=50:level=disabled]"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	cut := audioFX{EQ: true, Amp: 100}
	cut.Bands[2] = -4
	if strings.Contains(cut.filter(), "alimiter") {
		t.Fatal("limiter added without any boost")
	}
}

func TestEncodeDecode(t *testing.T) {
	f := audioFX{EQ: true, Preset: "Rock", Pre: 1.5, Norm: true, Amp: 250}
	f.Bands = [10]float64{1, 2, 3, 4, 5, -1, -2, -3, -4, 12}
	if g := decodeFX(f.encode()); g != f {
		t.Fatalf("round trip: %+v != %+v", g, f)
	}
	g := decodeFX("eq=1;amp=9000;pre=99;bands=1,2")
	if g.Amp != ampMax || g.Pre != eqMaxDB || g.Bands != [10]float64{} {
		t.Fatalf("clamping failed: %+v", g)
	}
}

func TestUserPresets(t *testing.T) {
	m := map[string]string{"eqp.Zeta": encodePreset(eqPreset{Pre: -2, Bands: [10]float64{1}}), "eqp.alpha": "0|1,1,1,1,1,1,1,1,1,1", "eqp.bad": "x", "theme": "x"}
	ps := userPresets(m)
	if len(ps) != 2 || ps[0].Name != "alpha" || ps[1].Name != "Zeta" || ps[1].Pre != -2 {
		t.Fatalf("%+v", ps)
	}
	if n := cleanPresetName("  My=Set;|\\ "); n != "MySet" {
		t.Fatalf("clean: %q", n)
	}
	for _, p := range eqPresets {
		for _, b := range p.Bands {
			if b > eqMaxDB || b < -eqMaxDB {
				t.Fatalf("%s out of range", p.Name)
			}
		}
	}
}
