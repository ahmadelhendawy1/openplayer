//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A small diagnostic log at %LOCALAPPDATA%\OpenPlayer\openplayer.log
// (restarted when it grows past 512 KB). No personal data is written.
var (
	logMu   sync.Mutex
	logFile *os.File
)

func openLog() {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "openplayer.log")
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(p); err == nil && st.Size() > 512<<10 {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(p, flag, 0o644)
	if err != nil {
		return
	}
	logFile = f
	log.SetOutput(f) // the WebView2 library reports its errors through package log
	logf("---- OpenPlayer %s started", appVersion)
}

func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		fmt.Fprintf(logFile, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, args...))
	}
}
