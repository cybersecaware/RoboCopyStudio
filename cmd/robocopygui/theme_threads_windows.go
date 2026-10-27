//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"robocopygui/internal/core"
)

func (a *app) updateThreadControls(lock bool) {
	if a.threads == nil || a.threadState == nil || a.useThreads == nil || a.expert == nil {
		return
	}
	expert := a.expert.Checked()
	args, parseErr := core.SplitCommandLine(a.raw.Text())
	n, raw, err := core.RawThreads(args)
	a.threaded.SetEnabled(!lock && !expert && !raw)
	a.threads.SetEnabled(!lock && !expert && !raw && a.threaded.Checked())
	a.useThreads.SetVisible(raw && !expert)
	a.useThreads.SetEnabled(!lock && parseErr == nil && err == nil && !expert)
	switch {
	case expert:
		a.threadState.SetText("Expert mode — raw full command controls MT; thread selector is inactive.")
	case raw && err != nil:
		a.threadState.SetText("Invalid raw /MT — use an integer 1–128, or remove it to enable the selector.")
	case raw:
		a.threadState.SetText(fmt.Sprintf("Raw /MT:%d overrides selector (last MT wins; duplicates canonicalized). Use selector to adopt/edit.", n))
	case a.threaded.Checked():
		a.threadState.SetText(fmt.Sprintf("/MT:%d — aggregate I/O activity; not copied bytes. Per-file percentages are never attributed.", int(a.threads.Value())))
	default:
		a.threadState.SetText("MT off — single-threaded telemetry available with a preview.")
	}
}

func (a *app) adoptRawThreads() {
	if a.busy || a.expert.Checked() {
		return
	}
	args, err := core.SplitCommandLine(a.raw.Text())
	if err != nil {
		a.fail(err)
		return
	}
	n, present, err := core.RawThreads(args)
	if err != nil {
		a.fail(err)
		return
	}
	if !present {
		return
	}
	a.initializing = true
	a.threads.SetValue(float64(n))
	a.threaded.SetChecked(true)
	a.raw.SetText(core.Command(withoutFlag(args, "MT")))
	a.initializing = false
	a.invalidate()
	a.threads.SetFocus()
}

// darkTitleBar requests a per-process/per-window dark native non-client frame.
var themeDLL = windows.NewLazySystemDLL("uxtheme.dll")
var getThemeProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcAddress")
var dwmAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
var compositionAttribute = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowCompositionAttribute")

// No registry/global theme changes. All optional APIs are capability checked.
// uxtheme ordinal 135 changed signature in 18362: never call it on 17763.
func darkTitleBar(hwnd win.HWND) {
	version := windows.RtlGetVersion()
	if version.MajorVersion < 10 || version.BuildNumber < 17763 {
		return
	}
	ux := themeDLL
	getProc := getThemeProc
	if version.BuildNumber >= 18362 {
		if ux.Load() == nil {
			if p, _, _ := getProc.Call(ux.Handle(), 135); p != 0 {
				syscall.SyscallN(p, 2) // SetPreferredAppMode(ForceDark)
			}
		}
	}
	if ux.Load() == nil {
		if p, _, _ := getProc.Call(ux.Handle(), 133); p != 0 {
			syscall.SyscallN(p, uintptr(hwnd), 1) // AllowDarkModeForWindow
		}
	}
	enabled := int32(1)
	dwm := dwmAttribute
	if dwm.Find() == nil {
		hr, _, _ := dwm.Call(uintptr(hwnd), 20, uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
		if int32(hr) < 0 {
			dwm.Call(uintptr(hwnd), 19, uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
		}
	}
	// Windows 10 1903/1909 can require the composition flag as well as DWM 19.
	if version.BuildNumber >= 18362 && version.BuildNumber < 19041 {
		type compositionData struct {
			Attribute uint32
			Data      unsafe.Pointer
			Size      uintptr
		}
		data := compositionData{Attribute: 26, Data: unsafe.Pointer(&enabled), Size: unsafe.Sizeof(enabled)}
		p := compositionAttribute
		if p.Find() == nil {
			p.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&data)))
		}
	}
	theme, _ := syscall.UTF16PtrFromString("DarkMode_Explorer")
	win.SetWindowTheme(hwnd, theme, nil)
	win.SetWindowPos(hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_FRAME|win.RDW_UPDATENOW)
}

// Ensure NumberEdit keeps the same dark text role as the other native edits.
func darkNumberEdit(v *walk.NumberEdit) {
	b, _ := walk.NewSolidColorBrush(walk.RGB(14, 17, 24))
	v.SetBackground(b)
	v.SetTextColor(walk.RGB(224, 231, 242))
}
