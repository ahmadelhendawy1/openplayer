//go:build windows

// OpenPlayer: a native Win32 window that embeds libmpv. mpv renders video and
// subtitles; the uosc skin draws the on-screen controls; the native menu bar,
// right-click menu, fullscreen, drag-and-drop, Open dialogs, single-instance
// handling and component updates live in this program.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const appVersion = "0.9.9"

// Set at build time: -ldflags "-X main.pubKey=<base64 ed25519 public key>"
var pubKey string

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pSendMessageW     = user32.NewProc("SendMessageW")
	pFindWindowW      = user32.NewProc("FindWindowW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pIsIconic         = user32.NewProc("IsIconic")
	pLoadCursorW      = user32.NewProc("LoadCursorW")
	pMessageBoxW      = user32.NewProc("MessageBoxW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pGetProcAddress   = kernel32.NewProc("GetProcAddress")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
	pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmEraseBkgnd = 0x0014
	wmAppFSExit  = 0x8003

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsClipChildren     = 0x02000000
	cwUseDefault       = 0x80000000

	dwmaUseImmersiveDarkMode = 20
	dwmaSystemBackdropType   = 38 // Windows 11 22H2+; value 2 = Mica

	windowClass = "OpenPlayerWindow"
	copyPlay    = 1 // WM_COPYDATA kinds sent by a second instance
	copyEnqueue = 2
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
}

type copyDataStruct struct {
	Data  uintptr
	Bytes uint32
	Ptr   uintptr
}

const wmAppPanelReq = 0x8007

var panelReq atomic.Pointer[[2]string]

// Program-wide state, set in main before the message loop starts.
var (
	mpv         *mpvLib
	mp          mpvHandle
	mainWnd     uintptr
	appRoot     string
	cfg         *settings
	arrowCursor uintptr
)

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	if r, ok := handleMenuBarMsg(hwnd, m, wp, lp); ok {
		return r
	}
	if r, ok := handleInput(hwnd, m, wp, lp); ok {
		return r
	}
	switch m {
	case wmEraseBkgnd:
		return 1 // mpv paints everything
	case wmCommand:
		handleCommand(int(wp & 0xFFFF))
		return 0
	case wmDropFiles:
		handleDrop(wp, false)
		return 0
	case wmCopyData:
		receiveFiles(wp, lp)
		return 1
	case wmAppFS:
		setFullscreen(wp == 1)
		return 0
	case wmAppFSExit:
		if fsActive {
			toggleFullscreen()
		}
		return 0
	case wmAppTitle:
		applyTitle()
		return 0
	case wmAppMenu:
		showContextMenu()
		return 0
	case wmAppNotice:
		showNotice()
		return 0
	case wmAppPanelReq:
		if r := panelReq.Load(); r != nil {
			togglePanel(r[0], r[1])
		}
		return 0
	case wmAppPanelCheck:
		switch wp {
		case 0:
			leftPanel.checkReady()
		case 1:
			rightPanel.checkReady()
		default:
			idleView.checkReady()
		}
		return 0
	case wmAppAskDefault:
		askDefault()
		return 0
	case wmAppDrop:
		finishDrop()
		return 0
	case wmAppNewVersion:
		showNewVersion()
		return 0
	case wmAppUpdProg:
		onUpdateProgress()
		return 0
	case wmAppUpdDone:
		onUpdateDone(wp == 1)
		return 0
	case wmAppThumb:
		sendReadyThumbs()
		return 0
	case wmAppVolume:
		volumeButton()
		return 0
	case wmAppPanels:
		panelsRefresh()
		pauseChanged()
		updateGlass()
		updateIdle()
		return 0
	case wmSize:
		layout()
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func messageBox(title, text string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16z(text))), uintptr(unsafe.Pointer(utf16z(title))), 0x10) // MB_ICONERROR
}

func fatal(err error) {
	messageBox("OpenPlayer", err.Error())
	os.Exit(1)
}

