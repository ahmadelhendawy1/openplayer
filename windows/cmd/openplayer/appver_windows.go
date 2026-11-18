//go:build windows

package main

// Update notice and self-update. The player reads latest.json from the
// download site at every launch (and every few hours while it runs). When a
// newer version is listed it opens the update window with the release notes;
// "Update now" downloads the installer, checks its SHA-256 against
// latest.json, starts it and closes the player so the files can be replaced.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

//go:embed update.html
var updateHTML string

const (
	wmAppNewVersion = 0x800D // wp=1: show the window even when up to date
	wmAppUpdProg    = 0x800E
	wmAppUpdDone    = 0x800F // wp=1: installer ready, 0: failed
	recheckEvery    = 6 * time.Hour
)

var (
	updMu       sync.Mutex
	updLatest   latestInfo
	updHave     bool
	updPct      int
	updText     string
	updErr      string
	updFile     string
	updCancel   context.CancelFunc
	updChecking bool
	// updShownThisRun: a forced update has been offered since this start.
	updShownThisRun bool
	// updWhatsNew: the window shows this version's notes (Help > What's New).
	updWhatsNew bool

	updWin = &webWin{name: "update", title: "OpenPlayer update", w: 560, h: 500, frame: true, minW: 460, minH: 380,
		page: func() string { return updateHTML }}
)

func init() {
	updWin.onMsg = updateMessage
	updWin.onReady = pushUpdate
	updWin.position = func(w, h int) (int, int) {
		var r rect
		pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&r)))
		return int(r.Left+r.Right)/2 - w/2, max(int(r.Top+r.Bottom)/2-h/2, 0)
	}
}

// maybeCheckAppVersion checks a few seconds after start and then every few
// hours while OpenPlayer stays open.
func maybeCheckAppVersion() {
	if !cfg.flag("check_app", true) {
		return
	}
	go func() {
		time.Sleep(5 * time.Second) // let playback start first
		for {
			checkAppVersion(false)
			time.Sleep(recheckEvery)
		}
	}()
}

// checkAppVersion runs on a worker goroutine.
func checkAppVersion(interactive bool) {
	updMu.Lock()
	if updChecking {
		updMu.Unlock()
		return
	}
	updChecking = true
	updMu.Unlock()
	defer func() { updMu.Lock(); updChecking = false; updMu.Unlock() }()

	l, err := fetchLatest()
	_ = cfg.set("last_app_check", strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		logf("version check: %v", err)
		if interactive {
			postNotice(T("Could not check for a new version:") + "\n" + err.Error())
		}
		return
	}
	newer := isNewer(l.Version, appVersion)
	logf("version check: latest %s, this is %s (newer=%v)", l.Version, appVersion, newer)
	updMu.Lock()
	updLatest, updHave = l, true
	updMu.Unlock()
	if !newer && !interactive {
		return
	}
	if newer && !interactive && l.Force {
		updMu.Lock()
		shown := updShownThisRun
		updShownThisRun = true
		updMu.Unlock()
		if shown {
			return // already asked since this start
		}
	} else if newer && !interactive && cfg.get("remind_version", "") == l.Version {
		if t, _ := strconv.ParseInt(cfg.get("remind_after", "0"), 10, 64); time.Now().Unix() < t {
			return // "Later" was chosen for this version less than a day ago
		}
	}
	pPostMessageW.Call(mainWnd, wmAppNewVersion, 1, 0)
}

