//go:build windows

package main

import (
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

type messageChoice struct {
	text   string
	result int
}

// Confirmations deliberately default to the non-destructive answer, including
// older call sites which did not specify MB_DEFBUTTON2. Escape and caption X
// never grant permission. Keep standard result IDs for existing callers.
func messageChoices(style walk.MsgBoxStyle) ([]messageChoice, int) {
	switch style & 15 {
	case walk.MsgBoxYesNo:
		return []messageChoice{{"Yes", walk.DlgCmdYes}, {"No", walk.DlgCmdNo}}, 1
	case walk.MsgBoxYesNoCancel:
		return []messageChoice{{"Yes", walk.DlgCmdYes}, {"No", walk.DlgCmdNo}, {"Cancel", walk.DlgCmdCancel}}, 2
	case walk.MsgBoxOKCancel:
		return []messageChoice{{"OK", walk.DlgCmdOK}, {"Cancel", walk.DlgCmdCancel}}, 1
	default:
		return []messageChoice{{"OK", walk.DlgCmdOK}}, 0
	}
}

// createMessageDialog is separate from Run so native tests can inspect colors,
// default/cancel actions and disposal without blocking in a modal message loop.
func createMessageDialog(owner walk.Form, title, message string, style walk.MsgBoxStyle) (*walk.Dialog, int, error) {
	var d *walk.Dialog
	var safeButton *walk.PushButton
	var heading *walk.TextLabel
	var icon walk.Image
	if owner != nil {
		icon = owner.Icon()
	}
	choices, safe := messageChoices(style)
	title = strings.ReplaceAll(title, "\x00", "␀")
	message = strings.ReplaceAll(message, "\x00", "␀")
	accent := walk.RGB(38, 188, 239)
	role := "INFORMATION"
	switch style & 0xf0 {
	case walk.MsgBoxIconError:
		accent, role = walk.RGB(255, 133, 133), "CANNOT CONTINUE"
	case walk.MsgBoxIconWarning, walk.MsgBoxIconQuestion:
		accent, role = walk.RGB(255, 189, 114), "PLEASE REVIEW"
	}
	row := []Widget{HSpacer{}}
	for i, choice := range choices {
		choice := choice
		b := PushButton{Text: choice.text, MinSize: Size{Width: 104, Height: 36}, OnClicked: func() { d.Close(choice.result) }}
		if i == safe {
			b.AssignTo = &safeButton
		}
		row = append(row, b)
	}
	height := 290
	if len(message) > 320 || strings.Count(message, "\n") > 4 {
		height = 410
	}
	err := (Dialog{
		AssignTo: &d, Name: "StudioMessageDialog", Title: title, Icon: icon,
		DefaultButton: &safeButton, CancelButton: &safeButton,
		Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)},
		Font:       Font{Family: "Segoe UI", PointSize: 10},
		MinSize:    Size{Width: 700, Height: height}, Size: Size{Width: 700, Height: height},
		Layout: VBox{Margins: Margins{Left: 22, Top: 18, Right: 22, Bottom: 18}, Spacing: 14},
		Children: []Widget{
			TextLabel{AssignTo: &heading, Text: role, Font: Font{Family: "Segoe UI Semibold", PointSize: 12}},
			TextEdit{Name: "MessageBody", Text: strings.ReplaceAll(strings.ReplaceAll(message, "\r\n", "\n"), "\n", "\r\n"), ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 100}, Accessibility: Accessibility{Name: title + " message"}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 10}, Children: row},
		},
	}).Create(owner)
	if err != nil {
		if d != nil {
			d.Dispose()
		}
		return nil, choices[safe].result, err
	}
	darken(d)
	darkTitleBar(d.Handle())
	heading.SetTextColor(accent)
	if painted, ok := paintedButtons[safeButton.Handle()]; ok {
		painted.primary = true
		paintedButtons[safeButton.Handle()] = painted
		safeButton.Invalidate()
	}
	// Walk applies its initial asynchronous layout after Starting and may
	// choose the first tab candidate again. Put the safe button first in native
	// tab/z-order without changing visual layout, then focus it on the queue.
	win.SetWindowPos(safeButton.Parent().Handle(), win.HWND_TOP, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	win.SetWindowPos(safeButton.Handle(), win.HWND_TOP, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	d.Starting().Attach(func() {
		safeButton.SetFocus()
		d.Synchronize(func() { safeButton.SetFocus() })
	})
	return d, choices[safe].result, nil
}

func themedMsgBox(owner walk.Form, title, message string, style walk.MsgBoxStyle) int {
	d, safe, err := createMessageDialog(owner, title, message, style)
	if err != nil {
		// A broken UI must not silently approve an operation. Native fallback is
		// reserved for failed dialog creation (e.g. exhausted desktop resources).
		style = (style &^ (walk.MsgBoxDefButton2 | walk.MsgBoxDefButton3 | walk.MsgBoxDefButton4))
		if style&15 == walk.MsgBoxYesNo || style&15 == walk.MsgBoxOKCancel {
			style |= walk.MsgBoxDefButton2
		}
		if style&15 == walk.MsgBoxYesNoCancel {
			style |= walk.MsgBoxDefButton3
		}
		return walk.MsgBox(owner, title, message, style)
	}
	defer d.Dispose()
	result := d.Run()
	if result == walk.DlgCmdCancel || result == 0 {
		return safe
	}
	return result
}