// useDarkMenus makes popup and right-click menus follow dark mode. Windows has
// no public API for this; uxtheme ordinal 135 (SetPreferredAppMode) and 136
// (FlushMenuThemes) are what Explorer and Notepad use. Missing = no-op.
func useDarkMenus() {
	h, err := syscall.LoadLibrary("uxtheme.dll")
	if err != nil {
		return
	}
	if p, _, _ := pGetProcAddress.Call(uintptr(h), 135); p != 0 {
		syscall.SyscallN(p, 2) // ForceDark
	}
	if p, _, _ := pGetProcAddress.Call(uintptr(h), 136); p != 0 {
		syscall.SyscallN(p)
	}
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}

// parseArgs splits flags from file arguments.
func parseArgs(args []string) (files []string, enqueue bool) {
	for _, a := range args {
		switch a {
		case "--enqueue":
			enqueue = true
		default:
			if !strings.HasPrefix(a, "--") {
				files = append(files, a)
			}
		}
	}
	return
}

// lastOpen is when files last arrived; Explorer starts one process per
// selected file, so files arriving close together are appended, not replaced.
var lastOpen time.Time

// forwardToRunning hands files to an already open OpenPlayer window, so
// "Open with" and "Add to playlist" reuse one player. Returns true if sent.
func forwardToRunning(files []string, enqueue bool) bool {
	find := func() uintptr {
		w, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16z(windowClass))), 0)
		return w
	}
	w := find()
	if w == 0 {
		// Several processes may start at once (multi-select in Explorer). The
		// first one to create the mutex becomes the player; the others wait
		// for its window and forward to it.
		_, _, e := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(utf16z("Local\\OpenPlayer.SingleInstance"))))
		if e != syscall.Errno(183) { // not ERROR_ALREADY_EXISTS: we are first
			return false
		}
		for i := 0; i < 100 && w == 0; i++ {
			time.Sleep(50 * time.Millisecond)
			w = find()
		}
		if w == 0 {
			return false
		}
	}
	if len(files) > 0 {
		payload := utf16.Encode([]rune(strings.Join(files, "\x00") + "\x00"))
		kind := uintptr(copyPlay)
		if enqueue {
			kind = copyEnqueue
		}
		cds := copyDataStruct{Data: kind, Bytes: uint32(len(payload) * 2), Ptr: uintptr(unsafe.Pointer(&payload[0]))}
		pSendMessageW.Call(w, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)))
	}
	if iconic, _, _ := pIsIconic.Call(w); iconic != 0 {
		pShowWindow.Call(w, 9) // SW_RESTORE
	}
	pSetForegroundWnd.Call(w)
	return true
}

func receiveFiles(_, lp uintptr) {
	cds := (*copyDataStruct)(ptrAt(lp))
	if cds.Ptr == 0 || cds.Bytes < 2 || cds.Bytes > 1<<22 {
		return
	}
	src := unsafe.Slice((*uint16)(ptrAt(cds.Ptr)), cds.Bytes/2)
	buf := append([]uint16(nil), src...) // copy before the sender frees it
	buf = append(buf, 0)
	var files []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				files = append(files, string(utf16.Decode(buf[start:i])))
			}
			start = i + 1
		}
	}
	if len(files) > 0 {
		openFiles(files, cds.Data == copyEnqueue || time.Since(lastOpen) < 2*time.Second)
		lastOpen = time.Now()
	}
}

