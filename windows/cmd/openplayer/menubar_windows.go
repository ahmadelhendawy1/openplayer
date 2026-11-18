//go:build windows

package main

// Dark, themed menu bar. Windows draws the menu bar in light colors only;
// the undocumented WM_UAHDRAWMENU / WM_UAHDRAWMENUITEM messages (used by
// Notepad++, Explorer and others) let us paint it ourselves. If they are
// never sent, Windows simply draws its normal light bar.

import (
	"syscall"
	"unsafe"
)

const (
	wmNcPaint         = 0x0085
	wmNcActivate      = 0x0086
	wmUahDrawMenu     = 0x0091
	wmUahDrawMenuItem = 0x0092
	objidMenu         = ^uintptr(2) // -3
	miimString        = 0x40
	odsSelected       = 0x0001
	odsGrayed         = 0x0002
	odsDisabled       = 0x0004
	odsHotlight       = 0x0040
	odsNoAccel        = 0x0100
	dtCenter          = 0x0001
	dtVCenter         = 0x0004
	dtSingleLine      = 0x0020
	dtHidePrefix      = 0x00100000
	bkTransparent     = 1
)

var (
	gdi32             = syscall.NewLazyDLL("gdi32.dll")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pFillRect         = user32.NewProc("FillRect")
	pDrawTextW        = user32.NewProc("DrawTextW")
	pGetMenuBarInfo   = user32.NewProc("GetMenuBarInfo")
	pGetWindowRect    = user32.NewProc("GetWindowRect")
	pGetWindowDC      = user32.NewProc("GetWindowDC")
	pReleaseDC        = user32.NewProc("ReleaseDC")
	pGetMenuItemInfoW = user32.NewProc("GetMenuItemInfoW")
	pDrawMenuBar      = user32.NewProc("DrawMenuBar")

	barBrush, hotBrush uintptr
	barText            uint32
)

type menuBarInfo struct {
	Size     uint32
	Bar      rect
	Menu     uintptr
	HwndMenu uintptr
	Flags    uint32
}

type menuItemInfo struct {
	Size, Mask, Type, State, ID uint32
	SubMenu, Checked, Unchecked uintptr
	ItemData                    uintptr
	TypeData                    *uint16
	Cch                         uint32
	BmpItem                     uintptr
}

// uahDrawMenuItem mirrors UAHDRAWMENUITEM: DRAWITEMSTRUCT, UAHMENU, then
// UAHMENUITEM whose first field is the item position.
type uahDrawMenuItem struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	HwndItem                                      uintptr
	HDC                                           uintptr
	RcItem                                        rect
	ItemData                                      uintptr
	UMenu                                         uintptr // UAHMENU.hmenu
	UHdc                                          uintptr
	UFlags                                        uint32
	_                                             uint32 // UAHMENU is padded to 24 bytes
	Position                                      int32  // UAHMENUITEM.iPosition
}

type uahMenu struct {
	Menu  uintptr
	HDC   uintptr
	Flags uint32
}

func setMenuBarColors(bg, hot, text uint32) {
	if barBrush != 0 {
		pDeleteObject.Call(barBrush)
		pDeleteObject.Call(hotBrush)
	}
	barBrush, _, _ = pCreateSolidBrush.Call(uintptr(bg))
	hotBrush, _, _ = pCreateSolidBrush.Call(uintptr(hot))
	barText = text
	if mainWnd != 0 {
		pDrawMenuBar.Call(mainWnd)
	}
}

// menuBarRect returns the menu bar rectangle in window coordinates.
func menuBarRect(hwnd uintptr) (rect, bool) {
	mbi := menuBarInfo{Size: uint32(unsafe.Sizeof(menuBarInfo{}))}
	if r, _, _ := pGetMenuBarInfo.Call(hwnd, objidMenu, 0, uintptr(unsafe.Pointer(&mbi))); r == 0 {
		return rect{}, false
	}
	var wr rect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	b := mbi.Bar
	b.Left -= wr.Left
	b.Right -= wr.Left
	b.Top -= wr.Top
	b.Bottom -= wr.Top
	return b, true
}

// handleMenuBarMsg paints the bar; returns handled=true if the message was consumed.
func handleMenuBarMsg(hwnd, m, wp, lp uintptr) (uintptr, bool) {
	if barBrush == 0 {
		return 0, false
	}
	switch m {
	case wmUahDrawMenu:
		um := (*uahMenu)(ptrAt(lp))
		if b, ok := menuBarRect(hwnd); ok {
			pFillRect.Call(um.HDC, uintptr(unsafe.Pointer(&b)), barBrush)
		}
		return 1, true
	case wmUahDrawMenuItem:
		it := (*uahDrawMenuItem)(ptrAt(lp))
		buf := make([]uint16, 256)
		mii := menuItemInfo{Mask: miimString, TypeData: &buf[0], Cch: uint32(len(buf) - 1)}
		mii.Size = uint32(unsafe.Sizeof(mii))
		pGetMenuItemInfoW.Call(it.UMenu, uintptr(it.Position), 1, uintptr(unsafe.Pointer(&mii)))
		brush := barBrush
		if it.ItemState&(odsHotlight|odsSelected) != 0 {
			brush = hotBrush
		}
		pFillRect.Call(it.HDC, uintptr(unsafe.Pointer(&it.RcItem)), brush)
		flags := uintptr(dtCenter | dtSingleLine | dtVCenter)
		if it.ItemState&odsNoAccel != 0 {
			flags |= dtHidePrefix
		}
		col := barText
		if it.ItemState&(odsGrayed|odsDisabled) != 0 {
			col = (barText & 0x00FEFEFE) >> 1
		}
		pSetBkMode.Call(it.HDC, bkTransparent)
		pSetTextColor.Call(it.HDC, uintptr(col))
		pDrawTextW.Call(it.HDC, uintptr(unsafe.Pointer(&buf[0])), ^uintptr(0), uintptr(unsafe.Pointer(&it.RcItem)), flags)
		return 1, true
	case wmNcPaint, wmNcActivate:
		r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
		paintMenuLine(hwnd)
		return r, true
	}
	return 0, false
}

// paintMenuLine covers the 1px light line Windows draws under the menu bar.
func paintMenuLine(hwnd uintptr) {
	b, ok := menuBarRect(hwnd)
	if !ok {
		return
	}
	line := rect{b.Left, b.Bottom, b.Right, b.Bottom + 1}
	dc, _, _ := pGetWindowDC.Call(hwnd)
	if dc == 0 {
		return
	}
	pFillRect.Call(dc, uintptr(unsafe.Pointer(&line)), barBrush)
	pReleaseDC.Call(hwnd, dc)
}

func init() {
	if unsafe.Offsetof(uahDrawMenuItem{}.Position) != 88 {
		panic("uahDrawMenuItem layout")
	}
}
