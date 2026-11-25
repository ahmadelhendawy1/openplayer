//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"openplayer/internal/mediaext"
)

var (
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pCreateMenu        = user32.NewProc("CreateMenu")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenuW       = user32.NewProc("AppendMenuW")
	pSetMenu           = user32.NewProc("SetMenu")
	pCheckMenuItem     = user32.NewProc("CheckMenuItem")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	pGetWindowPlace    = user32.NewProc("GetWindowPlacement")
	pSetWindowPlace    = user32.NewProc("SetWindowPlacement")
	pMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	pSetWindowTextW    = user32.NewProc("SetWindowTextW")
	pAdjustWindowRect  = user32.NewProc("AdjustWindowRect")
	pLoadIconW         = user32.NewProc("LoadIconW")
	pGetOpenFileNameW  = comdlg32.NewProc("GetOpenFileNameW")
	pDragAcceptFiles   = shell32.NewProc("DragAcceptFiles")
	pDragQueryFileW    = shell32.NewProc("DragQueryFileW")
	pDragFinish        = shell32.NewProc("DragFinish")
	pShellExecuteW     = shell32.NewProc("ShellExecuteW")
)

const (
	wmNull      = 0x0000
	wmCommand   = 0x0111
	wmDropFiles = 0x0233
	wmCopyData  = 0x004A
	wmAppFS     = 0x8001 // wParam: 1 = enter fullscreen, 0 = leave
	wmAppTitle  = 0x8002
	wmAppMenu   = 0x8005 // show the right-click menu at the cursor

	mfString, mfPopup, mfSeparator = 0x0, 0x10, 0x800
	mfChecked, mfUnchecked         = 0x8, 0x0
	tpmRightButton                 = 0x2
	tpmLayoutRTL                   = 0x8000

	gwlStyle   = ^uintptr(15) // -16
	wsPopup    = 0x80000000
	swpFrame   = 0x0020
	swpNoMove  = 0x0002
	swpNoSize  = 0x0001
	swpNoZ     = 0x0004
	hwndTop    = 0
	hwndTopmst = ^uintptr(0) // -1
	hwndNoTop  = ^uintptr(1) // -2
	monDefault = 2

	ofnFileMustExist = 0x1000
	ofnPathMustExist = 0x800
	ofnAllowMulti    = 0x200
	ofnExplorer      = 0x80000
	ofnNoChangeDir   = 0x8
)

// Menu command ids.
const (
	idmOpen = 100 + iota
	idmOpenSub
	idmExit
	idmFullscreen
	idmOnTop
	idmAbout
	idmCheckUpdates
	idmAutoUpdate
	idmOpenSettings
	idmPanelPlaylist
	idmPanelMenu
	idmMakeDefault
	idmOpenFolder
	idmSavePlaylist
	idmLoadPlaylist
	idmOptions
	idmLangEn
	idmLangAr
	idmWhatsNew
	idmReportBug
	idmWebsite
	idmSupport
	idmFirstMpv = 1000 // ids >= this map to mpvMenu entries
)

var mpvMenu [][]string // index = id - idmFirstMpv; the mpv command to run

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type windowPlacement struct {
	Length, Flags, ShowCmd uint32
	MinPos, MaxPos         [2]int32
	Normal                 rect
}

var (
	fsActive     bool
	fsPlacement  windowPlacement
	fsStyle      uintptr
	mainMenu     uintptr
	ctxMenu      uintptr
	onTop        bool
	titleMu      sync.Mutex
	pendingTitle string
)

func utf16z(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

// ---- menu building ----------------------------------------------------------

func addMpv(menu uintptr, label string, cmd ...string) {
	id := idmFirstMpv + len(mpvMenu)
	mpvMenu = append(mpvMenu, cmd)
	pAppendMenuW.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(utf16z(menuLabel(label)))))
}

func addItem(menu uintptr, id int, label string) {
	pAppendMenuW.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(utf16z(menuLabel(label)))))
}

