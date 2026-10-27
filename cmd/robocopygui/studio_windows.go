//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
	"robocopygui/internal/core"
)

func metric(title string, target **walk.Label, value string) Widget {
	// The spacer makes the card horizontally greedy: labels alone leave a
	// Composite content-sized and centered inside its grid cell in Walk.
	return Composite{StretchFactor: 1, Background: SolidColorBrush{Color: walk.RGB(20, 24, 34)}, MinSize: Size{Width: 210, Height: 104}, MaxSize: Size{Height: 104}, Layout: VBox{Margins: Margins{Left: 14, Top: 10, Right: 14, Bottom: 10}, Spacing: 6}, Children: []Widget{
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			Label{Text: title, EllipsisMode: EllipsisEnd, Font: Font{Family: "Segoe UI", PointSize: 10}}, HSpacer{},
		}},
		Label{AssignTo: target, Text: value, MinSize: Size{Height: 56}, Font: Font{Family: "Consolas", PointSize: 14, Bold: true}},
	}}
}

// Keep real Win32 Button HWNDs and their default input/accessibility behavior;
// replace only their painting so automation, focus and keyboard still work.
type buttonPaint struct {
	prev           uintptr
	w              *walk.Button
	check, primary bool
}

var createBrushProc = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateSolidBrush")
var fillRectProc = windows.NewLazySystemDLL("user32.dll").NewProc("FillRect")
var drawTextProc = windows.NewLazySystemDLL("user32.dll").NewProc("DrawTextW")
var createPenProc = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreatePen")
var paintedButtons = map[win.HWND]buttonPaint{}
var paintProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	b, ok := paintedButtons[hwnd]
	if !ok {
		return win.DefWindowProc(hwnd, msg, wp, lp)
	}
	switch msg {
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		dc := win.BeginPaint(hwnd, &ps)
		defer win.EndPaint(hwnd, &ps)
		var r win.RECT
		win.GetClientRect(hwnd, &r)
		bg := walk.RGB(30, 36, 50)
		fg := walk.RGB(220, 227, 238)
		if b.primary {
			bg = walk.RGB(0, 137, 193)
			fg = walk.RGB(255, 255, 255)
		}
		if b.check {
			bg = walk.RGB(27, 31, 43)
		}
		if !win.IsWindowEnabled(hwnd) {
			fg = walk.RGB(104, 116, 137)
		}
		brush, _, _ := createBrushProc.Call(uintptr(bg))
		fillRectProc.Call(uintptr(dc), uintptr(unsafe.Pointer(&r)), brush)
		win.DeleteObject(win.HGDIOBJ(brush))
		win.SetBkMode(dc, win.TRANSPARENT)
		win.SetTextColor(dc, win.COLORREF(fg))
		font := win.HGDIOBJ(win.SendMessage(hwnd, win.WM_GETFONT, 0, 0))
		old := win.SelectObject(dc, font)
		defer win.SelectObject(dc, old)
		text := b.w.Text()
		focus := r
		flags := uint32(win.DT_CENTER | win.DT_VCENTER | win.DT_SINGLELINE | win.DT_NOPREFIX)
		if b.check {
			// Vector indicator: independent of Segoe UI glyph metrics, and all
			// geometry follows the control's current DPI.
			scale := func(n int) int32 { return int32(b.w.IntFrom96DPI(n)) }
			size := scale(13)
			x, y := r.Left+scale(4), (r.Bottom-size)/2
			color := fg
			if b.w.Checked() && win.IsWindowEnabled(hwnd) {
				color = walk.RGB(38, 188, 239)
			}
			pen, _, _ := createPenProc.Call(win.PS_SOLID, uintptr(scale(1)), uintptr(color))
			oldPen := win.SelectObject(dc, win.HGDIOBJ(pen))
			oldBrush := win.SelectObject(dc, win.GetStockObject(win.NULL_BRUSH))
			win.Rectangle_(dc, x, y, x+size, y+size)
			win.SelectObject(dc, oldPen)
			win.DeleteObject(win.HGDIOBJ(pen))
			if b.w.Checked() {
				pen, _, _ = createPenProc.Call(win.PS_SOLID, uintptr(scale(2)), uintptr(color))
				oldPen = win.SelectObject(dc, win.HGDIOBJ(pen))
				win.MoveToEx(dc, int(x+scale(3)), int(y+scale(6)), nil)
				win.LineTo(dc, x+scale(5), y+scale(9))
				win.LineTo(dc, x+scale(10), y+scale(3))
				win.SelectObject(dc, oldPen)
				win.DeleteObject(win.HGDIOBJ(pen))
			}
			win.SelectObject(dc, oldBrush)
			focus = win.RECT{Left: x - scale(2), Top: y - scale(2), Right: x + size + scale(2), Bottom: y + size + scale(2)}
			flags = win.DT_LEFT | win.DT_VCENTER | win.DT_SINGLELINE | win.DT_NOPREFIX
			r.Left += scale(24)
			r.Right -= scale(4)
		}
		t, _ := syscall.UTF16PtrFromString(text)
		drawTextProc.Call(uintptr(dc), uintptr(unsafe.Pointer(t)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags))
		if win.GetFocus() == hwnd && win.SendMessage(hwnd, win.WM_QUERYUISTATE, 0, 0)&2 == 0 {
			if !b.check {
				focus.Left += 2
				focus.Top += 2
				focus.Right -= 2
				focus.Bottom -= 2
			}
			win.DrawFocusRect(dc, &focus)
		}
		return 0
	case win.WM_DESTROY, win.WM_NCDESTROY:
		// Walk restores its native WNDPROC during WM_DESTROY, so our
		// subclass may never receive WM_NCDESTROY. Release Go references now.
		delete(paintedButtons, hwnd)
		return win.CallWindowProc(b.prev, hwnd, msg, wp, lp)
	}
	result := win.CallWindowProc(b.prev, hwnd, msg, wp, lp)
	if msg == win.BM_SETCHECK || msg == win.BM_SETSTATE || msg == win.WM_ENABLE || msg == win.WM_SETFOCUS || msg == win.WM_KILLFOCUS || msg == win.WM_LBUTTONDOWN || msg == win.WM_LBUTTONUP || msg == win.WM_KEYDOWN || msg == win.WM_KEYUP || msg == win.WM_UPDATEUISTATE {
		// Native Button focus/check handlers can draw synchronously. Repaint
		// immediately, not one message later, so their caption border cannot linger.
		win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW)
	}
	return result
})

