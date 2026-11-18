//go:build windows

package main

// Thumbnails from Windows itself (the ones File Explorer shows): video frames
// and album covers, through IShellItemImageFactory. No second copy of the
// playback engine is needed.

import (
	"syscall"
	"unsafe"
)

var (
	ole32                        = syscall.NewLazyDLL("ole32.dll")
	pCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	pSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	pGetObjectW                  = gdi32.NewProc("GetObjectW")
	pGetDIBits                   = gdi32.NewProc("GetDIBits")
	pCreateCompatibleDC          = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC                    = gdi32.NewProc("DeleteDC")
)

type comGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

// IID_IShellItemImageFactory {bcc18b79-ba16-442f-80c4-8a59c30c463b}
var iidShellItemImageFactory = comGUID{0xbcc18b79, 0xba16, 0x442f, [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

type bitmapObj struct { // BITMAP
	Type, Width, Height, WidthBytes int32
	Planes, BitsPixel               uint16
	Bits                            uintptr
}

type bitmapInfo struct { // BITMAPINFO with one (unused) colour entry
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPelsPerM, YPelsPerM   int32
	ClrUsed, ClrImportant  uint32
	Colors                 [1]uint32
}

func comMethod(obj uintptr, index int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(ptrAt(obj))
	fn := *(*uintptr)(ptrAt(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

// shellThumb returns a JPEG (at most 320x180) or nil when Windows has no
// thumbnail for the file. Must run on a thread with COM initialised.
func shellThumb(path string) []byte {
	var fac uintptr
	hr, _, _ := pSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(utf16z(path))), 0,
		uintptr(unsafe.Pointer(&iidShellItemImageFactory)), uintptr(unsafe.Pointer(&fac)))
	if int32(hr) < 0 || fac == 0 {
		return nil
	}
	defer comMethod(fac, 2) // Release
	var hbm uintptr
	size := uintptr(320) | uintptr(180)<<32 // SIZE{320,180}, passed by value
	// GetImage (slot 3), SIIGBF_THUMBNAILONLY: no generic file icons.
	if r := comMethod(fac, 3, size, 0x8, uintptr(unsafe.Pointer(&hbm))); int32(r) < 0 || hbm == 0 {
		return nil
	}
	defer pDeleteObject.Call(hbm)
	var bm bitmapObj
	if r, _, _ := pGetObjectW.Call(hbm, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil
	}
	w, h := int(bm.Width), int(bm.Height)
	if h < 0 {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil
	}
	bi := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32} // top-down BGRA
	px := make([]byte, w*h*4)
	dc, _, _ := pCreateCompatibleDC.Call(0)
	n, _, _ := pGetDIBits.Call(dc, hbm, 0, uintptr(h), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&bi)), 0)
	pDeleteDC.Call(dc)
	if n == 0 {
		return nil
	}
	return encodeThumb(px, w, h)
}