// addRawItem adds an item whose text is not translated (names the user typed).
func addRawItem(menu uintptr, id int, label string) {
	pAppendMenuW.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(utf16z(label))))
}

func addSep(menu uintptr) { pAppendMenuW.Call(menu, mfSeparator, 0, 0) }

func newPopup() uintptr { h, _, _ := pCreatePopupMenu.Call(); return h }

func addSub(menu, sub uintptr, label string) {
	pAppendMenuW.Call(menu, mfPopup, sub, uintptr(unsafe.Pointer(utf16z(menuLabel(label)))))
}

// Each builder returns a fresh popup, so the menu bar and the right-click
// menu can both contain the same sections.

func fileMenu() uintptr {
	m := newPopup()
	addItem(m, idmOpen, "&Open File(s)...\tCtrl+O")
	addItem(m, idmOpenFolder, "Open &Folder...")
	addItem(m, idmOpenSub, "Open &Subtitle File...")
	addSep(m)
	addItem(m, idmSavePlaylist, "Sa&ve Playlist...\tCtrl+S")
	addItem(m, idmLoadPlaylist, "&Load Playlist...\tCtrl+L")
	addSep(m)
	addMpv(m, "Take &Screenshot\tS", "screenshot")
	addSep(m)
	addItem(m, idmExit, "E&xit\tAlt+F4")
	return m
}

func playMenu() uintptr {
	m := newPopup()
	addMpv(m, "&Play / Pause\tSpace", "cycle", "pause")
	addMpv(m, "S&top", "stop")
	addSep(m)
	addMpv(m, "&Previous\t<", "playlist-prev")
	addMpv(m, "&Next\t>", "playlist-next")
	addSep(m)
	addMpv(m, "Forward 10 s\tRight", "seek", "10")
	addMpv(m, "Back 10 s\tLeft", "seek", "-10")
	addMpv(m, "Forward 1 min\tUp", "seek", "60")
	addMpv(m, "Back 1 min\tDown", "seek", "-60")
	addMpv(m, "Next Frame\t.", "frame-step")
	addMpv(m, "Previous Frame\t,", "frame-back-step")
	addSep(m)
	addMpv(m, "Speed Up\t]", "add", "speed", "0.1")
	addMpv(m, "Speed Down\t[", "add", "speed", "-0.1")
	addMpv(m, "Normal Speed\tBackspace", "set", "speed", "1")
	addSep(m)
	addMpv(m, "Loop File", "cycle-values", "loop-file", "inf", "no")
	addMpv(m, "Loop A-B\tL", "ab-loop")
	addMpv(m, "Shuffle Playlist", "playlist-shuffle")
	return m
}

func audioMenu() uintptr {
	m := newPopup()
	addMpv(m, "Next Audio &Track\t#", "cycle", "audio")
	addSep(m)
	addMpv(m, "Volume Up\t0", "add", "volume", "5")
	addMpv(m, "Volume Down\t9", "add", "volume", "-5")
	addMpv(m, "&Mute\tM", "cycle", "mute")
	addSep(m)
	addMpv(m, "Audio Delay +0.1 s", "add", "audio-delay", "0.1")
	addMpv(m, "Audio Delay -0.1 s", "add", "audio-delay", "-0.1")
	addSep(m)
	audioFXMenu(m)
	return m
}

func videoMenu() uintptr {
	m := newPopup()
	addMpv(m, "Next Video Track\t_", "cycle", "video")
	addSep(m)
	ar := newPopup()
	addMpv(ar, "Original", "set", "video-aspect-override", "-1")
	addMpv(ar, "16:9", "set", "video-aspect-override", "16:9")
	addMpv(ar, "4:3", "set", "video-aspect-override", "4:3")
	addMpv(ar, "21:9", "set", "video-aspect-override", "21:9")
	addMpv(ar, "1:1", "set", "video-aspect-override", "1:1")
	addSub(m, ar, "&Aspect Ratio")
	addMpv(m, "&Rotate", "cycle-values", "video-rotate", "90", "180", "270", "0")
	addMpv(m, "&Deinterlace\tD", "cycle", "deinterlace")
	addSep(m)
	addMpv(m, "Brightness +", "add", "brightness", "2")
	addMpv(m, "Brightness -", "add", "brightness", "-2")
	addMpv(m, "Contrast +", "add", "contrast", "2")
	addMpv(m, "Contrast -", "add", "contrast", "-2")
	addMpv(m, "Saturation +", "add", "saturation", "2")
	addMpv(m, "Saturation -", "add", "saturation", "-2")
	addMpv(m, "Reset Picture", "set", "brightness", "0")
	return m
}

