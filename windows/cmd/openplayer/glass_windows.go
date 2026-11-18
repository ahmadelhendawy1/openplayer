//go:build windows

package main

// Floating glass control plate. The blur is done by config\shaders\glass.glsl
// inside mpv's renderer (gpu-next), on the real video pixels under the
// controls; uosc then draws the controls sharp on top. This file computes the
// plate geometry and tint and passes them to the shader as parameters.
//
// The shader runs at mpv's OUTPUT stage, whose coordinates cover only the
// video rectangle (not letterbox bars), so the window-space plate is
// converted using mpv's osd-dimensions margins.

import (
	"encoding/json"
	"fmt"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

var glassLast string

type osdDims struct {
	W, H           float64
	ML, MR, MT, MB float64
}

func parseOsdDims(js string) (osdDims, bool) {
	var m map[string]float64
	if json.Unmarshal([]byte(js), &m) != nil {
		return osdDims{}, false
	}
	d := osdDims{W: m["w"], H: m["h"], ML: m["ml"], MR: m["mr"], MT: m["mt"], MB: m["mb"]}
	return d, d.W > 0 && d.H > 0
}

// transparencyOff reports Windows' "Transparency effects" setting.
func transparencyOff() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("EnableTransparency")
	return err == nil && v == 0
}

func hexRGB(h string) (float64, float64, float64) {
	v, _ := strconv.ParseUint(h, 16, 32)
	return float64(v>>16&0xFF) / 255, float64(v>>8&0xFF) / 255, float64(v&0xFF) / 255
}

// glassPlate returns the plate in window pixels (uosc's control area).
func glassPlate(w, h, scale float64) (x0, y0, x1, y1, r float64) {
	// timeline (30) + controls (40) + margins and spacing, in uosc units.
	return 8 * scale, h - 106*scale, w - 8*scale, h - 4*scale, 16 * scale
}

func updateGlass() {
	if mp == 0 {
		return
	}
	stateMu.Lock()
	dimsJS, idle := mpvState["osd-dimensions"], mpvState["idle-active"] == "yes"
	stateMu.Unlock()
	d, ok := parseOsdDims(dimsJS)
	scale := float64(scalePx(1000)) / 1000
	if fsActive {
		scale *= 1.25 // uosc scale_fullscreen
	}
	// In a window the controls are always shown. In fullscreen uosc reveals
	// them only when the mouse comes near the bottom (also while paused), so the
	// glass follows the same rule instead of lingering as an empty bar.
	nearBottom := false
	if ok && mouseInside && !cursorHidden {
		_, plateTop, _, _, _ := glassPlate(d.W, d.H, scale)
		nearBottom = float64(lastMouse[1]) >= plateTop-60*scale
	}
	on := ok && !idle && (!fsActive || nearBottom) // paused too: hides with the controls
	opts := "glass_on=0"
	if on {
		px0, py0, px1, py1, r := glassPlate(d.W, d.H, scale)
		// Window space -> video (OUTPUT) space, clipped to the video rectangle.
		vw, vh := d.W-d.ML-d.MR, d.H-d.MT-d.MB
		x0, x1 := max(px0-d.ML, 0), min(px1-d.ML, vw)
		y0, y1 := max(py0-d.MT, 0), min(py1-d.MT, vh)
		if x1-x0 > 2*r && y1-y0 > 8 {
			t := currentTheme()
			tr, tg, tb := hexRGB(t.Surface)
			ta, hl := 0.38, 0.20 // light tint: the blurred video stays clearly visible
			if !t.Dark {
				ta, hl = 0.45, 0.45
			}
			if t.ID == "high-contrast" || transparencyOff() {
				ta = 1.0 // opaque fallback
			}
			opts = fmt.Sprintf("glass_on=1,glass_x0=%.1f,glass_y0=%.1f,glass_x1=%.1f,glass_y1=%.1f,glass_r=%.1f,glass_step=%.2f,glass_tr=%.3f,glass_tg=%.3f,glass_tb=%.3f,glass_ta=%.2f,glass_hl=%.2f",
				x0, y0, x1, y1, r, 2.5*scale, tr, tg, tb, ta, hl)
		}
	}
	if opts != glassLast {
		glassLast = opts
		mpv.Async(mp, "set", "glsl-shader-opts", opts)
	}
}
