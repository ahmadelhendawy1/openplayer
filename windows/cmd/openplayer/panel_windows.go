//go:build windows

package main

// Docked side panels (playlist on the right, menu on the left), drawn as
// HTML in Microsoft Edge WebView2. If WebView2 is unavailable, the uosc
// pop-up menus are used instead.

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
)

//go:embed panel.html
var panelHTML string

const (
	wmAppPanels     = 0x8006 // mpv state changed; push to visible panels
	wmAppPanelCheck = 0x8008 // 5 s after creation: did the page load?
)

type panel struct {
	side    string // "menu" (left) or "playlist" (right)
	hwnd    uintptr
	web     *edge.Chromium
	visible bool
	ready   bool
	failed  bool
	section string
}

var (
	leftPanel  = &panel{side: "menu"}
	rightPanel = &panel{side: "playlist"}

	stateMu   sync.Mutex
	mpvState  = map[string]string{} // property name -> latest value (JSON or plain)
	aspectSel = "-1"
)

func (p *panel) shown() bool { return p != nil && p.visible && p.hwnd != 0 }

func (p *panel) place(x, y, w, h int) {
	if p.hwnd == 0 {
		return
	}
	if !p.shown() || w <= 0 || fsActive {
		pShowWindowP.Call(p.hwnd, 0) // SW_HIDE
		return
	}
	pMoveWindow.Call(p.hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
	pShowWindowP.Call(p.hwnd, 8) // SW_SHOWNA
	if p.side == "idle" {
		pSetWindowPos.Call(p.hwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize) // above the video
	}
	p.showWeb()
}

func panelProc(hwnd, m, wp, lp uintptr) uintptr {
	if m == wmSize {
		for _, p := range []*panel{leftPanel, rightPanel} {
			if p.hwnd == hwnd && p.web != nil {
				p.web.Resize()
			}
		}
	}
	if m == wmDropFiles { // full list of dropped files (WebView2's own drop is off)
		enqueue := false
		for _, p := range []*panel{leftPanel, rightPanel} {
			if p.hwnd == hwnd && p.side == "playlist" {
				enqueue = true
			}
		}
		handleDrop(wp, enqueue)
		return 0
	}
	if m == wmEraseBkgnd && hostBrush != 0 {
		var r rect
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		pFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), hostBrush)
		return 1
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// create builds the panel's WebView2 the first time it is opened. The panel
// window is made visible and sized BEFORE WebView2 is attached to it: a
// WebView2 created inside a hidden 10x10 window can stay blank.
func (p *panel) create() bool {
	if p.web != nil {
		return !p.failed
	}
	if p.failed {
		return false
	}
	inst, _, _ := pGetModuleHandleW.Call(0)
	p.hwnd, _, _ = pCreateWindowExW.Call(wsExNoParent, uintptr(unsafe.Pointer(utf16z(panelClassName))), 0,
		wsChild|wsClipChildren, 0, 0, 10, 10, mainWnd, 0, inst, 0)
	if p.hwnd == 0 {
		logf("panel %s: CreateWindowEx failed", p.side)
		p.failed = true
		return false
	}
	p.visible = true
	layout() // shows and sizes the empty panel window
	logf("panel %s: window created, embedding WebView2", p.side)
	w := edge.NewChromium()
	w.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		logf("panel %s: page loaded", p.side)
	}
	w.DataPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "WebView")
	w.MessageCallback = func(msg string) { p.onMessage(msg) }
	if !w.Embed(p.hwnd) {
		logf("panel %s: WebView2 Embed failed", p.side)
		p.visible = false
		p.failed = true
		layout()
		return false
	}
	logf("panel %s: WebView2 ready", p.side)
	// Dropped files: WebView2 reports each file of a drop as its own file://
	// navigation; onFileDrop collects the whole burst and adds them together.
	// (Turning WebView2's drop off does not hand the drop to this window:
	// Windows shows the "not allowed" cursor instead, so it stays on.)
	w.OnFileNavigation(func(uri string) { p.onFileDrop(uri) })
	if s, err := w.GetSettings(); err == nil {
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutAreDevToolsEnabled(false)
	}
	if c := w.GetController(); c != nil {
		if c2 := c.GetICoreWebView2Controller2(); c2 != nil {
			t := currentTheme()
			v := colorref(t.Canvas)
			_ = c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16)})
		}
	}
	p.web = w
	p.showWeb()
	if p.side == "idle" {
		w.NavigateToString(idleHTML())
	} else {
		w.NavigateToString(panelPage())
	}
	// Safety net: if the page never reports ready, use the pop-up menus.
	side := p.side
	go func() {
		time.Sleep(5 * time.Second)
		pPostMessageW.Call(mainWnd, wmAppPanelCheck, map[string]uintptr{"menu": 0, "playlist": 1, "idle": 2}[side], 0)
	}()
	return true
}

