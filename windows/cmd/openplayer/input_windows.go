//go:build windows

package main

// Input forwarding. When libmpv is embedded with --wid on Windows, mpv
// disables its own video window (w32_common.c: EnableWindow(window, 0)), so
// all mouse and keyboard input arrives at OUR window. We forward it to mpv
// with the `mouse`, `keydown`, `keyup` and `keypress` commands, which is what
// the on-screen controls (uosc) and the key bindings need.

import (
	"strconv"
	"strings"
	"unsafe"
)

const (
	wmSetCursor   = 0x0020
	wmKeyDown     = 0x0100
	wmChar        = 0x0102
	wmTimer       = 0x0113
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmMouseWheel  = 0x020A
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	wmMouseHWheel = 0x020E
	wmMouseLeave  = 0x02A3

	htClient    = 1
	tmeLeave    = 0x2
	cursorTimer = 1
)

// cursorHideMs: fullscreen controls and cursor hide after this (Options).
// While paused they stay a little longer (pausedHideMs) and then hide too.
var cursorHideMs uintptr = 1500

const pausedHideMs = 3000

var (
	pSetCapture     = user32.NewProc("SetCapture")
	pReleaseCapture = user32.NewProc("ReleaseCapture")
	pTrackMouse     = user32.NewProc("TrackMouseEvent")
	pGetKeyState    = user32.NewProc("GetKeyState")
	pSetCursor      = user32.NewProc("SetCursor")
	pSetTimer       = user32.NewProc("SetTimer")
	pKillTimer      = user32.NewProc("KillTimer")

	mouseInside  bool
	suppressChar bool
	cursorHidden bool
	// uiAway: we told mpv the mouse left so uosc hides its controls; the
	// next real mouse move brings them back.
	uiAway bool
	lastMouse    = [2]int32{-1, -1}
)

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Track     uintptr
	HoverTime uint32
}

var vkNames = map[uintptr]string{
	0x08: "BS", 0x09: "TAB", 0x0D: "ENTER", 0x1B: "ESC", 0x20: "SPACE",
	0x21: "PGUP", 0x22: "PGDWN", 0x23: "END", 0x24: "HOME",
	0x25: "LEFT", 0x26: "UP", 0x27: "RIGHT", 0x28: "DOWN", 0x2D: "INS", 0x2E: "DEL",
	0x70: "F1", 0x71: "F2", 0x72: "F3", 0x73: "F4", 0x74: "F5", 0x75: "F6",
	0x76: "F7", 0x77: "F8", 0x78: "F9", 0x79: "F10", 0x7A: "F11", 0x7B: "F12",
	0xAD: "MUTE", 0xAE: "VOLUME_DOWN", 0xAF: "VOLUME_UP",
	0xB0: "NEXT", 0xB1: "PREV", 0xB2: "STOP", 0xB3: "PLAYPAUSE",
}

func keyDown(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return int16(r) < 0
}

func mods(withShift bool) string {
	m := ""
	if keyDown(0x11) {
		m += "ctrl+"
	}
	if keyDown(0x12) {
		m += "alt+"
	}
	if withShift && keyDown(0x10) {
		m += "shift+"
	}
	return m
}

func lparamXY(lp uintptr) (int32, int32) {
	return int32(int16(lp & 0xFFFF)), int32(int16((lp >> 16) & 0xFFFF))
}

