//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"unicode/utf16"
	"unsafe"
)

var multiByteToWideChar = windows.NewLazySystemDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// Robocopy /LOG uses the system ANSI code page, not UTF-8 or UTF-16.
func decodeANSI(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	n, _, _ := multiByteToWideChar.Call(0, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b)
	}
	u := make([]uint16, n)
	multiByteToWideChar.Call(0, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&u[0])), n)
	return string(utf16.Decode(u))
}