// showWeb sizes the WebView2 to the panel and makes sure it is visible.
func (p *panel) showWeb() {
	if p.web == nil {
		return
	}
	p.web.Resize()
	if c := p.web.GetController(); c != nil {
		_ = c.PutIsVisible(true)
		_ = c.NotifyParentWindowPositionChanged()
	}
	var r rect
	pGetClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&r)))
	logf("panel %s: shown at %dx%d", p.side, r.Right, r.Bottom)
}

// checkReady runs 5 s after creation; a panel that never loaded is replaced
// by the controls skin's pop-up menu.
func (p *panel) checkReady() {
	if p.ready || p.web == nil {
		return
	}
	if p.side == "idle" { // mpv's own idle screen remains underneath
		logf("idle screen did not load; using mpv's idle screen")
		p.failed, p.visible = true, false
		pShowWindowP.Call(p.hwnd, 0)
		layout()
		return
	}
	logf("panel %s: page did not report ready within 5 s; using pop-up menus", p.side)
	p.failed = true
	p.visible = false
	pShowWindowP.Call(p.hwnd, 0)
	layout()
	if p.side == "menu" {
		mpv.Async(mp, "script-binding", "uosc/menu")
	} else {
		mpv.Async(mp, "script-binding", "uosc/playlist")
	}
}

func (p *panel) eval(js string) {
	if p.web != nil && p.ready {
		p.web.Eval(js)
	}
}

// togglePanel opens or closes a side panel ("playlist" or "menu").
func togglePanel(side, section string) {
	p := rightPanel
	if side == "menu" {
		p = leftPanel
	}
	if p.visible && (section == "" || section == p.section) {
		p.visible = false
		layout()
		pSetFocus.Call(mainWnd)
		return
	}
	if !p.create() {
		// WebView2 missing: fall back to the pop-up menus of the controls skin.
		if side == "menu" {
			mpv.Async(mp, "script-binding", "uosc/menu")
		} else {
			mpv.Async(mp, "script-binding", "uosc/playlist")
		}
		return
	}
	p.section = section
	p.visible = true
	layout()
	if p.ready {
		p.eval("OP.mode(" + jsStr(p.side) + "," + jsStr(section) + ")")
		p.pushAll()
	}
	setCheck(idmPanelPlaylist, rightPanel.visible)
	setCheck(idmPanelMenu, leftPanel.visible)
}

func jsStr(s string) string { b, _ := json.Marshal(s); return string(b) }

