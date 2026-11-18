//go:build windows

package main

import (
	"encoding/json"
	"strconv"
	"unsafe"
)

// theme drives the on-screen controls (uosc), the side panels, the menu bar
// and the Windows 11 title bar. Colors are "rrggbb".
type theme struct {
	ID, Name   string
	Dark       bool
	Canvas     string // window/title/menu bar/panel background
	Surface    string // control bar & menus
	Text       string
	Muted      string
	Accent     string // buttons, progress, selection
	OnAccent   string // text on accent
	Match      string // search match / secondary accent
	Hot        string // menu bar hover
	Border     string
	Opacity    string // uosc opacity overrides ("" = glass default)
	ExtraGroup bool   // listed after a separator
}

const glassOpacity = "timeline=0.45,position=1,chapters=0.7,slider=0.85,slider_gauge=1,controls=0,speed=0.6,menu=0.88,submenu=0.5,tooltip=0.92,curtain=0.75,idle_indicator=0.85,pause_indicator=0.85,playlist_position=0.85,thumbnail=1"
const lightOpacity = "timeline=0.55,position=1,chapters=0.7,slider=0.9,slider_gauge=1,controls=0,speed=0.7,menu=0.94,submenu=0.6,tooltip=0.95,curtain=0.7,idle_indicator=0.9,pause_indicator=0.9,playlist_position=0.9,thumbnail=1"
const solidOpacity = "timeline=1,position=1,chapters=1,slider=1,slider_gauge=1,controls=0,speed=1,menu=1,submenu=1,tooltip=1,curtain=0.9,idle_indicator=1,pause_indicator=1,playlist_position=1,thumbnail=1"

var themes = []theme{
	{ID: "aurora-glass", Name: "Aurora Glass", Dark: false, Canvas: "f6f7fb", Surface: "f8f9ff", Text: "0f172a", Muted: "334155", Accent: "4338ca", OnAccent: "ffffff", Match: "0891b2", Hot: "e0e7ff", Border: "a5b4fc", Opacity: lightOpacity},
	{ID: "black-gold", Name: "Black Gold Aurora", Dark: true, Canvas: "070707", Surface: "111111", Text: "fff5df", Muted: "cfc4ae", Accent: "f5d78e", OnAccent: "1a1206", Match: "be872d", Hot: "1f1b14", Border: "5a4318"},
	{ID: "aurora-night", Name: "Aurora Night", Dark: true, Canvas: "080f1c", Surface: "111827", Text: "f1f5f9", Muted: "cbd5e1", Accent: "a5b4fc", OnAccent: "0b1020", Match: "22d3ee", Hot: "1e2a4a", Border: "3b3b9a"},
	{ID: "honey", Name: "Honey Glass", Dark: false, Canvas: "fff8e7", Surface: "fff4dc", Text: "3a2412", Muted: "69482a", Accent: "b8770f", OnAccent: "2b1a0c", Match: "c96a00", Hot: "f3e2bd", Border: "d9a54a", Opacity: lightOpacity},
	{ID: "burgundy", Name: "Burgundy Glass", Dark: true, Canvas: "200f19", Surface: "351321", Text: "fff1f5", Muted: "e6bbc9", Accent: "e0a0b6", OnAccent: "3a0d1a", Match: "d48ba4", Hot: "4a1a2a", Border: "7a2e45"},
	{ID: "retro-white", Name: "Retro White", Dark: false, Canvas: "f4f1ea", Surface: "fbfaf6", Text: "111111", Muted: "555555", Accent: "111111", OnAccent: "ffffff", Match: "c8102e", Hot: "e4ded1", Border: "222222", Opacity: lightOpacity},
	{ID: "midnight-glass", Name: "Midnight Glass", Dark: true, Canvas: "080f1c", Surface: "111d30", Text: "f1f5f9", Muted: "cbd5e1", Accent: "67e8f9", OnAccent: "06202a", Match: "a5b4fc", Hot: "1e293b", Border: "2a3a55", ExtraGroup: true},
	{ID: "aurora", Name: "Aurora", Dark: true, Canvas: "100d1c", Surface: "211b32", Text: "f5f3ff", Muted: "ddd6fe", Accent: "c4b5fd", OnAccent: "1a1033", Match: "67e8f9", Hot: "302840", Border: "4a3d6b", ExtraGroup: true},
	{ID: "frost", Name: "Frost", Dark: false, Canvas: "f7faff", Surface: "eaf0f8", Text: "0f172a", Muted: "334155", Accent: "1749b5", OnAccent: "ffffff", Match: "075b68", Hot: "dce5f0", Border: "52647a", Opacity: lightOpacity, ExtraGroup: true},
	{ID: "oled-black", Name: "OLED Black", Dark: true, Canvas: "000000", Surface: "101010", Text: "e2e2e2", Muted: "bdbdbd", Accent: "67d4e8", OnAccent: "00191f", Match: "b9a8f0", Hot: "202020", Border: "303030", ExtraGroup: true},
	{ID: "high-contrast", Name: "High Contrast", Dark: true, Canvas: "000000", Surface: "000000", Text: "ffffff", Muted: "ffffff", Accent: "ffff00", OnAccent: "000000", Match: "00ffff", Hot: "333300", Border: "ffffff", Opacity: solidOpacity, ExtraGroup: true},
}

