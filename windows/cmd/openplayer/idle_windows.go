//go:build windows

package main

// Idle screen: when nothing is playing, a WebView2 layer covers the video
// area with the theme's landscape and a glass welcome card (real CSS backdrop
// blur over the artwork). It is hidden as soon as playback starts, so the
// artwork is never drawn over video.

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed idle.html
var idleHTMLRaw string

//go:embed landscape.js
var landscapeJS string

//go:embed icon.png
var iconPNG []byte

var idleView = &panel{side: "idle"}

func idleHTML() string  { return strings.Replace(idleHTMLRaw, "/*LANDSCAPE*/", landscapeJS, 1) }
func panelPage() string { return strings.Replace(panelHTML, "/*LANDSCAPE*/", landscapeJS, 1) }

func iconDataURI() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG)
}

// updateIdle shows the idle layer while mpv is idle and hides it otherwise.
func updateIdle() {
	stateMu.Lock()
	idle := mpvState["idle-active"] == "yes"
	stateMu.Unlock()
	switch {
	case idle && !idleView.visible && !idleView.failed:
		if idleView.web == nil {
			idleView.create()
		} else {
			idleView.visible = true
			layout()
		}
	case !idle && idleView.visible:
		idleView.visible = false
		layout()
		pSetFocus.Call(mainWnd)
	}
	ensureCoverArt()
}

func idleSetTheme(t theme) {
	idleView.eval("OP.theme(" + t.panelJSON() + ")")
}

// ---- open folder ------------------------------------------------------------

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	Param       uintptr
	Image       int32
}

var (
	pBrowseFolder  = shell32.NewProc("SHBrowseForFolderW")
	pPathFromIDL   = shell32.NewProc("SHGetPathFromIDListW")
	pCoTaskMemFree = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")
)

func openFolder() {
	name := make([]uint16, 260)
	bi := browseInfo{Owner: mainWnd, DisplayName: &name[0], Title: utf16z(T("Choose a folder to play")), Flags: 0x1 | 0x40}
	pidl, _, _ := pBrowseFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return
	}
	defer pCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 1024)
	if r, _, _ := pPathFromIDL.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		openFiles([]string{syscall.UTF16ToString(buf)}, false) // mpv plays the folder's media in order
	}
}

// ---- cover placeholder for audio without artwork ---------------------------------

var coverDoneFor string

// ensureCoverArt adds the theme's vector-made cover image to audio files that
// have no picture of their own, so audio never plays over an empty canvas.
func ensureCoverArt() {
	stateMu.Lock()
	tracksJS, plJS := mpvState["track-list"], mpvState["playlist"]
	stateMu.Unlock()
	var tracks []struct {
		Type     string `json:"type"`
		Albumart bool   `json:"albumart"`
	}
	var pl []struct {
		Filename string `json:"filename"`
		Current  bool   `json:"current"`
	}
	if json.Unmarshal([]byte(tracksJS), &tracks) != nil || json.Unmarshal([]byte(plJS), &pl) != nil || len(tracks) == 0 {
		return
	}
	cur := ""
	for _, e := range pl {
		if e.Current {
			cur = e.Filename
		}
	}
	if cur == "" || cur == coverDoneFor {
		return
	}
	for _, t := range tracks {
		if t.Type == "video" {
			coverDoneFor = cur // has video or embedded artwork
			return
		}
	}
	hasAudio := false
	for _, t := range tracks {
		hasAudio = hasAudio || t.Type == "audio"
	}
	if !hasAudio {
		return
	}
	coverDoneFor = cur
	cover := filepath.Join(appRoot, "config", "covers", currentTheme().ID+".png")
	if _, err := os.Stat(cover); err == nil {
		mpv.Async(mp, "video-add", cover, "select", "OpenPlayer cover")
	}
}