func fetchLatest() (latestInfo, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", latestURL+"?t="+strconv.FormatInt(time.Now().Unix()/60, 10), nil) // skip stale caches
	req.Header.Set("User-Agent", "OpenPlayer/"+appVersion)
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.Do(req)
	if err != nil {
		return latestInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return latestInfo{}, errStatus(resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return latestInfo{}, err
	}
	l, ok := parseLatest(b)
	if !ok {
		return latestInfo{}, errStatus("invalid latest.json")
	}
	return l, nil
}

type errStatus string

func (e errStatus) Error() string { return string(e) }

// showNewVersion runs on the window thread.
func showNewVersion() {
	updMu.Lock()
	updWhatsNew = false
	updMu.Unlock()
	updWin.show()
	pushUpdate()
}

// showWhatsNew runs on the window thread (Help > What's New).
func showWhatsNew() {
	updMu.Lock()
	updWhatsNew = true
	updMu.Unlock()
	updWin.show()
	pushUpdate()
}

// noteUpdated runs at start-up: after an update, say so once on screen.
func noteUpdated() {
	last := cfg.get("last_run_version", "")
	_ = cfg.set("last_run_version", appVersion)
	if last != "" && last != appVersion {
		logf("updated from %s to %s", last, appVersion)
		mpv.Async(mp, "show-text", Tf("OpenPlayer was updated to %s. See what's new: Help › What's New", appVersion), "7000")
	}
}

func pushUpdate() {
	if !updWin.ready {
		return
	}
	updMu.Lock()
	l, have, wn := updLatest, updHave, updWhatsNew
	updMu.Unlock()
	if wn {
		notes := whatsNewEN
		if uiLang == "ar" {
			notes = whatsNewAR
		}
		b, _ := json.Marshal(map[string]any{"whatsnew": true, "newer": false, "current": appVersion, "version": appVersion,
			"date": humanDate(whatsNewDate), "notes": notesList(notes)})
		updWin.eval("OP.data(" + string(b) + ")")
		return
	}
	if !have {
		return
	}
	notes := l.NotesEN
	if uiLang == "ar" && l.NotesAR != "" {
		notes = l.NotesAR
	}
	date := humanDate(l.Date)
	size := ""
	if l.SizeMB > 0 {
		size = strconv.FormatFloat(l.SizeMB, 'f', 1, 64)
	}
	b, _ := json.Marshal(map[string]any{"newer": isNewer(l.Version, appVersion), "current": appVersion, "version": l.Version,
		"date": date, "size": size, "notes": notesList(notes), "canSelf": l.canSelfUpdate()})
	updWin.eval("OP.data(" + string(b) + ")")
}

func humanDate(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	if uiLang == "ar" {
		months := []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
		return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
	}
	return t.Format("2 January 2006")
}

func updateMessage(a string, _ json.RawMessage) {
	updMu.Lock()
	l := updLatest
	cancel := updCancel
	updMu.Unlock()
	switch a {
	case "later":
		if cancel != nil {
			cancel()
		}
		if isNewer(l.Version, appVersion) && !l.Force {
			_ = cfg.set("remind_version", l.Version)
			_ = cfg.set("remind_after", strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10))
		}
		updWin.hide()
	case "page":
		openPath(l.URL)
		updWin.hide()
	case "update":
		if cancel == nil && l.canSelfUpdate() {
			startDownload(l)
		}
	}
}

func startDownload(l latestInfo) {
	ctx, cancel := context.WithCancel(context.Background())
	updMu.Lock()
	updCancel = cancel
	updMu.Unlock()
	setProgress(0, T("Starting the download…"))
	go func() {
		defer func() { updMu.Lock(); updCancel = nil; updMu.Unlock(); cancel() }()
		path, err := downloadInstaller(ctx, l)
		updMu.Lock()
		updFile, updErr = path, ""
		if err != nil {
			updErr = err.Error()
		}
		updMu.Unlock()
		ok := uintptr(0)
		if err == nil {
			ok = 1
		} else {
			logf("update download: %v", err)
		}
		if ctx.Err() == nil || err == nil {
			pPostMessageW.Call(mainWnd, wmAppUpdDone, ok, 0)
		}
	}()
}

func setProgress(pct int, text string) {
	updMu.Lock()
	updPct, updText = pct, text
	updMu.Unlock()
	pPostMessageW.Call(mainWnd, wmAppUpdProg, 0, 0)
}

func downloadInstaller(ctx context.Context, l latestInfo) (string, error) {
	last := time.Time{}
	path, err := fetchInstaller(ctx, l, latestURL, filepath.Join(os.TempDir(), "OpenPlayer", "updates"), "OpenPlayer/"+appVersion,
		func(got, total int64) {
			if time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				pct := 0
				if total > 0 {
					pct = int(got * 100 / total)
				}
				setProgress(pct, Tf("Downloading… %.1f of %.1f MB", float64(got)/1048576, float64(max(total, got))/1048576))
			}
		})
	if err == errBadHash {
		return "", errStatus(T("The downloaded file is damaged or was changed (SHA-256 does not match). Nothing was installed."))
	}
	if err == nil {
		setProgress(100, T("Download checked. Starting the installer…"))
	}
	return path, err
}

// onUpdateProgress / onUpdateDone run on the window thread.
func onUpdateProgress() {
	updMu.Lock()
	p, t := updPct, updText
	updMu.Unlock()
	b, _ := json.Marshal(t)
	updWin.eval(fmt.Sprintf("OP.progress(%d,%s)", p, b))
}

func onUpdateDone(ok bool) {
	updMu.Lock()
	file, e := updFile, updErr
	updMu.Unlock()
	if !ok {
		b, _ := json.Marshal(T("The update could not be downloaded:") + " " + e)
		updWin.eval("OP.error(" + string(b) + ")")
		return
	}
	logf("update: starting installer %s", file)
	r, _, _ := pShellExecuteW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z("open"))), uintptr(unsafe.Pointer(utf16z(file))), 0, 0, 1)
	if r <= 32 {
		b, _ := json.Marshal(T("The installer could not be started. Please use the download page."))
		updWin.eval("OP.error(" + string(b) + ")")
		return
	}
	pPostMessageW.Call(mainWnd, wmClose, 0, 0) // the installer replaces the player's files
}