const (
	dwmaBorderColor  = 34
	dwmaCaptionColor = 35
	dwmaTextColor    = 36
	idmThemeFirst    = 900 // + index into themes
	idmMenuBar       = 899
)

// colorref converts "rrggbb" to a Windows COLORREF (0x00BBGGRR).
func colorref(hex string) uint32 {
	v, _ := strconv.ParseUint(hex, 16, 32)
	r, g, b := uint32(v>>16)&0xFF, uint32(v>>8)&0xFF, uint32(v)&0xFF
	return b<<16 | g<<8 | r
}

func (t theme) uoscColor() string {
	return "foreground=" + t.Accent + ",foreground_text=" + t.OnAccent + ",background=" + t.Surface +
		",background_text=" + t.Text + ",curtain=" + t.Canvas + ",success=7ee787,error=ff6b81,match=" + t.Match +
		",heatmap=" + t.Accent + ",window_border=" + t.Canvas
}

func (t theme) uoscOpacity() string {
	if t.Opacity != "" {
		return t.Opacity
	}
	return glassOpacity
}

// panelJSON is the palette the side panels use.
func (t theme) panelJSON() string {
	b, _ := json.Marshal(map[string]any{
		"id": t.ID, "dark": t.Dark, "canvas": "#" + t.Canvas, "surface": "#" + t.Surface, "text": "#" + t.Text,
		"muted": "#" + t.Muted, "accent": "#" + t.Accent, "onAccent": "#" + t.OnAccent, "match": "#" + t.Match,
		"border": "#" + t.Border,
	})
	return string(b)
}

func themeIndex(id string) int {
	for i, t := range themes {
		if t.ID == id {
			return i
		}
	}
	return 0
}

func currentTheme() theme { return themes[themeIndex(cfg.get("theme", themes[0].ID))] }

// applyTheme recolors the controls (live, through uosc's script-opts), the
// side panels, the menu bar and the window frame.
func applyTheme(t theme, save bool) {
	mpv.Async(mp, "change-list", "script-opts", "append", "uosc-color="+t.uoscColor())
	mpv.Async(mp, "change-list", "script-opts", "append", "uosc-opacity="+t.uoscOpacity())
	dark := uint32(0)
	if t.Dark {
		dark = 1
	}
	pDwmSetWindowAttr.Call(mainWnd, dwmaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), 4)
	for _, a := range []struct {
		attr uintptr
		v    uint32
	}{{dwmaCaptionColor, colorref(t.Canvas)}, {dwmaTextColor, colorref(t.Text)}, {dwmaBorderColor, colorref(t.Border)}} {
		v := a.v
		pDwmSetWindowAttr.Call(mainWnd, a.attr, uintptr(unsafe.Pointer(&v)), 4) // Windows 11 only
	}
	setMenuBarColors(colorref(t.Canvas), colorref(t.Hot), colorref(t.Text))
	setHostBackground(colorref(t.Canvas))
	panelsSetTheme(t)
	idleSetTheme(t)
	popupsSetTheme(t)
	glassLast = ""
	updateGlass()
	for i, th := range themes {
		setCheck(idmThemeFirst+i, th.ID == t.ID)
	}
	if save {
		_ = cfg.set("theme", t.ID)
	}
}

func themeMenu() uintptr {
	m := newPopup()
	for i, t := range themes {
		if t.ExtraGroup && (i == 0 || !themes[i-1].ExtraGroup) {
			addSep(m)
		}
		addItem(m, idmThemeFirst+i, t.Name)
	}
	return m
}

// Menu bar: shown by default (themed); View > Show Menu Bar hides it.
func menuBarOn() bool { return cfg.flag("menu_bar", true) }

func applyMenuBar(on bool, save bool) {
	if !fsActive {
		if on {
			pSetMenu.Call(mainWnd, mainMenu)
		} else {
			pSetMenu.Call(mainWnd, 0)
		}
	}
	setCheck(idmMenuBar, on)
	if save {
		_ = cfg.set("menu_bar", map[bool]string{true: "yes", false: "no"}[on])
	}
}
