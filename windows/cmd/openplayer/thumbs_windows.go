//go:build windows

package main

// Playlist thumbnails. A second, hidden libmpv instance (vo=image, no audio)
// grabs one frame at 12 % of each video, or the embedded cover of a song,
// scaled to 320 px. Results are cached in %LOCALAPPDATA%\OpenPlayer\thumbs,
// keyed by path, size and modification time, and sent to the playlist panel
// as data: URLs.

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	wmAppThumb   = 0x800C
	thumbTimeout = 6 * time.Second
)

var (
	thumbMu      sync.Mutex
	thumbData    = map[string]string{} // path -> data URL ("" = none available)
	thumbQueued  = map[string]bool{}
	thumbReady   []string // paths waiting to be sent to the panel
	thumbJobs    = make(chan string, 4096)
	thumbStarted sync.Once
	thumbDir     = filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "thumbs")
)

// queueThumbs is called with the playlist JSON (event thread).
func queueThumbs(playlistJSON string) {
	var pl []struct {
		Filename string `json:"filename"`
	}
	if json.Unmarshal([]byte(playlistJSON), &pl) != nil {
		return
	}
	thumbMu.Lock()
	var add []string
	for _, e := range pl {
		f := e.Filename
		if f == "" || strings.Contains(f, "://") || thumbQueued[f] {
			continue
		}
		thumbQueued[f] = true
		add = append(add, f)
	}
	thumbMu.Unlock()
	if len(add) == 0 {
		return
	}
	thumbStarted.Do(func() { go thumbWorker() })
	for _, f := range add {
		select {
		case thumbJobs <- f:
		default: // very long playlist: the rest get icons
		}
	}
}

func thumbKey(path string) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", false
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", strings.ToLower(path), st.Size(), st.ModTime().UnixNano())))
	return hex.EncodeToString(h[:]), true
}

func thumbWorker() {
	// Windows thumbnails are COM objects: this goroutine keeps one OS thread
	// with COM initialised for its whole life.
	runtime.LockOSThread()
	pCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED
	_ = os.MkdirAll(thumbDir, 0o755)
	_ = os.RemoveAll(filepath.Join(thumbDir, "work")) // left over from 0.9.0-0.9.3
	for path := range thumbJobs {
		uri := ""
		key, ok := thumbKey(path)
		if ok {
			cache := filepath.Join(thumbDir, key+".jpg")
			none := filepath.Join(thumbDir, key+".none")
			if b, err := os.ReadFile(cache); err == nil {
				uri = jpegURI(b)
			} else if _, err := os.Stat(none); err == nil {
				uri = ""
			} else {
				t0 := time.Now()
				b := shellThumb(path)
				logf("thumbnail: %.2fs ok=%v", time.Since(t0).Seconds(), b != nil)
				if b != nil {
					_ = os.WriteFile(cache, b, 0o644)
					uri = jpegURI(b)
				} else {
					_ = os.WriteFile(none, nil, 0o644) // no thumbnail from Windows (e.g. a song without a cover)
				}
			}
		}
		thumbMu.Lock()
		thumbData[path] = uri
		if uri != "" {
			thumbReady = append(thumbReady, path)
		}
		thumbMu.Unlock()
		if uri != "" {
			pPostMessageW.Call(mainWnd, wmAppThumb, 0, 0)
		}
	}
}

func jpegURI(b []byte) string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
}

// sendReadyThumbs runs on the window thread.
func sendReadyThumbs() {
	thumbMu.Lock()
	paths := thumbReady
	thumbReady = nil
	thumbMu.Unlock()
	_ = paths
	pushThumbs(rightPanel)
}

// thumbSent: thumbnails the playlist page already has (window thread only;
// cleared when the page loads).
var thumbSent = map[string]bool{}

// pushThumbs sends the thumbnails the page does not have yet.
func pushThumbs(p *panel) {
	if !p.ready {
		return
	}
	thumbMu.Lock()
	todo := map[string]string{}
	for k, v := range thumbData {
		if v != "" && !thumbSent[k] {
			todo[k] = v
		}
	}
	thumbMu.Unlock()
	for k, v := range todo {
		thumbSent[k] = true
		p.eval("OP.thumb(" + jsStr(k) + "," + jsStr(v) + ")")
	}
}

func isAudioPath(p string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
	for _, e := range extensionLists()["audio"] {
		if e == ext {
			return true
		}
	}
	return false
}
