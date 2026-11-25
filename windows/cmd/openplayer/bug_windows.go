//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

//go:embed bug.html
var bugHTML string

const wmAppBugDone = 0x8010 // a report finished sending

var (
	bugMu     sync.Mutex
	bugResult map[string]any // last send result, shown by the window thread

	bugWin = &webWin{name: "bug", title: "Report a bug", w: 600, h: 640, frame: true, minW: 480, minH: 520,
		page: func() string { return bugHTML }}

	pRtlGetVersion = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
)

func init() {
	bugWin.onMsg = bugMessage
	bugWin.onReady = pushBugInfo
	bugWin.position = func(w, h int) (int, int) {
		var r rect
		pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&r)))
		return int(r.Left+r.Right)/2 - w/2, max(int(r.Top+r.Bottom)/2-h/2, 0)
	}
}

// showBugReport runs on the window thread (Help > Report a Bug).
func showBugReport() {
	bugWin.show()
	pushBugInfo()
}

// windowsVersion is e.g. "Windows 11 (build 26100)".
func windowsVersion() string {
	var v struct {
		Size                             uint32
		Major, Minor, Build, PlatformID uint32
		CSD                              [128]uint16
	}
	v.Size = uint32(unsafe.Sizeof(v))
	pRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	name := fmt.Sprintf("Windows %d.%d", v.Major, v.Minor)
	if v.Major == 10 {
		name = "Windows 10"
		if v.Build >= 22000 {
			name = "Windows 11"
		}
	}
	return fmt.Sprintf("%s (build %d)", name, v.Build)
}

// currentMedia describes what is playing, without the file name.
func currentMedia() string {
	stateMu.Lock()
	tl, pl := mpvState["track-list"], mpvState["playlist"]
	stateMu.Unlock()
	_, file := currentEntry(pl)
	return mediaSummary(tl, filepath.Ext(file))
}

func pushBugInfo() {
	if !bugWin.ready {
		return
	}
	b, _ := json.Marshal(map[string]any{"version": appVersion, "os": windowsVersion(), "media": currentMedia(), "github": githubURL})
	bugWin.eval("OP.info(" + string(b) + ")")
}

func bugMessage(a string, v json.RawMessage) {
	var r bugReport
	_ = json.Unmarshal(v, &r)
	r.Version, r.OS, r.Lang, r.Media = appVersion, windowsVersion(), uiLang, currentMedia()
	switch a {
	case "github":
		openPath(githubIssueURL(r))
	case "send":
		var in struct {
			Log bool `json:"includeLog"`
		}
		_ = json.Unmarshal(v, &in)
		if in.Log {
			if b, err := os.ReadFile(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "openplayer.log")); err == nil {
				r.Log = lastLines(string(b), 300, 60<<10)
			}
		}
		go sendBug(r)
	}
}

func sendBug(r bugReport) {
	res := map[string]any{}
	body, _ := json.Marshal(r)
	c := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("POST", bugAPI, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenPlayer/"+appVersion)
	resp, err := c.Do(req)
	if err != nil {
		res["error"] = err.Error()
	} else {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		var out struct {
			ID, Error string
		}
		_ = json.Unmarshal(b, &out)
		switch {
		case resp.StatusCode == 200 && strings.HasPrefix(out.ID, "OP-"):
			res["id"] = out.ID
			logf("bug report sent: %s", out.ID)
		case out.Error != "":
			res["error"] = out.Error
		default:
			res["error"] = resp.Status
		}
	}
	bugMu.Lock()
	bugResult = res
	bugMu.Unlock()
	pPostMessageW.Call(mainWnd, wmAppBugDone, 0, 0)
}

// onBugDone runs on the window thread.
func onBugDone() {
	bugMu.Lock()
	res := bugResult
	bugMu.Unlock()
	b, _ := json.Marshal(res)
	bugWin.eval("OP.sent(" + string(b) + ")")
}