func subsMenu() uintptr {
	m := newPopup()
	addItem(m, idmOpenSub, "&Load Subtitle File...")
	addMpv(m, "Next Subtitle &Track\tJ", "cycle", "sub")
	addMpv(m, "Show / Hide\tV", "cycle", "sub-visibility")
	addSep(m)
	addMpv(m, "Delay +0.1 s\tX", "add", "sub-delay", "0.1")
	addMpv(m, "Delay -0.1 s\tZ", "add", "sub-delay", "-0.1")
	addMpv(m, "Larger", "add", "sub-scale", "0.1")
	addMpv(m, "Smaller", "add", "sub-scale", "-0.1")
	addMpv(m, "Move Up", "add", "sub-pos", "-2")
	addMpv(m, "Move Down", "add", "sub-pos", "2")
	return m
}

func viewMenu() uintptr {
	m := newPopup()
	addItem(m, idmFullscreen, "&Fullscreen\tF / Double-click")
	addItem(m, idmOnTop, "Always on &Top")
	addItem(m, idmMenuBar, "Show &Menu Bar")
	addSub(m, themeMenu(), "T&heme")
	addSep(m)
	addItem(m, idmPanelPlaylist, "&Playlist Panel")
	addItem(m, idmPanelMenu, "M&enu Panel")
	addMpv(m, "Show Controls", "script-binding", "uosc/toggle-ui")
	addMpv(m, "Playback &Statistics\tShift+I", "script-binding", "stats/display-stats-toggle")
	return m
}

func toolsMenu() uintptr {
	m := newPopup()
	addItem(m, idmOptions, "&Options...\tCtrl+,")
	addItem(m, idmEqualizer, "&Equalizer...\tCtrl+E")
	lm := newPopup()
	addRawItem(lm, idmLangEn, "English")
	addRawItem(lm, idmLangAr, "العربية (Arabic)")
	addSub(m, lm, "&Language")
	addSep(m)
	addItem(m, idmCheckUpdates, "Check for &Updates...")
	addItem(m, idmAutoUpdate, "&Automatic Component Updates")
	addSep(m)
	addItem(m, idmMakeDefault, "Make OpenPlayer the &Default Player...")
	addItem(m, idmOpenSettings, "Open &Settings Folder")
	return m
}

func helpMenu() uintptr {
	m := newPopup()
	addItem(m, idmWhatsNew, "&What's New in OpenPlayer...")
	addItem(m, idmCheckUpdates, "Check for &Updates...")
	addSep(m)
	addItem(m, idmWebsite, "OpenPlayer &Website (ai4edutech.com)")
	addItem(m, idmReportBug, "&Report a Bug...")
	addItem(m, idmSupport, "Email &Support...")
	addSep(m)
	addItem(m, idmAbout, "&About OpenPlayer")
	return m
}

func buildMenuBar() uintptr {
	bar, _, _ := pCreateMenu.Call()
	addSub(bar, fileMenu(), "&File")
	addSub(bar, playMenu(), "&Playback")
	addSub(bar, audioMenu(), "&Audio")
	addSub(bar, videoMenu(), "&Video")
	addSub(bar, subsMenu(), "&Subtitles")
	addSub(bar, viewMenu(), "V&iew")
	addSub(bar, themeMenu(), "T&heme")
	addSub(bar, toolsMenu(), "&Tools")
	addSub(bar, helpMenu(), "&Help")
	return bar
}