func sendMouse(x, y int32) {
	mpv.Async(mp, "mouse", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
}

func showCursor() {
	if cursorHidden {
		cursorHidden = false
		pSetCursor.Call(arrowCursor)
		updateGlass() // controls come back in fullscreen
	}
	restartHideTimer()
}

// restartHideTimer starts the fullscreen auto-hide countdown again.
func restartHideTimer() {
	pKillTimer.Call(mainWnd, cursorTimer)
	if fsActive {
		ms := cursorHideMs
		stateMu.Lock()
		paused := mpvState["pause"] == "yes"
		stateMu.Unlock()
		if paused {
			ms = max(ms, pausedHideMs)
		}
		pSetTimer.Call(mainWnd, cursorTimer, ms, 0)
	}
}

// pauseChanged runs on the window thread when mpv's pause state changes.
var lastPaused string

func pauseChanged() {
	stateMu.Lock()
	p := mpvState["pause"]
	stateMu.Unlock()
	if p != lastPaused {
		lastPaused = p
		restartHideTimer()
	}
}

// handleInput forwards input to mpv. Returns handled=true if consumed.
func handleInput(hwnd, m, wp, lp uintptr) (uintptr, bool) {
	if mp == 0 {
		return 0, false
	}
	switch m {
	case wmMouseMove:
		x, y := lparamXY(lp)
		if !mouseInside {
			mouseInside = true
			tme := trackMouseEvent{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			pTrackMouse.Call(uintptr(unsafe.Pointer(&tme)))
			mpv.Async(mp, "keypress", "MOUSE_ENTER")
		}
		if x != lastMouse[0] || y != lastMouse[1] { // Windows sends spurious repeats
			lastMouse = [2]int32{x, y}
			if uiAway {
				uiAway = false
				mpv.Async(mp, "keypress", "MOUSE_ENTER")
			}
			sendMouse(x, y)
			showCursor()
			if fsActive {
				updateGlass() // glass follows the controls near the bottom edge
			}
		}
		return 0, true
	case wmMouseLeave:
		mouseInside = false
		lastMouse = [2]int32{-1, -1}
		mpv.Async(mp, "keypress", "MOUSE_LEAVE")
		updateGlass()
		return 0, true
	case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
		x, y := lparamXY(lp)
		sendMouse(x, y)
		mpv.Async(mp, "keydown", mods(true)+buttonName(m, wp))
		pSetCapture.Call(hwnd)
		showCursor()
		if m == wmXButtonDown {
			return 1, true
		}
		return 0, true
	case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
		x, y := lparamXY(lp)
		sendMouse(x, y)
		mpv.Async(mp, "keyup", mods(true)+buttonName(m, wp))
		pReleaseCapture.Call()
		if m == wmXButtonUp {
			return 1, true
		}
		return 0, true
	case wmMouseWheel, wmMouseHWheel:
		delta := int16((wp >> 16) & 0xFFFF)
		name := "WHEEL_UP"
		switch {
		case m == wmMouseWheel && delta < 0:
			name = "WHEEL_DOWN"
		case m == wmMouseHWheel && delta > 0:
			name = "WHEEL_RIGHT"
		case m == wmMouseHWheel:
			name = "WHEEL_LEFT"
		}
		mpv.Async(mp, "keypress", mods(true)+name)
		return 0, true
	case wmKeyDown:
		suppressChar = false
		if n, ok := vkNames[wp]; ok {
			mpv.Async(mp, "keypress", mods(true)+n)
			suppressChar = true // ENTER, ESC, SPACE... also produce WM_CHAR
			return 0, true
		}
		ctrl := keyDown(0x11)
		if ctrl && wp == 0xBC { // Ctrl+, (Options): no WM_CHAR is sent for it
			mpv.Async(mp, "keypress", mods(false)+",")
			suppressChar = true
			return 0, true
		}
		switch {
		case wp >= 0x41 && wp <= 0x5A: // letters: use the key, not the layout's character
			ch := strings.ToLower(string(rune(wp)))
			if keyDown(0x10) {
				ch = strings.ToUpper(ch)
			}
			mpv.Async(mp, "keypress", mods(false)+ch)
			suppressChar = true
			return 0, true
		case wp >= 0x30 && wp <= 0x39 && (ctrl || !keyDown(0x10)):
			mpv.Async(mp, "keypress", mods(false)+string(rune(wp)))
			suppressChar = true
			return 0, true
		}
		return 0, false // punctuation and symbols arrive as WM_CHAR
	case wmChar:
		if suppressChar {
			suppressChar = false
			return 0, true
		}
		if wp > 0x20 && wp < 0x7F {
			mpv.Async(mp, "keypress", mods(false)+string(rune(wp)))
			return 0, true
		}
		return 0, false
	case wmSetCursor:
		if lp&0xFFFF == htClient && cursorHidden {
			pSetCursor.Call(0)
			return 1, true
		}
		return 0, false
	case wmTimer:
		if wp == cursorTimer {
			pKillTimer.Call(hwnd, cursorTimer)
			if fsActive {
				cursorHidden = true
				pSetCursor.Call(0)
				if mouseInside && !uiAway {
					uiAway = true // uosc hides its controls even if the mouse rests on them
					mpv.Async(mp, "keypress", "MOUSE_LEAVE")
				}
				updateGlass() // controls hide in fullscreen
			}
			return 0, true
		}
	}
	return 0, false
}

func buttonName(m, wp uintptr) string {
	switch m {
	case wmRButtonDown, wmRButtonUp:
		return "MBTN_RIGHT"
	case wmMButtonDown, wmMButtonUp:
		return "MBTN_MID"
	case wmXButtonDown, wmXButtonUp:
		if (wp>>16)&0xFFFF == 1 {
			return "MBTN_BACK"
		}
		return "MBTN_FORWARD"
	}
	return "MBTN_LEFT"
}
