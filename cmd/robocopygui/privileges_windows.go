//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	"golang.org/x/sys/windows"
	"robocopygui/internal/core"
)

func currentProcessElevated() (bool, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	var elevated, size uint32
	if err := windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevated)), uint32(unsafe.Sizeof(elevated)), &size); err != nil {
		return false, err
	}
	if size != uint32(unsafe.Sizeof(elevated)) {
		return false, fmt.Errorf("unexpected token-elevation result size")
	}
	return elevated != 0, nil
}

func privilegeTitle(elevated bool) string {
	if elevated {
		return "RoboCopy Studio — Administrator (elevated)"
	}
	return "RoboCopy Studio — Current user (not elevated)"
}

// This is advice, not an authorization test: some non-admin accounts have
// delegated rights, and administrator membership does not grant remote access.
func privilegeHint(elevated bool, extra []string) string {
	if elevated {
		return "Administrator · Elevated — use only for work that needs these privileges. Share permissions still apply."
	}
	for _, arg := range extra {
		k := core.SwitchName(arg)
		sensitive := k == "B" || k == "ZB" || k == "COPYALL" || k == "SEC" || k == "SECFIX"
		if k == "COPY" {
			if _, bits, ok := strings.Cut(arg, ":"); ok && strings.ContainsAny(strings.ToUpper(bits), "SOU") {
				sensitive = true
			}
		}
		if sensitive {
			return "Current user · Not elevated — selected backup/security flags may need extra rights. No automatic elevation."
		}
	}
	return "Current user · Not elevated — folder/share permissions apply. Elevation is optional and explicit."
}

// Do not close the existing window on declined consent, failed launch or a
// cancelled UAC prompt. No endpoints, commands or authorizations are forwarded.
func elevationTransition(confirm func() bool, launch func() error, closeCurrent func()) error {
	if !confirm() {
		return nil
	}
	if err := launch(); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return nil
		}
		return err
	}
	closeCurrent()
	return nil
}

func shellLaunchAdministrator(owner windows.Handle) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("application path is not absolute")
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return err
	}
	// The operating system owns UAC consent/credentials. Never collect them here,
	// never run a shell, and never replay a scheduled job or arbitrary arguments.
	return windows.ShellExecute(owner, verb, file, nil, cwd, windows.SW_SHOWNORMAL)
}

func (a *app) restartAdministrator() {
	if a.elevated {
		return
	}
	if a.busy || a.exportCancel != nil {
		a.fail(fmt.Errorf("finish the current copy/preview or export before restarting as administrator"))
		return
	}
	err := elevationTransition(func() bool {
		return themedMsgBox(a.mw, "Restart as administrator?", "Use elevation only for protected local paths or options requiring additional privileges. It does not bypass network-share permissions.\n\nWindows will request consent or an administrator's credentials. If you cannot provide them, continue using this app with your existing permissions.\n\nThe current window closes only after Windows accepts the launch. The new window starts with a BLANK FORM: save a profile first if needed. No copy, preview approval or job is automatically replayed. If you use another administrator's credentials, profiles/history and schedules belong to that account; mapped drives and network access may differ. New schedules still use normal-user permissions.\n\nRestart as administrator now?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) == walk.DlgCmdYes
	}, func() error { return shellLaunchAdministrator(windows.Handle(a.mw.Handle())) }, func() { a.mw.Close() })
	if err != nil {
		a.fail(fmt.Errorf("administrator restart failed; current window is unchanged: %w", err))
	}
}