func detachPaintButton(hwnd win.HWND) {
	if b, ok := paintedButtons[hwnd]; ok {
		// Explicit Dispose publishes before DestroyWindow. Restore Walk's
		// chain before deleting the entry, so teardown still reaches Walk.
		if win.GetWindowLongPtr(hwnd, win.GWLP_WNDPROC) == paintProc {
			win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, b.prev)
		}
		delete(paintedButtons, hwnd)
	}
}

func paintButton(w *walk.Button, check, primary bool) {
	hwnd := w.Handle()
	if _, ok := paintedButtons[hwnd]; ok {
		return
	}
	prev := win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, paintProc)
	paintedButtons[hwnd] = buttonPaint{prev: prev, w: w, check: check, primary: primary}
	w.Disposing().Attach(func() { detachPaintButton(hwnd) })
	win.InvalidateRect(hwnd, nil, true)
}
func darken(w walk.Window) {
	switch v := w.(type) {
	case *walk.TextLabel:
		v.SetTextColor(walk.RGB(150, 160, 178))
	case *walk.Label:
		v.SetTextColor(walk.RGB(150, 160, 178))
	case *walk.LineEdit:
		b, _ := walk.NewSolidColorBrush(walk.RGB(14, 17, 24))
		v.SetBackground(b)
		v.SetTextColor(walk.RGB(224, 231, 242))
	case *walk.NumberEdit:
		darkNumberEdit(v)
	case *walk.TextEdit:
		b, _ := walk.NewSolidColorBrush(walk.RGB(14, 17, 24))
		v.SetBackground(b)
		v.SetTextColor(walk.RGB(184, 197, 217))
	case *walk.CheckBox:
		paintButton(&v.Button, true, false)
	case *walk.PushButton:
		paintButton(&v.Button, false, v.Text() == "Run Sync")
	}
	if c, ok := w.(walk.Container); ok {
		for i := 0; i < c.Children().Len(); i++ {
			darken(c.Children().At(i))
		}
	}
}

