//go:build windows

package main

// "Make OpenPlayer the default player". Windows 10/11 do not allow any program
// to make itself the default silently (the choice is protected so installers
// cannot hijack it). The supported way is to open Windows' own Default apps
// page for OpenPlayer, where the user confirms with one click.

import (
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const wmAppAskDefault = 0x800B

var pAssocQueryStringW = syscall.NewLazyDLL("shlwapi.dll").NewProc("AssocQueryStringW")

// defaultExe returns the program Windows opens for an extension like ".mp4".
func defaultExe(ext string) string {
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := pAssocQueryStringW.Call(0x400 /*ASSOCF_INIT_IGNOREUNKNOWN*/, 2, /*ASSOCSTR_EXECUTABLE*/
		uintptr(unsafe.Pointer(utf16z(ext))), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func isDefaultPlayer() bool {
	self, _ := os.Executable()
	for _, ext := range []string{".mp4", ".mkv", ".mp3"} {
		if !strings.EqualFold(defaultExe(ext), self) {
			return false
		}
	}
	return true
}

func registeredWithWindows() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\RegisteredApplications`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("OpenPlayer")
	return err == nil
}

func openDefaultAppsPage() {
	pShellExecuteW.Call(mainWnd, uintptr(unsafe.Pointer(utf16z("open"))),
		uintptr(unsafe.Pointer(utf16z("ms-settings:defaultapps?registeredAppUser=OpenPlayer"))), 0, 0, 1)
}

func makeDefaultCommand() {
	if !registeredWithWindows() {
		messageInfo(T("Default player"), T("OpenPlayer is not registered for audio and video files yet.\n\nRun the OpenPlayer installer again and keep \"Audio and video file types\" selected."))
		return
	}
	if isDefaultPlayer() {
		messageInfo(T("Default player"), T("OpenPlayer is already your default player for video and audio."))
		return
	}
	openDefaultAppsPage()
}

// scheduleDefaultQuestion asks once, a few seconds after the first start.
func scheduleDefaultQuestion() {
	if cfg.flag("asked_default", false) || !registeredWithWindows() || isDefaultPlayer() {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)
		pPostMessageW.Call(mainWnd, wmAppAskDefault, 0, 0)
	}()
}

func askDefault() {
	_ = cfg.set("asked_default", "yes")
	r, _, _ := pMessageBoxW.Call(mainWnd,
		uintptr(unsafe.Pointer(utf16z(T("Make OpenPlayer your default player for video and audio files?\n\nWindows will open its Default apps page. Click \"Set default\" there to confirm.\n\nYou can do this later from Tools > Make OpenPlayer the Default Player.")))),
		uintptr(unsafe.Pointer(utf16z("OpenPlayer"))), 0x4|0x20) // MB_YESNO | MB_ICONQUESTION
	if r == 6 { // IDYES
		openDefaultAppsPage()
	}
}