func main() {
	runtime.LockOSThread() // the window and its message loop live on this thread
	files, enqueue := parseArgs(os.Args[1:])
	appRoot = exeDir()
	cfgDir := filepath.Join(appRoot, "config")
	cfg = loadSettings(filepath.Join(cfgDir, "openplayer.conf"))
	if cfg.flag("single_instance", true) && forwardToRunning(files, enqueue) {
		return
	}

	openLog()
	_ = os.MkdirAll(cfgDir, 0o755)
	initLanguage()
	loadFX()
	applyStagedUpdate() // before the engine DLL is loaded

	engine := filepath.Join(appRoot, "engine")
	dll := os.Getenv("OPENPLAYER_LIBMPV") // development override
	if dll == "" {
		dll = filepath.Join(engine, "libmpv-2.dll")
	}
	if _, err := os.Stat(dll); err != nil {
		fatal(fmt.Errorf("The playback engine is not installed.\n\nExpected: %s\n\nPlease reinstall OpenPlayer.", dll))
	}
	var err error
	if mpv, err = loadMPV(dll); err != nil {
		fatal(err)
	}

	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	arrowCursor = cursor
	icon, _, _ := pLoadIconW.Call(inst, 1) // icon resource #1 (openplayer.ico)
	class := utf16z(windowClass)
	wc := wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(wndProc),
		Instance: inst, Icon: icon, IconSm: icon, Cursor: cursor, ClassName: class,
	}
	if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fatal(fmt.Errorf("RegisterClassEx: %v", e))
	}

	useDarkMenus()
	registerChildClasses(inst)
	buildMenus()
	style := uintptr(wsOverlappedWindow | wsVisible | wsClipChildren)
	r := rect{0, 0, 1280, 720} // client size
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), style, 1)
	hwnd, _, e := pCreateWindowExW.Call(0x10, // WS_EX_ACCEPTFILES
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16z("OpenPlayer"))),
		style, cwUseDefault, cwUseDefault, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0, mainMenu, inst, 0)
	if hwnd == 0 {
		fatal(fmt.Errorf("CreateWindowEx: %v", e))
	}
	mainWnd = hwnd
	pDragAcceptFiles.Call(hwnd, 1)
	createHost(inst)
	layout()
	// Mica backdrop (ignored where unsupported); title colors come from the theme.
	mica := uint32(2)
	pDwmSetWindowAttr.Call(hwnd, dwmaSystemBackdropType, uintptr(unsafe.Pointer(&mica)), 4)

	if mp, err = mpv.New(); err != nil {
		fatal(err)
	}
	skin := filepath.Join(engine, "skin")
	required := [][2]string{
		{"wid", strconv.FormatUint(uint64(hostWnd), 10)}, // video area; panels sit beside it
		// libmpv starts with config=no, which ignores the config folder entirely
		// (input.conf, script-opts, fonts, resume data). Turn it on, isolated
		// to OpenPlayer's own folder.
		{"config", "yes"},
		{"config-dir", cfgDir},
	}
	optional := [][2]string{
		{"scripts", filepath.Join(skin, "uosc")}, // on-screen controls
		{"osd-fonts-dir", filepath.Join(skin, "fonts")},
		{"ytdl", "no"}, {"terminal", "no"}, {"load-scripts", "yes"},
		{"osc", "no"}, {"osd-bar", "no"}, {"border", "no"}, // uosc replaces the stock UI
		{"input-default-bindings", "yes"}, {"input-vo-keyboard", "yes"},
		// mpv would turn clicks into "drag the window" in windowed mode, which
		// does nothing for an embedded window and swallows every button click.
		{"window-dragging", "no"},
		{"vo", "gpu-next,gpu"}, {"hwdec", "auto-safe"},
		{"keep-open", "yes"}, {"force-window", "yes"}, {"idle", "yes"},
		{"save-position-on-quit", "yes"}, {"volume-max", "130"},
		{"sub-auto", "fuzzy"}, {"audio-file-auto", "fuzzy"}, {"audio-display", "embedded-first"},
		{"sub-font", "Segoe UI"}, {"screenshot-directory", "~~desktop/"},
		{"cursor-autohide", "1500"},
		{"glsl-shaders", filepath.Join(cfgDir, "shaders", "glass.glsl")}, // glass control plate
		{"script-opts", uoscStartOpts()},                                 // language, translated tooltips, control size
	}
	for _, o := range required {
		if err := mpv.Option(mp, o[0], o[1]); err != nil {
			fatal(err)
		}
	}
	for _, o := range optional {
		_ = mpv.Option(mp, o[0], o[1]) // a build lacking one of these still plays
	}
	applyStartupOptions() // Tools > Options choices override the defaults above
	if err := mpv.Init(mp); err != nil {
		fatal(err)
	}
	applyTheme(currentTheme(), false)
	applyMenuBar(menuBarOn(), false)
	applyHideDelay()
	applyFX(false)
	refreshAllChecks()
	_ = mpv.Observe(mp, "fullscreen", fmtFlag)
	_ = mpv.Observe(mp, "media-title", fmtString)
	for _, prop := range []string{"playlist", "track-list", "chapter-list", "chapter", "speed", "idle-active", "osd-dimensions", "pause", "audio-device-list", "eof-reached"} {
		_ = mpv.Observe(mp, prop, fmtString) // node properties arrive as JSON text
	}

	if len(files) > 0 {
		openFiles(files, false)
		lastOpen = time.Now()
	} else {
		restoreSession()
	}
	noteUpdated()
	maybeAutoUpdate()
	maybeCheckAppVersion()
	scheduleDefaultQuestion()

	// Event loop: fullscreen sync, title, shortcuts from input.conf, shutdown.
	var closing atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !closing.Load() {
			ev := mpv.Wait(mp, 0.25)
			switch ev.ID {
			case evShutdown:
				pPostMessageW.Call(hwnd, wmClose, 0, 0)
				return
			case evPropertyChange:
				name, _, val := propertyChange(ev.Data)
				switch name {
				case "fullscreen":
					if on, ok := propFlag(val); ok {
						v := uintptr(0)
						if on {
							v = 1
						}
						pPostMessageW.Call(hwnd, wmAppFS, v, 0)
					}
				case "media-title":
					queueTitle(propString(val))
				case "playlist", "track-list", "chapter-list", "chapter", "speed", "idle-active", "osd-dimensions", "pause", "audio-device-list", "eof-reached":
					storeProp(name, propString(val))
					if name == "eof-reached" {
						onEOFReached(propString(val))
					}
				}
			case 8: // MPV_EVENT_FILE_LOADED
				onFileLoaded()
			case 7: // MPV_EVENT_END_FILE: log files that fail to play
				if ev.Data != 0 {
					d := ptrAt(ev.Data) // mpv_event_end_file: int reason; int error; ...
					if reason, code := *(*int32)(d), *(*int32)(unsafe.Add(d, 4)); reason == 4 {
						logf("file failed to play: %v", mpv.err(uintptr(int64(code))))
					}
				}
			case evClientMessage:
				args := append(clientMessage(ev.Data), "", "", "")
				switch args[0] {
				case "openplayer-panel":
					logf("panel request: %q %q", args[1], args[2])
					panelReq.Store(&[2]string{args[1], args[2]})
					pPostMessageW.Call(hwnd, wmAppPanelReq, 0, 0)
				case "openplayer-open":
					pPostMessageW.Call(hwnd, wmCommand, idmOpen, 0)
				case "openplayer-click":
					videoClick()
				case "openplayer-fs":
					pPostMessageW.Call(hwnd, wmCommand, idmFullscreen, 0)
				case "openplayer-fs-exit":
					pPostMessageW.Call(hwnd, wmAppFSExit, 0, 0)
				case "openplayer-savepl":
					pPostMessageW.Call(hwnd, wmCommand, idmSavePlaylist, 0)
				case "openplayer-loadpl":
					pPostMessageW.Call(hwnd, wmCommand, idmLoadPlaylist, 0)
				case "openplayer-eq":
					pPostMessageW.Call(hwnd, wmCommand, idmEqualizer, 0)
				case "openplayer-options":
					pPostMessageW.Call(hwnd, wmCommand, idmOptions, 0)
				case "openplayer-volume":
					pPostMessageW.Call(hwnd, wmAppVolume, 0, 0)
				case "openplayer-menu":
					pPostMessageW.Call(hwnd, wmAppMenu, 0, 0)
				}
			}
		}
	}()

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	saveSession()
	closing.Store(true)
	<-done
	mpv.Destroy(mp) // also saves the position of the playing file (save-position-on-quit)
}
