package edge

// OpenPlayer addition (not upstream): intercept file:// navigations so that
// files dropped onto a panel are handed to the host instead of opening a
// browser window. See third_party/go-webview2/PATCHES.md.

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type genericHandlerVtbl struct {
	QueryInterface, AddRef, Release, Invoke ComProc
}

type genericHandler struct {
	vtbl *genericHandlerVtbl
	fn   func(sender, args uintptr) uintptr
}

var genericVtbl = genericHandlerVtbl{
	NewComProc(func(this *genericHandler, refiid, object uintptr) uintptr { return 0 }),
	NewComProc(func(this *genericHandler) uintptr { return 1 }),
	NewComProc(func(this *genericHandler) uintptr { return 1 }),
	NewComProc(func(this *genericHandler, sender, args uintptr) uintptr { return this.fn(sender, args) }),
}

var keepHandlers []*genericHandler // referenced by native code for the process lifetime

// addr turns a native address into a pointer (memory owned by WebView2).
func addr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func comCall(obj uintptr, index int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(addr(obj))
	fn := *(*uintptr)(addr(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func comString(obj uintptr, index int) string {
	var p *uint16
	comCall(obj, index, uintptr(unsafe.Pointer(&p)))
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// OnFileNavigation calls fn with the URI whenever the page tries to open a
// file:// URL (for example after a file is dropped onto it) and blocks it.
// Pop-up windows are always blocked.
func (e *Chromium) OnFileNavigation(fn func(uri string)) {
	if e.webview == nil {
		return
	}
	starting := &genericHandler{vtbl: &genericVtbl, fn: func(_, args uintptr) uintptr {
		uri := comString(args, 3) // ICoreWebView2NavigationStartingEventArgs::get_Uri
		if len(uri) > 5 && uri[:5] == "file:" {
			comCall(args, 8, 1) // put_Cancel(TRUE)
			fn(uri)
		}
		return 0
	}}
	popup := &genericHandler{vtbl: &genericVtbl, fn: func(_, args uintptr) uintptr {
		uri := comString(args, 3) // ICoreWebView2NewWindowRequestedEventArgs::get_Uri
		comCall(args, 6, 1)       // put_Handled(TRUE): never open a new window
		if len(uri) > 5 && uri[:5] == "file:" {
			fn(uri)
		}
		return 0
	}}
	keepHandlers = append(keepHandlers, starting, popup)
	var token _EventRegistrationToken
	_, _, _ = e.webview.vtbl.AddNavigationStarting.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(starting)), uintptr(unsafe.Pointer(&token)))
	_, _, _ = e.webview.vtbl.AddNewWindowRequested.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(popup)), uintptr(unsafe.Pointer(&token)))
}

// PutAllowExternalDrop (ICoreWebView2Controller4, WebView2 runtime 1.0.1185+)
// turns WebView2's own drag-and-drop off, so files dragged from Explorer
// reach the host window's WM_DROPFILES handler with the full list of paths.
// Returns false when the runtime is too old to support it.
func (e *Chromium) PutAllowExternalDrop(allow bool) bool {
	ctl := e.GetController()
	if ctl == nil {
		return false
	}
	iid := NewGUID("{97d418d5-a426-4e49-a151-e1a10f327d9e}") // IID_ICoreWebView2Controller4
	var c4 uintptr
	ctl.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(ctl)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&c4)))
	if c4 == 0 {
		return false
	}
	defer comCall(c4, 2) // Release
	v := uintptr(0)
	if allow {
		v = 1
	}
	return comCall(c4, 37, v) == 0 // put_AllowExternalDrop: vtable slot 37, S_OK
}