// buildContextMenu is the right-click menu shown over the video.
func buildContextMenu() uintptr {
	m := newPopup()
	addItem(m, idmOpen, "Open File(s)...\tCtrl+O")
	addSep(m)
	addMpv(m, "Play / Pause\tSpace", "cycle", "pause")
	addMpv(m, "Stop", "stop")
	addMpv(m, "Previous\t<", "playlist-prev")
	addMpv(m, "Next\t>", "playlist-next")
	addSep(m)
	addItem(m, idmFullscreen, "Fullscreen\tF")
	addItem(m, idmOnTop, "Always on Top")
	addItem(m, idmPanelPlaylist, "Playlist Panel")
	addItem(m, idmPanelMenu, "Menu Panel")
	addSep(m)
	addSub(m, playMenu(), "Playback")
	addSub(m, audioMenu(), "Audio")
	addSub(m, videoMenu(), "Video")
	addSub(m, subsMenu(), "Subtitles")
	addSub(m, viewMenu(), "View")
	addSep(m)
	addSub(m, toolsMenu(), "Tools")
	addItem(m, idmOptions, "Options...")
	addItem(m, idmAbout, "About OpenPlayer")
	addItem(m, idmExit, "Exit")
	return m
}

// setCheck updates a checkable item in both menus (MF_BYCOMMAND searches sub-menus).
func setCheck(id int, on bool) {
	c := uintptr(mfUnchecked)
	if on {
		c = mfChecked
	}
	pCheckMenuItem.Call(mainMenu, uintptr(id), c)
	pCheckMenuItem.Call(ctxMenu, uintptr(id), c)
}

func showContextMenu() {
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWnd.Call(mainWnd) // required so the menu closes when clicking elsewhere
	flags := uintptr(tpmRightButton)
	if rtl() {
		flags |= tpmLayoutRTL
	}
	pTrackPopupMenu.Call(ctxMenu, flags, uintptr(pt[0]), uintptr(pt[1]), 0, mainWnd, 0)
	pPostMessageW.Call(mainWnd, wmNull, 0, 0)
}

// handleCommand runs a menu command on the window thread.
func handleCommand(id int) {
	if audioCommand(id) {
		return
	}
	switch {
	case id == idmOptions:
		showOptions("")
	case id == idmLangEn:
		setLanguage("en")
	case id == idmLangAr:
		setLanguage("ar")
	case id == idmOpen:
		if files := openDialog(mediaFilter(), true); len(files) > 0 {
			openFiles(files, false)
		}
	case id == idmOpenSub:
		if files := openDialog(subtitleFilter(), false); len(files) > 0 {
			mpv.Async(mp, "sub-add", files[0], "select")
		}
	case id == idmExit:
		pPostMessageW.Call(mainWnd, wmClose, 0, 0)
	case id == idmFullscreen:
		toggleFullscreen()
	case id == idmOnTop:
		onTop = !onTop
		z := hwndNoTop
		if onTop {
			z = hwndTopmst
		}
		pSetWindowPos.Call(mainWnd, z, 0, 0, 0, 0, swpNoMove|swpNoSize)
		setCheck(idmOnTop, onTop)
	case id == idmSavePlaylist:
		savePlaylist()
	case id == idmLoadPlaylist:
		loadPlaylist()
	case id == idmOpenFolder:
		openFolder()
	case id == idmMakeDefault:
		makeDefaultCommand()
	case id == idmPanelPlaylist:
		togglePanel("playlist", "")
	case id == idmPanelMenu:
		togglePanel("menu", "")
	case id == idmMenuBar:
		applyMenuBar(!menuBarOn(), true)
	case id >= idmThemeFirst && id < idmThemeFirst+len(themes):
		applyTheme(themes[id-idmThemeFirst], true)
	case id == idmCheckUpdates:
		startUpdateCheck(true)
	case id == idmAutoUpdate:
		on := !cfg.flag("auto_update", true)
		_ = cfg.set("auto_update", map[bool]string{true: "yes", false: "no"}[on])
		setCheck(idmAutoUpdate, on)
	case id == idmOpenSettings:
		pShellExecuteW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z("open"))),
			uintptr(unsafe.Pointer(utf16z(filepath.Join(appRoot, "config")))), 0, 0, 1)
	case id == idmAbout:
		messageInfo(T("About OpenPlayer"), "OpenPlayer "+appVersion+"\n\n"+T("A fast, clean, open-source media player.")+"\n"+
			T("No ads, no tracking, no bundled software.")+"\n\n"+
			T("Made by Ahmad Elhendawy · AI4EduTech")+"\n"+
			T("Website:")+" "+websiteURL+"\n"+
			T("Support:")+" "+supportEmail+"\n"+
			T("Source code (GPL-3.0):")+" github.com/ahmadelhendawy1/openplayer\n\n"+
			T("Engine: libmpv (LGPL build) with FFmpeg.")+"\n"+T("On-screen controls: uosc (LGPL-2.1)."))
	case id == idmWhatsNew:
		showWhatsNew()
	case id == idmReportBug:
		showBugReport()
	case id == idmWebsite:
		openPath(websiteURL)
	case id == idmSupport:
		openPath("mailto:" + supportEmail + "?subject=" + strings.ReplaceAll("OpenPlayer "+appVersion+" support", " ", "%20"))
	case id >= idmFirstMpv && id-idmFirstMpv < len(mpvMenu):
		mpv.Async(mp, mpvMenu[id-idmFirstMpv]...)
	}
}

