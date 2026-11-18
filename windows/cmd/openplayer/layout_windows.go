//go:build windows

package main

// Window layout: [left panel][ video host ][right panel]. mpv is embedded in
// the video host (not the main window), so the side panels sit beside the
// video instead of covering it. mpv disables its own window, so mouse input
// arrives at the video host and is forwarded from here.

import (
	"syscall"
	"unsafe"
)

const (
	wmSize       = 0x0005
	wsChild      = 0x40000000
	wsExNoParent = 0x00000004 // WS_EX_NOPARENTNOTIFY
	panelWidthPx = 340        // at 96 DPI
)

var (
	pMoveWindow    = user32.NewProc("MoveWindow")
	pShowWindowP   = user32.NewProc("ShowWindow")
	pGetDpiForWnd  = user32.NewProc("GetDpiForWindow")
	pSetFocus      = user32.NewProc("SetFocus")
	pBeginPaint    = user32.NewProc("BeginPaint")
	pEndPaint      = user32.NewProc("EndPaint")
	hostWnd        uintptr
	hostBrush      uintptr
	hostClassName  = "OpenPlayerVideo"
	panelClassName = "OpenPlayerPanel"
	hostProcPtr    = syscall.NewCallback(hostProc)
	panelProcPtr   = syscall.NewCallback(panelProc)
)

func setHostBackground(c uint32) {
	if hostBrush != 0 {
		pDeleteObject.Call(hostBrush)
	}
	hostBrush, _, _ = pCreateSolidBrush.Call(uintptr(c))
	if hostWnd != 0 {
		pInvalidateRect.Call(hostWnd, 0, 1)
	}
}

var pInvalidateRect = user32.NewProc("InvalidateRect")

func registerChildClasses(inst uintptr) {
	for _, c := range []struct {
		name string
		proc uintptr
	}{{hostClassName, hostProcPtr}, {panelClassName, panelProcPtr}} {
		wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: c.proc, Instance: inst,
			Cursor: arrowCursor, ClassName: utf16z(c.name)}
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	}
}

func createHost(inst uintptr) uintptr {
	h, _, _ := pCreateWindowExW.Call(wsExNoParent|0x10, // + WS_EX_ACCEPTFILES
		uintptr(unsafe.Pointer(utf16z(hostClassName))), 0,
		wsChild|wsVisible|wsClipChildren, 0, 0, 100, 100, mainWnd, 0, inst, 0)
	hostWnd = h
	pDragAcceptFiles.Call(h, 1)
	return h
}

func scalePx(px int) int {
	dpi, _, _ := pGetDpiForWnd.Call(mainWnd)
	if dpi == 0 {
		dpi = 96
	}
	return px * int(dpi) / 96
}

// layout positions the video host and the side panels in the client area.
func layout() {
	if hostWnd == 0 {
		return
	}
	var r rect
	pGetClientRect.Call(mainWnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.Right), int(r.Bottom)
	left, right := 0, 0
	if !fsActive {
		if leftPanel.shown() {
			left = scalePx(panelWidthPx)
		}
		if rightPanel.shown() {
			right = scalePx(panelWidthPx)
		}
		if left+right > w-200 { // keep some video visible on narrow windows
			left, right = 0, min(right, w/2)
		}
	}
	pMoveWindow.Call(hostWnd, uintptr(left), 0, uintptr(max(w-left-right, 1)), uintptr(h), 1)
	idleView.place(left, 0, max(w-left-right, 1), h)
	leftPanel.place(0, 0, left, h)
	rightPanel.place(w-right, 0, right, h)
}

var pGetClientRect = user32.NewProc("GetClientRect")

func hostProc(hwnd, m, wp, lp uintptr) uintptr {
	switch m {
	case wmEraseBkgnd:
		if hostBrush != 0 {
			var r rect
			pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			pFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), hostBrush)
		}
		return 1
	case wmDropFiles:
		handleDrop(wp, false)
		return 0
	case wmLButtonDown, wmRButtonDown, wmMButtonDown:
		pSetFocus.Call(mainWnd) // keyboard shortcuts keep working after clicking the video
	}
	if r, ok := handleInput(hwnd, m, wp, lp); ok {
		return r
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}
