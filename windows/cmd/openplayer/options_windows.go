//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

//go:embed options.html
var optionsHTML string

var (
	optWin = &webWin{name: "options", title: "Options", w: 920, h: 640, frame: true, minW: 640, minH: 420,
		page: func() string { return optionsHTML }}
	optSectionReq string
)

func init() {
	optWin.onMsg = optMessage
	optWin.onReady = pushOptions
	optWin.position = func(w, h int) (int, int) {
		var r rect
		pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&r)))
		return int(r.Left+r.Right)/2 - w/2, max(int(r.Top+r.Bottom)/2-h/2, 0)
	}
}

func showOptions(section string) {
	optSectionReq = section
	optWin.show()
}

func boolStr(b bool) string { return map[bool]string{true: "yes", false: "no"}[b] }

// optValue is the current value of an option, as the page shows it.
func optValue(o optDef) string {
	kind, name, _ := strings.Cut(o.Key, ":")
	switch kind {
	case "mpv":
		return cfg.get("mpv."+name, o.Def)
	case "app":
		switch name {
		case "lang":
			return uiLang
		case "theme":
			return currentTheme().ID
		case "menu_bar":
			return boolStr(menuBarOn())
		}
		return cfg.get(name, o.Def)
	}
	return ""
}

func pushOptions() {
	if !optWin.ready {
		return
	}
	secs := optionSections()
	for si := range secs {
		for oi := range secs[si].Items {
			o := &secs[si].Items[oi]
			switch o.Key {
			case "app:theme":
				for _, t := range themes {
					o.Choices = append(o.Choices, optChoice{t.ID, t.Name})
				}
			case "mpv:audio-device":
				o.Choices = audioDevices()
			}
			o.Value = optValue(*o)
		}
	}
	b, _ := json.Marshal(map[string]any{"sections": secs, "section": optSectionReq, "version": appVersion})
	optSectionReq = ""
	optWin.eval("OP.data(" + string(b) + ")")
}

// audioDevices lists mpv's audio outputs (observed property audio-device-list).
func audioDevices() []optChoice {
	stateMu.Lock()
	js := mpvState["audio-device-list"]
	stateMu.Unlock()
	var list []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal([]byte(js), &list)
	out := []optChoice{{"auto", "Automatic (Windows default)"}}
	for _, d := range list {
		if d.Name == "auto" || d.Name == "" {
			continue
		}
		label := d.Description
		if label == "" {
			label = d.Name
		}
		out = append(out, optChoice{d.Name, label})
	}
	return out
}

func optMessage(a string, v json.RawMessage) {
	switch a {
	case "set":
		var kv struct{ Key, Value string }
		if json.Unmarshal(v, &kv) == nil {
			setOption(kv.Key, kv.Value)
		}
	case "act":
		optAction(jsonString(v))
	}
}

func setOption(key, val string) {
	o, ok := findOpt(key)
	if !ok || !validOptValue(o, val) {
		logf("options: rejected %s", key)
		return
	}
	kind, name, _ := strings.Cut(key, ":")
	switch kind {
	case "mpv":
		_ = cfg.set("mpv."+name, val)
		if !o.Restart {
			mpv.Async(mp, "set", name, val)
		}
	case "app":
		switch name {
		case "lang":
			if val == "en" || val == "ar" {
				setLanguage(val)
			}
		case "theme":
			for _, t := range themes {
				if t.ID == val {
					applyTheme(t, true)
				}
			}
		case "menu_bar":
			applyMenuBar(val == "yes", true)
		case "ui_scale":
			_ = cfg.set(name, val)
			applyUIScale()
		case "hide_ms":
			_ = cfg.set(name, val)
			applyHideDelay()
		case "smooth":
			_ = cfg.set(name, val)
			applySmooth(true)
		case "auto_update":
			_ = cfg.set(name, val)
			setCheck(idmAutoUpdate, val == "yes")
		default:
			_ = cfg.set(name, val)
		}
	}
}

// applyStartupOptions runs before mpv starts: saved engine options override
// the built-in defaults.
func applyStartupOptions() {
	for k, v := range cfg.all() {
		if name, ok := strings.CutPrefix(k, "mpv."); ok {
			if o, known := findOpt("mpv:" + name); known && validOptValue(o, v) {
				_ = mpv.Option(mp, name, v)
			}
		}
	}
	applySmooth(false)
}

func applyUIScale() {
	s := cfg.get("ui_scale", "1")
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		mpv.Async(mp, "change-list", "script-opts", "append", "uosc-scale="+s)
	}
}

func applyHideDelay() {
	if n, err := strconv.Atoi(cfg.get("hide_ms", "1500")); err == nil && n >= 500 && n <= 10000 {
		cursorHideMs = uintptr(n)
	}
}

// applySmooth turns frame interpolation on or off (two engine options).
func applySmooth(live bool) {
	on := cfg.flag("smooth", false)
	set := func(k, v string) {
		if live {
			mpv.Async(mp, "set", k, v)
		} else {
			_ = mpv.Option(mp, k, v)
		}
	}
	if on {
		set("video-sync", "display-resample")
		set("interpolation", "yes")
		set("tscale", "oversample")
	} else if live {
		set("interpolation", "no")
		set("video-sync", cfg.get("mpv.video-sync", "audio"))
	}
}

func optAction(name string) {
	switch name {
	case "eq":
		eqWin.show()
	case "default":
		makeDefaultCommand()
	case "update":
		startUpdateCheck(true)
	case "folder":
		openPath(filepath.Join(appRoot, "config"))
	case "mpvconf":
		p := filepath.Join(appRoot, "config", "mpv.conf")
		if _, err := os.Stat(p); err != nil {
			_ = os.WriteFile(p, []byte("# OpenPlayer engine options (mpv.conf)\r\n# One option per line, for example:\r\n#   deband=yes\r\n#   sub-scale=1.2\r\n# Restart OpenPlayer after saving. Reference: https://mpv.io/manual/stable/#options\r\n"), 0o644)
		}
		pShellExecuteW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z("open"))), uintptr(unsafe.Pointer(utf16z("notepad.exe"))),
			uintptr(unsafe.Pointer(utf16z(`"`+p+`"`))), 0, 1)
	case "reset":
		r, _, _ := pMessageBoxW.Call(optWin.hwnd, uintptr(unsafe.Pointer(utf16z(T("Reset all options to their defaults?\n\nYour theme, language, equalizer presets and playlist are kept.")))),
			uintptr(unsafe.Pointer(utf16z("OpenPlayer"))), 0x4|0x20)
		if r != 6 {
			return
		}
		for k := range cfg.all() {
			if name, ok := strings.CutPrefix(k, "mpv."); ok {
				_ = cfg.del(k)
				if o, known := findOpt("mpv:" + name); known && !o.Restart {
					mpv.Async(mp, "set", name, o.Def)
				}
			}
		}
		for _, k := range []string{"ui_scale", "hide_ms", "smooth", "single_instance", "resume_rewind", "restore_session"} {
			_ = cfg.del(k)
		}
		applyUIScale()
		applyHideDelay()
		applySmooth(true)
		pushOptions()
	}
}

func openPath(p string) {
	pShellExecuteW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z("open"))), uintptr(unsafe.Pointer(utf16z(p))), 0, 0, 1)
}