// openFiles plays files (replacing the playlist) or appends them.
// Subtitle files are attached to the current video instead.
func openFiles(files []string, enqueue bool) {
	cancelRestore()
	subs := map[string]bool{}
	for _, e := range extensionLists()["subtitle"] {
		subs["."+e] = true
	}
	var media []string
	for _, f := range files {
		if subs[strings.ToLower(filepath.Ext(f))] {
			mpv.Async(mp, "sub-add", f, "select")
			continue
		}
		media = append(media, f)
	}
	mode := "replace"
	if enqueue {
		mode = "append-play"
	}
	switch {
	case len(media) == 1:
		mpv.Async(mp, "loadfile", media[0], mode)
	case len(media) > 1:
		// Many files at once go to the engine as ONE playlist command, not
		// one command per file.
		if list := writeTempList(media); list != "" {
			mpv.Async(mp, "loadlist", list, mode)
		} else {
			for i, f := range media {
				m := "append-play"
				if i == 0 && !enqueue {
					m = "replace"
				}
				mpv.Async(mp, "loadfile", f, m)
			}
		}
	}
	logf("open: %d file(s), enqueue=%v", len(files), enqueue)
	// mpv keeps "pause" across files: a newly opened file must always play,
	// and an added file starts playing if nothing was playing.
	stateMu.Lock()
	idle := mpvState["idle-active"] == "yes"
	stateMu.Unlock()
	if !enqueue || idle {
		mpv.Async(mp, "set", "pause", "no")
	}
	if enqueue {
		mpv.Async(mp, "show-text", Tf("Added %d item(s) to the playlist", len(files)), "2500")
	}
}

// handleDrop reads files dropped from Explorer (WM_DROPFILES).
// enqueue=true adds the files to the playlist (drop on the playlist panel);
// false plays them now (drop on the video or the start screen).
func handleDrop(hDrop uintptr, enqueue bool) {
	n, _, _ := pDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	var files []string
	for i := uintptr(0); i < n; i++ {
		l, _, _ := pDragQueryFileW.Call(hDrop, i, 0, 0)
		buf := make([]uint16, l+1)
		pDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		files = append(files, syscall.UTF16ToString(buf))
	}
	pDragFinish.Call(hDrop)
	logf("drop: %d file(s), enqueue=%v", len(files), enqueue)
	if len(files) > 0 {
		openFiles(files, enqueue)
	}
}

// ---- fullscreen -----------------------------------------------------------

