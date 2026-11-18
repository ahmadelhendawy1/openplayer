//go:build windows

package main

// Floating windows drawn in WebView2: the equalizer (a borderless glass
// widget, like Zoom Player's) and Tools > Options (a normal window). Both
// are owned by the player window, so they stay above it, also in fullscreen.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
)

const (
	popupClassName = "OpenPlayerPopup"

	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsThickFrame  = 0x00040000
	wsMinimizeBox = 0x00020000
	wsExToolWin   = 0x00000080
	wmNcLButtonDn = 0x00A1
	htCaption     = 2
	wmGetMinMax   = 0x0024

	dwmaCornerPref = 33 // DWMWA_WINDOW_CORNER_PREFERENCE (Windows 11)
)

var (
	pDestroyWindow   = user32.NewProc("DestroyWindow")
	pSetWindowComp   = user32.NewProc("SetWindowCompositionAttribute")
	pIsWindowVisible = user32.NewProc("IsWindowVisible")
	pDwmEnableBlur   = dwmapi.NewProc("DwmEnableBlurBehindWindow")
	pCreateRectRgn   = gdi32.NewProc("CreateRectRgn")
	popupClassReg    bool
	popups           = map[uintptr]*webWin{}
)

type webWin struct {
	name     string
	title    string
	page     func() string
	w, h     int // logical size
	frame    bool
	glass    bool
	hwnd     uintptr
	web      *edge.Chromium
	ready    bool
	onMsg    func(a string, v json.RawMessage)
	onReady  func()
	minW     int
	minH     int
	position func(w, h int) (x, y int) // initial position in screen pixels
}

type minMaxInfo struct {
	Reserved, MaxSize, MaxPos, MinTrack, MaxTrack [2]int32
}

func popupProc(hwnd, m, wp, lp uintptr) uintptr {
	w := popups[hwnd]
	switch m {
	case wmSize:
		if w != nil && w.web != nil {
			w.web.Resize()
		}
	case wmClose:
		if w != nil {
			w.hide()
			return 0
		}
	case wmGetMinMax:
		if w != nil && w.minW > 0 {
			mmi := (*minMaxInfo)(ptrAt(lp))
			mmi.MinTrack = [2]int32{int32(scalePx(w.minW)), int32(scalePx(w.minH))}
		}
	case wmEraseBkgnd:
		if w != nil && w.glass {
			return 1
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

type accentPolicy struct {
	State, Flags, Gradient, Animation uint32
}

type winCompAttrData struct {
	Attrib uint32
	Data   uintptr
	Size   uintptr
}

type dwmBlurBehind struct {
	Flags      uint32
	Enable     int32
	Region     uintptr
	Transition int32
}

// makeGlass lets the desktop and the video behind the window show through,
// blurred (the technique used by Windows Terminal's predecessors and Tauri).
// Where unsupported, the page's own tinted background is what shows.
func makeGlass(hwnd uintptr) {
	rgn, _, _ := pCreateRectRgn.Call(0, 0, ^uintptr(0), ^uintptr(0)) // empty region: per-pixel alpha
	bb := dwmBlurBehind{Flags: 0x1 | 0x2, Enable: 1, Region: rgn}    // DWM_BB_ENABLE | DWM_BB_BLURREGION
	pDwmEnableBlur.Call(hwnd, uintptr(unsafe.Pointer(&bb)))
	ap := accentPolicy{State: 3} // ACCENT_ENABLE_BLURBEHIND
	data := winCompAttrData{Attrib: 19, Data: uintptr(unsafe.Pointer(&ap)), Size: unsafe.Sizeof(ap)}
	if pSetWindowComp.Find() == nil {
		pSetWindowComp.Call(hwnd, uintptr(unsafe.Pointer(&data)))
	}
	round := uint32(2) // DWMWCP_ROUND
	pDwmSetWindowAttr.Call(hwnd, dwmaCornerPref, uintptr(unsafe.Pointer(&round)), 4)
}

func (w *webWin) visible() bool {
	if w.hwnd == 0 {
		return false
	}
	r, _, _ := pIsWindowVisible.Call(w.hwnd)
	return r != 0
}

func (w *webWin) toggle() {
	if w.visible() {
		w.hide()
	} else {
		w.show()
	}
}

func (w *webWin) hide() {
	if w.hwnd != 0 {
		pShowWindowP.Call(w.hwnd, 0)
		pSetFocus.Call(mainWnd)
	}
}

func (w *webWin) show() {
	if w.hwnd != 0 {
		pShowWindowP.Call(w.hwnd, 5) // SW_SHOW
		pSetForegroundWnd.Call(w.hwnd)
		if w.ready && w.onReady != nil {
			w.onReady()
		}
		return
	}
	inst, _, _ := pGetModuleHandleW.Call(0)
	if !popupClassReg {
		wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(popupProc),
			Instance: inst, Cursor: arrowCursor, ClassName: utf16z(popupClassName)}
		icon, _, _ := pLoadIconW.Call(inst, 1)
		wc.Icon, wc.IconSm = icon, icon
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		popupClassReg = true
	}
	pw, ph := scalePx(w.w), scalePx(w.h)
	style := uintptr(wsPopup | wsClipChildren)
	ex := uintptr(wsExToolWin)
	if w.frame {
		style = wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsClipChildren
		ex = 0
		r := rect{0, 0, int32(pw), int32(ph)}
		pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), style, 0)
		pw, ph = int(r.Right-r.Left), int(r.Bottom-r.Top)
	}
	x, y := 100, 100
	if w.position != nil {
		x, y = w.position(pw, ph)
	}
	w.hwnd, _, _ = pCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(utf16z(popupClassName))),
		uintptr(unsafe.Pointer(utf16z(T(w.title)))), style, uintptr(x), uintptr(y), uintptr(pw), uintptr(ph), mainWnd, 0, inst, 0)
	if w.hwnd == 0 {
		logf("%s: CreateWindowEx failed", w.name)
		return
	}
	popups[w.hwnd] = w
	if w.glass {
		makeGlass(w.hwnd)
	}
	applyFrameTheme(w.hwnd, currentTheme())
	pShowWindowP.Call(w.hwnd, 5)
	c := edge.NewChromium()
	c.DataPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "WebView")
	c.MessageCallback = func(raw string) { w.message(raw) }
	if !c.Embed(w.hwnd) {
		logf("%s: WebView2 Embed failed", w.name)
		pDestroyWindow.Call(w.hwnd)
		delete(popups, w.hwnd)
		w.hwnd = 0
		messageInfo("OpenPlayer", T("This window needs Microsoft Edge WebView2, which is not installed."))
		return
	}
	c.OnFileNavigation(func(string) {}) // never navigate away (dropped files)
	c.PutAllowExternalDrop(false)
	if s, err := c.GetSettings(); err == nil {
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutAreDevToolsEnabled(false)
	}
	if ctl := c.GetController(); ctl != nil {
		if c2 := ctl.GetICoreWebView2Controller2(); c2 != nil {
			bg := edge.COREWEBVIEW2_COLOR{A: 255}
			if !w.glass {
				v := colorref(currentTheme().Canvas)
				bg = edge.COREWEBVIEW2_COLOR{A: 255, R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16)}
			} else {
				bg.A = 0 // transparent: the blurred background shows through
			}
			_ = c2.PutDefaultBackgroundColor(bg)
		}
		_ = ctl.PutIsVisible(true)
	}
	w.web = c
	c.Resize()
	c.NavigateToString(w.page())
}