func withoutFlag(args []string, name string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if core.SwitchName(args[i]) != name {
			out = append(out, args[i])
			continue
		}
		if name == "XF" || name == "XD" || name == "IF" {
			for i+1 < len(args) && core.SwitchName(args[i+1]) == "" {
				i++
			}
		}
	}
	return out
}
func (a *app) flagsDialog() {
	if a.busy {
		return
	}
	var d *walk.Dialog
	var groups, flags *walk.ComboBox
	var description *walk.TextLabel
	var value *walk.LineEdit
	var staged *walk.TextEdit
	groupNames := []string{"All options", "Copy", "Metadata", "Performance", "Selection", "Retry", "Logging", "Scheduling / Jobs"}
	var visible []core.Flag
	var selected core.Flag
	update := func() {
		if flags == nil || description == nil || value == nil {
			return
		}
		i := flags.CurrentIndex()
		if i < 0 || i >= len(visible) {
			return
		}
		selected = visible[i]
		suffix := ""
		if selected.Newer {
			suffix = "  [NEWER WINDOWS / VERSION-DEPENDENT]"
		}
		description.SetText("/" + selected.Name + " — " + selected.Description + suffix + "\r\nValue kind: " + selected.Kind)
		value.SetEnabled(selected.Kind != "bool")
		value.SetText("")
	}
	load := func() {
		if groups == nil || flags == nil {
			return
		}
		visible = nil
		var names []string
		g := groups.Text()
		for _, f := range core.Catalog {
			if g == "All options" || g == f.Group {
				visible = append(visible, f)
				names = append(names, "/"+f.Name+" — "+f.Description)
			}
		}
		flags.SetModel(names)
		flags.SetCurrentIndex(0)
		update()
	}
	err := (Dialog{AssignTo: &d, Title: "Advanced Flags — documented Robocopy catalog", Size: Size{Width: 960, Height: 650}, MinSize: Size{Width: 800, Height: 580}, Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Font: Font{Family: "Segoe UI", PointSize: 10}, Layout: VBox{Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 18}, Spacing: 12}, Children: []Widget{
		Label{Text: fmt.Sprintf("%d documented switches • Support depends on installed Robocopy.", len(core.Catalog)), Font: Font{Family: "Segoe UI", PointSize: 12, Bold: true}},
		TextLabel{MinSize: Size{Width: 300}, Text: "Choose a group and flag, enter a value if needed, then Add / Replace. Edit raw arguments for repeats, unknown switches and file specs. Raw MT explicitly overrides the main selector; Use thread selector adopts it for quick editing."},
		Label{Text: "Option group"}, ComboBox{AssignTo: &groups, Model: groupNames, OnCurrentIndexChanged: load},
		Label{Text: "Documented flag"}, ComboBox{AssignTo: &flags, OnCurrentIndexChanged: update},
		TextLabel{AssignTo: &description, MinSize: Size{Width: 300, Height: 55}},
		LineEdit{AssignTo: &value, CueBanner: "Value only (no colon); XF/XD/IF: space-separated values, quote paths with spaces"},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			PushButton{Text: "Add / Replace Flag", OnClicked: func() {
				args, e := core.SplitCommandLine(staged.Text())
				if e != nil {
					a.fail(e)
					return
				}
				add, e := core.FormatFlag(selected, value.Text())
				if e != nil {
					a.fail(e)
					return
				}
				args = withoutFlag(args, selected.Name)
				args = append(args, add...)
				staged.SetText(core.Command(args))
			}},
			PushButton{Text: "Remove Selected Flag", OnClicked: func() {
				args, e := core.SplitCommandLine(staged.Text())
				if e != nil {
					a.fail(e)
					return
				}
				staged.SetText(core.Command(withoutFlag(args, selected.Name)))
			}}, HSpacer{},
		}},
		Label{Text: "Full editable option arguments — direct exec arguments, NOT a shell. In expert mode include endpoints here."},
		TextEdit{AssignTo: &staged, Text: a.raw.Text(), VScroll: true, MinSize: Size{Height: 110}, Font: Font{Family: "Consolas", PointSize: 10}},
		TextLabel{MinSize: Size{Width: 300}, Text: "MIR/PURGE/MOV/MOVE/M require managed preview + confirmation. REG/SAVE and log paths write even during /L and require confirmation. JOB/NOSD/NODD require expert mode. SAVE must be last. Unknown switches pass through unchanged; unsupported options fail visibly."},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{HSpacer{}, PushButton{Text: "Apply Flags", OnClicked: func() {
			args, e := core.SplitCommandLine(staged.Text())
			if e == nil {
				e = core.ValidateFlags(args, a.expert.Checked())
			}
			if e != nil {
				a.fail(e)
				return
			}
			a.raw.SetText(core.Command(args))
			d.Accept()
		}}, PushButton{Text: "Cancel", OnClicked: func() { d.Cancel() }}}},
	}}).Create(a.mw)
	if err != nil {
		a.fail(err)
		return
	}
	darken(d)
	darkTitleBar(d.Handle())
	groups.SetCurrentIndex(0)
	load()
	d.Run()
}
func (a *app) report() string {
	header := a.reportHeader()
	var reader io.Reader = strings.NewReader(a.logText)
	if a.runLogPath != "" {
		f, err := a.openAuditLog(a.record.ID)
		if err != nil {
			return "Audit log unreadable: " + err.Error()
		}
		defer f.Close()
		if _, err = auditio.Regular(f); err != nil {
			return err.Error()
		}
		reader = f
	}
	text, err := auditio.Clipboard(header, reader)
	if err != nil {
		return "Audit read failed: " + err.Error()
	}
	return text
}
func (a *app) reportHeader() string {
	notice := ""
	if a.auditErr != nil {
		notice = "AUDIT INCOMPLETE: " + a.auditErr.Error() + "\r\n"
	}
	return "RoboCopy Studio audit (UTF-8 captured native text; not encrypted)\r\n" + notice + a.status.Text() + "\r\n\r\n" + a.summary.Text() + "\r\n\r\n"
}