func toggleFullscreen() {
	setFullscreen(!fsActive)
	v := "no"
	if fsActive {
		v = "yes"
	}
	mpv.Async(mp, "set", "fullscreen", v) // keep the uosc button state in sync
}

// setFullscreen is idempotent; it only acts when the state changes.
func setFullscreen(on bool) {
	if on == fsActive {
		return
	}
	fsActive = on
	defer updateGlass()
	if on {
		fsPlacement.Length = uint32(unsafe.Sizeof(fsPlacement))
		pGetWindowPlace.Call(mainWnd, uintptr(unsafe.Pointer(&fsPlacement)))
		fsStyle, _, _ = pGetWindowLongPtrW.Call(mainWnd, gwlStyle)
		mon, _, _ := pMonitorFromWindow.Call(mainWnd, monDefault)
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		pSetMenu.Call(mainWnd, 0)
		pSetWindowLongPtrW.Call(mainWnd, gwlStyle, wsPopup|wsVisible|wsClipChildren)
		pSetWindowPos.Call(mainWnd, hwndTop, uintptr(mi.Monitor.Left), uintptr(mi.Monitor.Top),
			uintptr(mi.Monitor.Right-mi.Monitor.Left), uintptr(mi.Monitor.Bottom-mi.Monitor.Top), swpFrame)
		showCursor() // starts the hide timer
		return
	}
	showCursor()
	pSetWindowLongPtrW.Call(mainWnd, gwlStyle, fsStyle)
	if menuBarOn() {
		pSetMenu.Call(mainWnd, mainMenu)
	}
	pSetWindowPlace.Call(mainWnd, uintptr(unsafe.Pointer(&fsPlacement)))
	pSetWindowPos.Call(mainWnd, 0, 0, 0, 0, 0, swpFrame|swpNoMove|swpNoSize|swpNoZ)
}

// ---- title ----------------------------------------------------------------

func queueTitle(t string) {
	titleMu.Lock()
	pendingTitle = t
	titleMu.Unlock()
	pPostMessageW.Call(mainWnd, wmAppTitle, 0, 0)
}

func applyTitle() {
	titleMu.Lock()
	t := pendingTitle
	titleMu.Unlock()
	if t == "" {
		t = "OpenPlayer"
	} else {
		t += " - OpenPlayer"
	}
	pSetWindowTextW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z(t))))
}

// ---- file dialog ----------------------------------------------------------

type openFileName struct {
	StructSize   uint32
	Owner        uintptr
	Instance     uintptr
	Filter       *uint16
	CustomFilter *uint16
	MaxCustFilt  uint32
	FilterIndex  uint32
	File         *uint16
	MaxFile      uint32
	FileTitle    *uint16
	MaxFileTitle uint32
	InitialDir   *uint16
	Title        *uint16
	Flags        uint32
	FileOffset   uint16
	FileExt      uint16
	DefExt       *uint16
	CustData     uintptr
	FnHook       uintptr
	TemplateName *uint16
	PvReserved   uintptr
	DwReserved   uint32
	FlagsEx      uint32
}

func extensionLists() map[string][]string { return mediaext.Lists() }

func globs(exts ...[]string) string {
	var g []string
	for _, l := range exts {
		for _, e := range l {
			g = append(g, "*."+e)
		}
	}
	return strings.Join(g, ";")
}

// dialog filters are "name\0pattern\0...\0\0"
func mediaFilter() string {
	m := extensionLists()
	return T("All media files") + "\x00" + globs(m["video"], m["audio"]) +
		"\x00" + T("Video files") + "\x00" + globs(m["video"]) +
		"\x00" + T("Audio files") + "\x00" + globs(m["audio"]) + "\x00" + T("All files") + "\x00*.*\x00"
}

func subtitleFilter() string {
	return T("Subtitle files") + "\x00" + globs(extensionLists()["subtitle"]) + "\x00" + T("All files") + "\x00*.*\x00"
}

