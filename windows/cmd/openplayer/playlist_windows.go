//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"
)

const ofnOverwritePrompt = 0x2

var pGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")

func playlistFilter() string {
	return T("Playlists") + " (*.m3u8, *.m3u, *.pls)\x00*.m3u8;*.m3u;*.pls\x00" + T("All files") + "\x00*.*\x00"
}

func playlistDir() string {
	if d := cfg.get("playlist_dir", ""); d != "" {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Music")
}

// saveDialog asks where to save; returns "" when cancelled.
func saveDialog(filter, defName, defExt, initialDir string) string {
	buf := make([]uint16, 4096)
	copy(buf, utf16.Encode([]rune(defName)))
	f := utf16.Encode([]rune(filter + "\x00"))
	ofn := openFileName{
		Owner: mainWnd, Filter: &f[0], FilterIndex: 1,
		File: &buf[0], MaxFile: uint32(len(buf)), Title: utf16z(T("Save Playlist")),
		InitialDir: utf16z(initialDir), DefExt: utf16z(defExt),
		Flags: ofnOverwritePrompt | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := pGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return ""
	}
	if p := splitMulti(buf); len(p) > 0 {
		return p[0]
	}
	return ""
}

func savePlaylist() {
	stateMu.Lock()
	js := mpvState["playlist"]
	stateMu.Unlock()
	text, n := buildM3U(js)
	if n == 0 {
		mpv.Async(mp, "show-text", T("The playlist is empty"), "2000")
		return
	}
	path := saveDialog(playlistFilter(), "OpenPlayer playlist.m3u8", "m3u8", playlistDir())
	if path == "" {
		return
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".m3u8" && ext != ".m3u" {
		path += ".m3u8"
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		logf("save playlist: %v", err)
		messageInfo("OpenPlayer", T("The playlist could not be saved:")+"\n"+err.Error())
		return
	}
	_ = cfg.set("playlist_dir", filepath.Dir(path))
	mpv.Async(mp, "show-text", Tf("Playlist saved: %s (%d items)", filepath.Base(path), n), "2500")
}

func loadPlaylist() {
	files := openDialogIn(playlistFilter(), playlistDir())
	if len(files) == 0 {
		return
	}
	path := files[0]
	_ = cfg.set("playlist_dir", filepath.Dir(path))
	mpv.Async(mp, "loadlist", path, "replace")
	mpv.Async(mp, "set", "pause", "no")
	mpv.Async(mp, "show-text", Tf("Playlist loaded: %s", filepath.Base(path)), "2500")
}

// openDialogIn is openDialog (single file) starting in a given folder.
func openDialogIn(filter, dir string) []string {
	buf := make([]uint16, 4096)
	f := utf16.Encode([]rune(filter + "\x00"))
	ofn := openFileName{
		Owner: mainWnd, Filter: &f[0], FilterIndex: 1,
		File: &buf[0], MaxFile: uint32(len(buf)), Title: utf16z(T("Load Playlist")), InitialDir: utf16z(dir),
		Flags: ofnFileMustExist | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return nil
	}
	return splitMulti(buf)
}

// writeTempList writes files to a temporary .m3u8 (removing old ones) and
// returns its path, or "" on failure.
func writeTempList(files []string) string {
	dir := filepath.Join(os.TempDir(), "OpenPlayer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "add-*.m3u8")); len(old) > 0 {
		for _, f := range old {
			if st, err := os.Stat(f); err == nil && time.Since(st.ModTime()) > 10*time.Minute {
				_ = os.Remove(f)
			}
		}
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\r\n")
	for _, f := range files {
		if !strings.ContainsAny(f, "\r\n") {
			b.WriteString(f + "\r\n")
		}
	}
	p := filepath.Join(dir, "add-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".m3u8")
	if os.WriteFile(p, []byte(b.String()), 0o644) != nil {
		return ""
	}
	return p
}