func themeListJSON() string {
	var list []json.RawMessage
	for _, t := range themes {
		var m map[string]any
		_ = json.Unmarshal([]byte(t.panelJSON()), &m)
		m["name"] = T(t.Name)
		b, _ := json.Marshal(m)
		list = append(list, b)
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func (p *panel) pushAll() {
	if !p.ready {
		return
	}
	p.eval("OP.theme(" + currentTheme().panelJSON() + ")")
	p.eval("OP.themes(" + themeListJSON() + ")")
	p.pushState()
}

func (p *panel) pushState() {
	if !p.ready || !p.visible {
		return
	}
	stateMu.Lock()
	st := make(map[string]string, len(mpvState))
	for k, v := range mpvState {
		st[k] = v
	}
	stateMu.Unlock()
	if p.side == "playlist" {
		if v := st["playlist"]; v != "" {
			p.eval("OP.playlist(" + v + ")")
		}
		pushThumbs(p)
		return
	}
	if v := st["track-list"]; v != "" {
		p.eval("OP.tracks(" + v + ")")
	}
	if v := st["chapter-list"]; v != "" {
		p.eval("OP.chapters(" + v + ")")
	}
	speed, _ := strconv.ParseFloat(st["speed"], 64)
	if speed == 0 {
		speed = 1
	}
	chapter, err := strconv.Atoi(st["chapter"])
	if err != nil {
		chapter = -1
	}
	b, _ := json.Marshal(map[string]any{"speed": speed, "chapter": chapter, "aspect": aspectSel})
	p.eval("OP.state(" + string(b) + ")")
	p.eval("OP.fx(" + fxJSON() + ")")
}

func panelsSetTheme(t theme) {
	for _, p := range []*panel{leftPanel, rightPanel} {
		if p.web != nil {
			if c := p.web.GetController(); c != nil {
				if c2 := c.GetICoreWebView2Controller2(); c2 != nil {
					v := colorref(t.Canvas)
					_ = c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16)})
				}
			}
		}
		p.eval("OP.theme(" + t.panelJSON() + ")")
	}
}

// storeProp records an mpv property for the panels (called on the event thread).
var lastPlaylistLen = -1

func storeProp(name, value string) {
	if name == "playlist" {
		queueThumbs(value)
		if n := strings.Count(value, `"filename"`); n != lastPlaylistLen {
			lastPlaylistLen = n
			logf("playlist: %d item(s)", n)
		}
	}
	stateMu.Lock()
	mpvState[name] = value
	stateMu.Unlock()
	pPostMessageW.Call(mainWnd, wmAppPanels, 0, 0)
}

func panelsRefresh() {
	leftPanel.pushState()
	rightPanel.pushState()
}

