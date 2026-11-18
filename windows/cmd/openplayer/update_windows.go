//go:build windows

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"openplayer/internal/updater"
)

// Set at build time together with pubKey:
//
//	-X main.updateURL=https://github.com/<owner>/<repo>/releases/latest/download
var updateURL string

const wmAppNotice = 0x8004

var (
	updateRunning atomic.Bool
	noticeMu      sync.Mutex
	noticeText    string
)

func updatesConfigured() bool { return updateURL != "" && releaseKey() != nil }

func releaseKey() ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}

func postNotice(text string) {
	noticeMu.Lock()
	noticeText = text
	noticeMu.Unlock()
	pPostMessageW.Call(mainWnd, wmAppNotice, 0, 0)
}

func showNotice() {
	noticeMu.Lock()
	t := noticeText
	noticeMu.Unlock()
	messageInfo(T("OpenPlayer updates"), t)
}

// maybeAutoUpdate runs a background check at most once a day when enabled.
func maybeAutoUpdate() {
	if !cfg.flag("auto_update", true) || !updatesConfigured() {
		return
	}
	last, _ := strconv.ParseInt(cfg.get("last_update_check", "0"), 10, 64)
	if time.Since(time.Unix(last, 0)) < 24*time.Hour {
		return
	}
	startUpdateCheck(false)
}

// startUpdateCheck checks for new engine/skin components and stages them.
// They are installed the next time OpenPlayer starts (the engine DLL is in use).
func startUpdateCheck(interactive bool) {
	if interactive { // Help > Check for Updates: is there a newer OpenPlayer?
		go checkAppVersion(true)
	}
	if !updatesConfigured() {
		return
	}
	if !updateRunning.CompareAndSwap(false, true) {
		if interactive {
			messageInfo(T("OpenPlayer updates"), T("An update check is already running."))
		}
		return
	}
	go func() {
		defer updateRunning.Store(false)
		text, staged := checkAndStage()
		_ = cfg.set("last_update_check", strconv.FormatInt(time.Now().Unix(), 10))
		switch {
		case interactive:
			postNotice(text)
		case staged:
			mpv.Async(mp, "show-text", T("OpenPlayer: component update downloaded, it installs on next start"), "6000")
		}
	}()
}

func checkAndStage() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	u := &updater.Updater{Root: appRoot, PubKey: releaseKey(), Src: updater.HTTPSource{Base: updateURL, Flat: true}}
	plan, err := u.Check(ctx)
	switch {
	case errors.Is(err, updater.ErrDowngrade):
		return T("Your components are newer than the published release."), false
	case errors.Is(err, updater.ErrNeedUpdater):
		return T("A new OpenPlayer version is required for the latest components. Please download the new installer."), false
	case err != nil:
		return T("Could not check for updates:") + "\n" + err.Error(), false
	case plan.UpToDate:
		return Tf("Everything is up to date.\n\nComponents: %s", plan.Manifest.EngineVersion), false
	}
	if err := u.Download(ctx, plan); err != nil {
		return T("The update could not be downloaded:") + "\n" + err.Error(), false
	}
	return Tf("Update %s downloaded and verified (%d file(s)).\n\nIt will be installed the next time you start OpenPlayer.",
		plan.Manifest.EngineVersion, len(plan.Fetch)), true
}

// applyStagedUpdate runs before the engine DLL is loaded. Failures are not
// fatal: the current components keep working and the update stays staged.
func applyStagedUpdate() {
	key := releaseKey()
	if key == nil {
		return
	}
	u := &updater.Updater{Root: appRoot, PubKey: key}
	_, _ = u.Apply()
}