func (w *webWin) eval(js string) {
	if w.web != nil && w.ready {
		w.web.Eval(js)
	}
}

func (w *webWin) message(raw string) {
	var m struct {
		A string          `json:"a"`
		V json.RawMessage `json:"v"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	switch m.A {
	case "ready":
		w.ready = true
		w.eval("OP.i18n(" + i18nJSON() + ")")
		w.eval("OP.theme(" + currentTheme().panelJSON() + ")")
		if w.onReady != nil {
			w.onReady()
		}
		return
	case "drag": // move the borderless window by its header
		pReleaseCapture.Call()
		pPostMessageW.Call(w.hwnd, wmNcLButtonDn, htCaption, 0)
		return
	case "close":
		w.hide()
		return
	}
	if w.onMsg != nil {
		w.onMsg(m.A, m.V)
	}
}

// applyFrameTheme colors a window's title bar like the player's.
func applyFrameTheme(hwnd uintptr, t theme) {
	dark := uint32(0)
	if t.Dark {
		dark = 1
	}
	pDwmSetWindowAttr.Call(hwnd, dwmaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), 4)
	for _, a := range []struct {
		attr uintptr
		v    uint32
	}{{dwmaCaptionColor, colorref(t.Canvas)}, {dwmaTextColor, colorref(t.Text)}, {dwmaBorderColor, colorref(t.Border)}} {
		v := a.v
		pDwmSetWindowAttr.Call(hwnd, a.attr, uintptr(unsafe.Pointer(&v)), 4)
	}
}

// popupsSetTheme / popupsSetLang push changes to open popups.
func popupsSetTheme(t theme) {
	for h, w := range popups {
		applyFrameTheme(h, t)
		w.eval("OP.theme(" + t.panelJSON() + ")")
	}
}

func popupsSetLang() {
	for h, w := range popups {
		pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(utf16z(T(w.title)))))
		w.eval("OP.i18n(" + i18nJSON() + ")")
		if w.onReady != nil {
			w.onReady()
		}
	}
}

// decode helpers for popup messages.
func jsonString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return string(v)
}
