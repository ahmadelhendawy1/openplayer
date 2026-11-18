package main

import "unsafe"

// ptrAt turns a native address into a pointer (memory owned by libmpv/Windows).
func ptrAt(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// cstrMax guards against a missing terminator. It must be far above any real
// value: the playlist of a few hundred songs with long Arabic paths is
// hundreds of kilobytes. (0.9.4 and earlier stopped at 1 KB, which cut the
// playlist off after a handful of songs.)
const cstrMax = 64 << 20

// cstr copies a NUL-terminated C string.
func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptrAt(p)
	n := 0
	for n < cstrMax && *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n))
}
