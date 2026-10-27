//go:build windows

package main

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
)

func TestMessageDialogVisualFixture(t *testing.T) {
	if os.Getenv("ROBOCOPY_GUI_VISUAL_FIXTURE") != "1" {
		t.Skip("opt-in desktop screenshot fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	owner.SetTitle("Studio theme fixture owner")
	if err := owner.SetLayout(walk.NewVBoxLayout()); err != nil {
		t.Fatal(err)
	}
	owner.SetSize(walk.Size{Width: 900, Height: 600})
	owner.Show()
	d, _, err := createMessageDialog(owner, "Confirm destructive / persistent side effects", "Source: C:\\example source\nDestination: D:\\example target\n\nrobocopy.exe \"C:\\example source\" \"D:\\example target\" /MIR /Z /E /R:2 /W:2 /XJ /COPY:DAT /DCOPY:DAT\n\nMIR/PURGE delete destination data; MOV/MOVE delete source data. Files may change since preview. Continue?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Dispose()
	go func() { time.Sleep(45 * time.Second); d.Synchronize(func() { d.Close(walk.DlgCmdNo) }) }()
	if got := d.Run(); got != walk.DlgCmdNo {
		t.Errorf("warning must decline in fixture, got %d", got)
	}
}

func TestMessageChoicesAlwaysDefaultSafe(t *testing.T) {
	for _, tc := range []struct {
		style   walk.MsgBoxStyle
		n, want int
	}{
		{walk.MsgBoxYesNo | walk.MsgBoxIconWarning, 2, walk.DlgCmdNo},
		{walk.MsgBoxYesNo | walk.MsgBoxDefButton1, 2, walk.DlgCmdNo},
		{walk.MsgBoxYesNo | walk.MsgBoxDefButton2, 2, walk.DlgCmdNo},
		{walk.MsgBoxYesNoCancel, 3, walk.DlgCmdCancel},
		{walk.MsgBoxOKCancel, 2, walk.DlgCmdCancel},
		{walk.MsgBoxIconError, 1, walk.DlgCmdOK},
	} {
		choices, safe := messageChoices(tc.style)
		if len(choices) != tc.n || choices[safe].result != tc.want {
			t.Errorf("style %d: choices=%v safe=%d", tc.style, choices, safe)
		}
	}
}

func TestMessageDialogThemedSafeAndDisposed(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	before := len(paintedButtons)
	for _, style := range []walk.MsgBoxStyle{walk.MsgBoxIconError, walk.MsgBoxYesNo | walk.MsgBoxIconWarning, walk.MsgBoxYesNoCancel, walk.MsgBoxOKCancel} {
		d, safe, err := createMessageDialog(owner, "Review & confirm", "Source: C:\\example\nDestination: D:\\example\n\nMIR deletes extras. Continue?", style)
		if err != nil {
			t.Fatal(err)
		}
		if d.DefaultButton() == nil || d.DefaultButton() != d.CancelButton() {
			t.Error("default and Escape must use the same safe button")
		}
		if style&15 == walk.MsgBoxYesNo && (safe != walk.DlgCmdNo || d.DefaultButton().Text() != "No") {
			t.Error("destructive dialog does not default No")
		}
		if brush, ok := d.Background().(*walk.SolidColorBrush); !ok || brush.Color() != walk.RGB(27, 31, 43) {
			t.Error("dialog background does not match studio")
		}
		d.Dispose()
		if len(paintedButtons) != before {
			t.Fatalf("dialog retained painted buttons: before=%d after=%d", before, len(paintedButtons))
		}
	}
}