// onMessage handles actions from the panel page (runs on the window thread).
func (p *panel) onMessage(raw string) {
	var m struct {
		A string          `json:"a"`
		V json.RawMessage `json:"v"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	var v string
	if err := json.Unmarshal(m.V, &v); err != nil {
		var n json.Number
		if json.Unmarshal(m.V, &n) == nil {
			v = n.String()
		}
	}
	num := func() (string, bool) {
		if _, err := strconv.Atoi(v); err != nil {
			return "", false
		}
		return v, true
	}
	switch m.A {
	case "ready":
		logf("panel %s: page ready", p.side)
		p.ready = true
		if p.side == "playlist" {
			thumbSent = map[string]bool{}
		}
		p.eval("OP.i18n(" + i18nJSON() + ")")
		if p.side == "idle" {
			p.eval("OP.icon(" + jsStr(iconDataURI()) + ")")
			p.eval("OP.theme(" + currentTheme().panelJSON() + ")")
			return
		}
		p.eval("OP.mode(" + jsStr(p.side) + "," + jsStr(p.section) + ")")
		p.pushAll()
	case "close":
		if p.visible {
			togglePanel(p.side, "")
		}
	case "play":
		if n, ok := num(); ok {
			mpv.Async(mp, "playlist-play-index", n)
			mpv.Async(mp, "set", "pause", "no") // clicking an item always starts it
		}
	case "dropnames":
		dropMu.Lock()
		dropNames, dropAt = strings.Split(v, "\n"), time.Now()
		dropMu.Unlock()
	case "remove":
		if n, ok := num(); ok {
			mpv.Async(mp, "playlist-remove", n)
		}
	case "jserror":
		logf("panel %s: page error: %s", p.side, v)
	case "savepl":
		savePlaylist()
	case "loadpl":
		loadPlaylist()
	case "add":
		if files := openDialog(mediaFilter(), true); len(files) > 0 {
			openFiles(files, true)
		}
	case "shuffle":
		mpv.Async(mp, "playlist-shuffle")
	case "clear":
		mpv.Async(mp, "playlist-clear")
	case "sid", "aid", "vid":
		if _, ok := num(); ok || v == "no" {
			mpv.Async(mp, "set", m.A, v)
		}
	case "chapter":
		if n, ok := num(); ok {
			mpv.Async(mp, "set", "chapter", n)
		}
	case "speed":
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.1 && f <= 8 {
			mpv.Async(mp, "set", "speed", strconv.FormatFloat(f, 'f', -1, 64))
		}
	case "aspect":
		if v == "-1" || strings.Count(v, ":") == 1 {
			aspectSel = v
			mpv.Async(mp, "set", "video-aspect-override", v)
			p.pushState()
		}
	case "openfile":
		handleCommand(idmOpen)
	case "openfolder":
		openFolder()
	case "key":
		if v == "playlist" {
			togglePanel("playlist", "")
		}
	case "loadsub":
		handleCommand(idmOpenSub)
	case "eqwin":
		eqWin.show()
	case "norm":
		fx.Norm = !fx.Norm
		applyFX(true)
	case "amp":
		if n, ok := num(); ok {
			a, _ := strconv.Atoi(n)
			fx.Amp = clampAmp(a)
			applyFX(true)
		}
	case "theme":
		for _, t := range themes {
			if t.ID == v {
				applyTheme(t, true)
			}
		}
	}
}

// ---- drag and drop onto a panel -------------------------------------------
//
// WebView2 reports only the first dropped file (as a file:// navigation, which
// we cancel). The page sends the names of all dropped files; files with those
// names next to the first one are added too.

var (
	dropMu    sync.Mutex
	dropNames []string
	dropAt    time.Time
	dropFirst string
	dropMore  []string
	dropSeq   int
	dropSide  string
)

const wmAppDrop = 0x8009

func (p *panel) onFileDrop(uri string) {
	path := uriToPath(uri)
	logf("panel %s: file dropped (%d chars)", p.side, len(path))
	if path == "" {
		return
	}
	// WebView2 reports a multi-file drop as one event per file, all at once:
	// collect them and add them together once the burst is over.
	dropMu.Lock()
	if dropFirst == "" {
		dropFirst = path
	} else if path != dropFirst && !containsStr(dropMore, path) {
		dropMore = append(dropMore, path)
	}
	dropSide = p.side
	dropSeq++
	seq := dropSeq
	dropMu.Unlock()
	go func() {
		time.Sleep(250 * time.Millisecond)
		dropMu.Lock()
		last := seq == dropSeq
		dropMu.Unlock()
		if last {
			pPostMessageW.Call(mainWnd, wmAppDrop, 0, 0)
		}
	}()
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if u.Host != "" { // \\server\share
		p = "//" + u.Host + p
	} else {
		p = strings.TrimPrefix(p, "/")
	}
	return filepath.FromSlash(p)
}

func finishDrop() {
	dropMu.Lock()
	first, more, side, names, at := dropFirst, dropMore, dropSide, dropNames, dropAt
	dropFirst, dropMore, dropNames = "", nil, nil
	dropMu.Unlock()
	if first == "" {
		return
	}
	files := append([]string{first}, more...)
	if len(more) == 0 && time.Since(at) < 3*time.Second { // only one path came through: use the names
		dir, base := filepath.Dir(first), filepath.Base(first)
		for _, n := range names {
			if n == "" || n == base || strings.ContainsAny(n, `\/:`) {
				continue
			}
			if st, err := os.Stat(filepath.Join(dir, n)); err == nil && !st.IsDir() {
				files = append(files, filepath.Join(dir, n))
			}
		}
	}
	logf("drop (fallback): %d file(s)", len(files))
	openFiles(files, side == "playlist")
	pSetFocus.Call(mainWnd)
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
