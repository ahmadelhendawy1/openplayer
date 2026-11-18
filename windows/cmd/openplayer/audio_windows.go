//go:build windows

package main

// Equalizer, normalizer and amplifier: state, menus and the floating widget.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

//go:embed eq.html
var eqHTML string

const (
	idmEqualizer   = 140
	idmEqOn        = 141
	idmNormalize   = 142
	idmPresetFirst = 1500 // + index into menuPresets
	idmAmpFirst    = 1700 // + index into ampSteps
)

var (
	fx          audioFX
	lastAF      = "" // nothing sent yet: an empty chain at start-up keeps mpv.conf's own "af"
	menuPresets []string
	ampSteps    = []int{100, 150, 200, 250, 300, 400, 500}

	eqWin = &webWin{name: "equalizer", title: "Equalizer", w: 660, h: 300, glass: true,
		page: func() string { return eqHTML }}
)

func init() {
	eqWin.onMsg = eqMessage
	eqWin.onReady = pushEQ
	eqWin.position = func(w, h int) (int, int) {
		var r rect
		pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&r)))
		x := int(r.Left+r.Right)/2 - w/2
		y := int(r.Bottom) - h - scalePx(130) // above the controls, like Zoom Player
		if y < int(r.Top) {
			y = int(r.Top)
		}
		return x, y
	}
}

func loadFX() { fx = decodeFX(cfg.get("audio_fx", "")) }

// applyFX sends the filter graph to mpv (only when it changed) and updates
// the menus and the widget. save=true also stores it as the preference.
func applyFX(save bool) {
	if af := fx.filter(); af != lastAF {
		lastAF = af
		mpv.Async(mp, "set", "af", af)
	}
	if save {
		_ = cfg.set("audio_fx", fx.encode())
	}
	refreshAudioChecks()
	pushEQ()
	leftPanel.pushState()
}

func fxJSON() string {
	b, _ := json.Marshal(map[string]any{"eq": fx.EQ, "norm": fx.Norm, "amp": fx.Amp})
	return string(b)
}

func allPresetNames() (builtin, user []string) {
	for _, p := range eqPresets {
		builtin = append(builtin, p.Name)
	}
	for _, p := range userPresets(cfg.all()) {
		user = append(user, p.Name)
	}
	return
}

func pushEQ() {
	if !eqWin.ready {
		return
	}
	b, u := allPresetNames()
	if u == nil {
		u = []string{}
	}
	j, _ := json.Marshal(map[string]any{"eq": fx.EQ, "preset": fx.Preset, "pre": fx.Pre, "bands": fx.Bands,
		"norm": fx.Norm, "amp": fx.Amp, "presets": b, "user": u, "freqs": eqFreqs})
	eqWin.eval("OP.state(" + string(j) + ")")
}

func selectPreset(name string) {
	if p, ok := builtinPreset(name); ok {
		fx.Bands, fx.Pre = p.Bands, p.Pre
	} else {
		found := false
		for _, p := range userPresets(cfg.all()) {
			if p.Name == name {
				fx.Bands, fx.Pre, found = p.Bands, p.Pre, true
			}
		}
		if !found {
			return
		}
	}
	fx.Preset, fx.EQ = name, true
	applyFX(true)
	mpv.Async(mp, "show-text", T("Equalizer")+": "+presetLabel(name), "1500")
}

func presetLabel(n string) string {
	if _, ok := builtinPreset(n); ok {
		return T(n)
	}
	return n
}