func openDialog(filter string, multi bool) []string {
	buf := make([]uint16, 64*1024)
	f := utf16.Encode([]rune(filter + "\x00"))
	ofn := openFileName{
		Owner: mainWnd, Filter: &f[0], FilterIndex: 1,
		File: &buf[0], MaxFile: uint32(len(buf)), Title: utf16z(T("Open")),
		Flags: ofnFileMustExist | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if multi {
		ofn.Flags |= ofnAllowMulti
	}
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return nil // cancelled
	}
	return splitMulti(buf)
}

// splitMulti decodes "path\0" (single) or "dir\0file1\0file2\0\0" (multi).
func splitMulti(buf []uint16) []string {
	var parts []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			parts = append(parts, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts
	}
	dir := strings.TrimRight(parts[0], `\`)
	out := make([]string, 0, len(parts)-1)
	for _, n := range parts[1:] {
		out = append(out, dir+`\`+n)
	}
	return out
}

func messageInfo(title, text string) {
	pMessageBoxW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z(text))), uintptr(unsafe.Pointer(utf16z(title))), 0x40)
}

// ---- volume button ------------------------------------------------------------
//
// First click shows the volume slider; a second click while it is showing
// toggles mute (like Zoom Player and VLC).

const wmAppVolume = 0x800A

var lastVolumeClick time.Time

func volumeButton() {
	if time.Since(lastVolumeClick) < 3*time.Second {
		mpv.Async(mp, "cycle", "mute")
	}
	mpv.Async(mp, "script-binding", "uosc/flash-volume")
	lastVolumeClick = time.Now()
}

// ---- menu rebuilds (language change, new equalizer presets) -----------------

var (
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pGetMenuItemCount = user32.NewProc("GetMenuItemCount")
	pSetMenuItemInfoW = user32.NewProc("SetMenuItemInfoW")
)

const (
	miimFType     = 0x100
	miimSubmenu   = 0x4
	mftRightOrder = 0x2000
)

// rtlMenu marks every item right-to-left (Arabic), recursively.
func rtlMenu(h uintptr) {
	n, _, _ := pGetMenuItemCount.Call(h)
	for i := 0; i < int(int32(n)); i++ {
		mii := menuItemInfo{Mask: miimFType | miimSubmenu}
		mii.Size = uint32(unsafe.Sizeof(mii))
		if r, _, _ := pGetMenuItemInfoW.Call(h, uintptr(i), 1, uintptr(unsafe.Pointer(&mii))); r == 0 {
			continue
		}
		sub := mii.SubMenu
		mii.Mask = miimFType
		mii.Type |= mftRightOrder
		pSetMenuItemInfoW.Call(h, uintptr(i), 1, uintptr(unsafe.Pointer(&mii)))
		if sub != 0 {
			rtlMenu(sub)
		}
	}
}

func buildMenus() {
	mpvMenu = nil
	mainMenu = buildMenuBar()
	ctxMenu = buildContextMenu()
	if rtl() {
		rtlMenu(mainMenu)
		rtlMenu(ctxMenu)
	}
}

func rebuildMenus() {
	oldMain, oldCtx := mainMenu, ctxMenu
	buildMenus()
	if menuBarOn() && !fsActive {
		pSetMenu.Call(mainWnd, mainMenu)
	}
	pDestroyMenu.Call(oldMain)
	pDestroyMenu.Call(oldCtx)
	refreshAllChecks()
	pDrawMenuBar.Call(mainWnd)
}

// refreshAllChecks re-applies every check mark (after menus are rebuilt).
func refreshAllChecks() {
	setCheck(idmAutoUpdate, cfg.flag("auto_update", true))
	setCheck(idmOnTop, onTop)
	setCheck(idmPanelPlaylist, rightPanel.visible)
	setCheck(idmPanelMenu, leftPanel.visible)
	setCheck(idmMenuBar, menuBarOn())
	t := currentTheme()
	for i, th := range themes {
		setCheck(idmThemeFirst+i, th.ID == t.ID)
	}
	setCheck(idmLangEn, uiLang == "en")
	setCheck(idmLangAr, uiLang == "ar")
	refreshAudioChecks()
}