type exportResult struct {
	path string
	err  error
}

func (a *app) exportLog() {
	if a.exportCancel != nil {
		a.fail(fmt.Errorf("an export is already running"))
		return
	}
	if a.runLogPath == "" {
		a.fail(fmt.Errorf("No run log yet"))
		return
	}
	dlg := walk.FileDialog{Title: "Export sensitive run log (not encrypted)", FilePath: "RoboCopy-run-log.txt", Filter: "Text logs (*.txt)|*.txt|All files (*.*)|*.*", Flags: win.OFN_OVERWRITEPROMPT}
	ok, err := dlg.ShowSave(a.mw)
	if err != nil {
		a.fail(err)
		return
	}
	if !ok {
		return
	}
	if themedMsgBox(a.mw, "Export sensitive data?", "Logs include paths, commands and filenames. Export is NOT encrypted; destination folder permissions apply, including shared/network folders. Continue?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
		return
	}
	if err = a.validateExport(dlg.FilePath); err != nil {
		a.fail(err)
		return
	}
	original, statErr := os.Lstat(dlg.FilePath)
	if statErr != nil && !os.IsNotExist(statErr) {
		a.fail(statErr)
		return
	}
	f, err := a.openAuditLog(a.record.ID)
	if err != nil {
		a.fail(err)
		return
	}
	st, err := auditio.Regular(f)
	if err != nil {
		f.Close()
		a.fail(err)
		return
	}
	// Open and sample on the UI thread: exactly the captured bytes present at
	// this click are exported, regardless of later growth or a new run.
	header := a.reportHeader() + fmt.Sprintf("Snapshot: first %d captured log bytes at %s; later output excluded.\r\n\r\n", st.Size(), time.Now().UTC().Format(time.RFC3339))
	ctx, cancel := context.WithCancel(context.Background())
	a.exportCancel = cancel
	a.exportDone = make(chan struct{})
	a.exportResults = make(chan exportResult, 1)
	done, results, dest := a.exportDone, a.exportResults, dlg.FilePath
	go func() {
		defer close(done)
		err := a.exportSnapshot(ctx, f, st.Size(), header, dest, original)
		results <- exportResult{path: dest, err: err} // capacity 1; never waits for a disposed UI
	}()
}
func (a *app) updateSummary(e event) {
	text := a.logText
	if a.fullLog != nil {
		f, err := a.openAuditLog(a.record.ID)
		if err == nil {
			defer f.Close()
			b, readErr := auditio.Tail(f)
			if readErr == nil {
				text = string(b)
			}
		}
	}
	rows := core.Summary(text)
	if !e.preview {
		// Never leave the last I/O sample looking like a final copied counter.
		a.details.SetText("Run ended — final metrics use native summary when parseable, not I/O estimates. History retains captured output subject to audit limits; check audit warnings.")
		a.amount.SetText("Unavailable")
		a.remaining.SetText("Unavailable")
		a.speed.SetText("Unavailable")
		a.eta.SetText("No native counters")
		if core.Has(a.opts.Extra, "MT") {
			a.peak = 0
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-10s %13s %13s %13s %13s %13s %13s\r\n", "METRIC", "TOTAL", "COPIED", "SKIPPED", "MISMATCH", "FAILED", "EXTRAS"))
	for _, name := range []string{"Dirs", "Files", "Bytes", "Times"} {
		v, ok := rows[name]
		if !ok {
			b.WriteString(fmt.Sprintf("%-10s  unavailable (suppressed, localized or non-integer units)\r\n", name))
			continue
		}
		b.WriteString(fmt.Sprintf("%-10s", name))
		for _, s := range v {
			b.WriteString(fmt.Sprintf(" %13s", s))
		}
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n" + core.Outcome(e.code) + "\r\nElapsed: " + timeSince(a) + "\r\n\r\nAbout these results:\r\nThe figures above come from Robocopy's report. This app does not separately compare the contents of copied files with the source files.\r\nOpen Raw Console Output to see Robocopy's original messages.")
	b.WriteString("\r\n\r\nFile and folder error details:\r\nThis list shows error messages the app could identify, up to 1,000 different entries. Other messages, including those in another language, may only appear in the full log.\r\n")
	for _, failure := range a.record.Failures {
		b.WriteString(failure.Evidence + "\r\n")
	}
	if len(a.record.Failures) == 0 {
		b.WriteString("No file or folder error details could be listed here. Check the FAILED column and the run result above to see what Robocopy reported. If it reported a problem, open Raw Console Output or the full log in History for more details.\r\n")
	}
	a.summary.SetText(b.String())
	if e.preview {
		return
	}
	if v, ok := rows["Bytes"]; ok {
		copied, copiedErr := strconv.ParseInt(v[1], 10, 64)
		if copiedErr != nil {
			a.amount.SetText("Unavailable")
			a.remaining.SetText("Unavailable")
			return
		}
		a.amount.SetText(core.Bytes(copied))
		totalBytes, totalErr := strconv.ParseInt(v[0], 10, 64)
		skippedBytes, skippedErr := strconv.ParseInt(v[2], 10, 64)
		if totalErr == nil && skippedErr == nil && totalBytes >= copied && totalBytes-copied >= skippedBytes && !core.Has(a.opts.Extra, "L") && !core.Has(a.opts.Extra, "QUIT") {
			a.remaining.SetText(core.Bytes(totalBytes - copied - skippedBytes))
			if totalBytes > 0 {
				a.progress.SetValue(int((float64(copied) + float64(skippedBytes)) / float64(totalBytes) * 1000))
			} else if e.err == nil && e.code < 8 {
				a.progress.SetValue(1000)
			}
		} else {
			a.remaining.SetText("Unavailable")
		}
		if !core.Has(a.opts.Extra, "L") && !core.Has(a.opts.Extra, "QUIT") {
			avg := float64(copied) / time.Since(a.began).Seconds()
			peak := "—"
			if a.peak > 0 {
				peak = "≈ " + core.Bytes(int64(a.peak)) + "/s"
			}
			a.speed.SetText(fmt.Sprintf("Avg %s/s\r\nPeak %s", core.Bytes(int64(avg)), peak))
		} else {
			a.amount.SetText("List / params only")
		}
	}
	if v, ok := rows["Files"]; ok {
		a.eta.SetText(fmt.Sprintf("ETA —\r\nFiles %s / %s", v[1], v[0]))
	}
	if core.OpenEnded(a.opts.Extra) {
		a.eta.SetText("Monitoring stopped")
	}
}
func timeSince(a *app) string { return fmt.Sprint(time.Since(a.began).Round(time.Millisecond)) }