func eqMessage(a string, v json.RawMessage) {
	switch a {
	case "band":
		var b struct {
			I int     `json:"i"`
			G float64 `json:"g"`
		}
		if json.Unmarshal(v, &b) != nil || b.I < -1 || b.I > 9 {
			return
		}
		if b.I < 0 {
			fx.Pre = clampDB(b.G)
		} else {
			fx.Bands[b.I] = clampDB(b.G)
			fx.Preset, fx.EQ = presetCustom, true
		}
		if af := fx.filter(); af != lastAF { // live while dragging; saved on "commit"
			lastAF = af
			mpv.Async(mp, "set", "af", af)
		}
	case "commit":
		applyFX(true)
	case "eq":
		fx.EQ = jsonString(v) == "true"
		applyFX(true)
	case "norm":
		fx.Norm = jsonString(v) == "true"
		applyFX(true)
	case "amp":
		n, err := strconv.Atoi(jsonString(v))
		if err == nil {
			fx.Amp = clampAmp(n)
			if af := fx.filter(); af != lastAF {
				lastAF = af
				mpv.Async(mp, "set", "af", af)
			}
		}
	case "preset":
		selectPreset(jsonString(v))
	case "reset":
		fx.Bands, fx.Pre, fx.Preset = [10]float64{}, 0, "Flat"
		applyFX(true)
	case "save":
		name := cleanPresetName(jsonString(v))
		if name == "" || name == presetCustom {
			return
		}
		if _, ok := builtinPreset(name); ok {
			name += " (" + T("mine") + ")"
		}
		_ = cfg.set(userPresetPrefix+name, encodePreset(eqPreset{Pre: fx.Pre, Bands: fx.Bands}))
		fx.Preset, fx.EQ = name, true
		rebuildMenus()
		applyFX(true)
		mpv.Async(mp, "show-text", Tf("Preset saved: %s", name), "2000")
	case "delete":
		name := jsonString(v)
		if _, ok := builtinPreset(name); ok {
			return
		}
		_ = cfg.del(userPresetPrefix + name)
		if fx.Preset == name {
			fx.Preset = presetCustom
		}
		rebuildMenus()
		applyFX(true)
	}
}

// ---- menus ------------------------------------------------------------------

func audioFXMenu(m uintptr) {
	addItem(m, idmEqualizer, "&Equalizer...\tCtrl+E")
	addItem(m, idmEqOn, "Equalizer On")
	pm := newPopup()
	b, u := allPresetNames()
	menuPresets = append(append([]string{}, b...), u...)
	amp := func(n string) string { return strings.ReplaceAll(n, "&", "&&") } // "&" would underline
	for i, n := range b {
		addRawItem(pm, idmPresetFirst+i, amp(T(n)))
	}
	if len(u) > 0 {
		addSep(pm)
		for i, n := range u {
			addRawItem(pm, idmPresetFirst+len(b)+i, amp(n)) // user names are not translated
		}
	}
	addSub(m, pm, "Equalizer &Preset")
	addItem(m, idmNormalize, "&Normalize Audio")
	am := newPopup()
	for i, a := range ampSteps {
		label := fmt.Sprintf("%d%%", a)
		if a == 100 {
			label = "100% (Off)"
		}
		addItem(am, idmAmpFirst+i, label)
	}
	addSub(m, am, "&Amplifier")
}

func refreshAudioChecks() {
	setCheck(idmEqOn, fx.EQ)
	setCheck(idmNormalize, fx.Norm)
	for i, n := range menuPresets {
		setCheck(idmPresetFirst+i, fx.EQ && n == fx.Preset)
	}
	for i, a := range ampSteps {
		setCheck(idmAmpFirst+i, fx.Amp == a)
	}
}

// audioCommand handles the audio menu ids; returns false if not ours.
func audioCommand(id int) bool {
	switch {
	case id == idmEqualizer:
		eqWin.toggle()
	case id == idmEqOn:
		fx.EQ = !fx.EQ
		applyFX(true)
		mpv.Async(mp, "show-text", T(map[bool]string{true: "Equalizer on", false: "Equalizer off"}[fx.EQ]), "1500")
	case id == idmNormalize:
		fx.Norm = !fx.Norm
		applyFX(true)
		mpv.Async(mp, "show-text", T(map[bool]string{true: "Normalize on", false: "Normalize off"}[fx.Norm]), "1500")
	case id >= idmPresetFirst && id < idmPresetFirst+len(menuPresets):
		selectPreset(menuPresets[id-idmPresetFirst])
	case id >= idmAmpFirst && id < idmAmpFirst+len(ampSteps):
		fx.Amp = ampSteps[id-idmAmpFirst]
		applyFX(true)
		mpv.Async(mp, "show-text", Tf("Amplifier: %d%%", fx.Amp), "1500")
	default:
		return false
	}
	return true
}
