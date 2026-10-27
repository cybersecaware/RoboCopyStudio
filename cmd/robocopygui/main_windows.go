//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
	"robocopygui/internal/core"
)

type event struct {
	telemetryLimited bool
	auditError       error
	commandText      string
	ioBytes          uint64
	ioValid          bool
	text             string
	line             string
	done             bool
	code             int
	err              error
	preview          bool
	plan             core.Plan
}
type app struct {
	elevated                                bool
	adminRestart                            *walk.PushButton
	privilegeStatus                         *walk.TextLabel
	exportCancel                            context.CancelFunc
	exportDone                              chan struct{}
	exportResults                           chan exportResult
	auditBytes                              int64
	storeBytes                              int64
	store                                   core.Store
	record                                  core.RunRecord
	auditErr                                error
	profiles                                *walk.ComboBox
	profileIDs                              []string
	profileSave, profileLoad, profileDelete *walk.PushButton
	danger                                  *walk.TextLabel
	mw                                      *walk.MainWindow
	src, dst                                *walk.LineEdit
	mirror, show                            *walk.CheckBox
	start, cancel, bs, bd                   *walk.PushButton
	status, amount, details, current        *walk.Label
	progress                                *walk.ProgressBar
	console                                 *walk.TextEdit
	events                                  chan event
	stop                                    context.CancelFunc
	busy, ready, closing                    bool
	opts                                    core.Options
	plan                                    core.Plan
	tracker                                 *core.Tracker
	began                                   time.Time
	logText                                 string
	raw, command                            *walk.LineEdit
	expert                                  *walk.CheckBox
	swap, advanced, previewButton           *walk.PushButton
	restart, threaded                       *walk.CheckBox
	threads                                 *walk.NumberEdit
	threadState                             *walk.Label
	useThreads                              *walk.PushButton
	remaining, speed, eta                   *walk.Label
	lower                                   *walk.Composite
	summary                                 *walk.TextEdit
	fullLog                                 *os.File
	runLogPath                              string
	lastPreview                             string
	peak                                    float64
	lastSample                              time.Time
	lastSampleBytes                         int64
	initializing                            bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--scheduled-job" {
		if len(os.Args) != 3 {
			os.Exit(2)
		}
		os.Exit(scheduledWorker(os.Args[2]))
	}
	if len(os.Args) > 1 && os.Args[1] == "--smoke-test" {
		output := ""
		if len(os.Args) > 2 {
			output = os.Args[2]
		}
		os.Exit(smoke(output))
	}
	a := &app{events: make(chan event, 32)}
	err := a.create()
	if err != nil {
		themedMsgBox(nil, "RoboCopy GUI", err.Error(), walk.MsgBoxIconError)
		return
	}
	a.mw.Run()
}
func (a *app) create() error {
	var privilegeErr error
	a.elevated, privilegeErr = currentProcessElevated()
	if privilegeErr != nil {
		return fmt.Errorf("read current process privileges: %w", privilegeErr)
	}
	var storeErr error
	a.store, storeErr = defaultStore()
	if storeErr != nil {
		return storeErr
	}
	a.initializing = true
	var title *walk.Label
	// rsrc assigns manifest ID 1, then the first icon-group ID 2.
	appIcon, err := walk.NewIconFromResourceId(2)
	if err != nil {
		return fmt.Errorf("load embedded application icon: %w", err)
	}
	err = (MainWindow{AssignTo: &a.mw, Icon: appIcon, Title: privilegeTitle(a.elevated), MinSize: Size{Width: 1050, Height: 780}, Size: Size{Width: 1280, Height: 960}, Font: Font{Family: "Segoe UI", PointSize: 10}, Background: SolidColorBrush{Color: walk.RGB(14, 17, 24)}, Layout: VBox{Margins: Margins{Left: 20, Top: 16, Right: 20, Bottom: 14}, Spacing: 8}, Children: []Widget{
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			Label{AssignTo: &title, Text: "ROBOCOPY STUDIO", Font: Font{Family: "Segoe UI Semibold", PointSize: 17}}, HSpacer{},
			PushButton{Text: "Schedule Jobs…", MinSize: Size{Height: 32}, OnClicked: a.scheduleDialog},
			PushButton{Text: "History…", MinSize: Size{Height: 32}, OnClicked: a.historyDialog},
			PushButton{Text: "Flags Doc", MinSize: Size{Height: 32}, OnClicked: func() {
				cmd, err := documentationCommand()
				if err == nil {
					err = startDetached(cmd)
				}
				if err != nil {
					a.fail(err)
				}
			}},
			PushButton{Text: "Export Run Log", MinSize: Size{Height: 32}, OnClicked: a.exportLog},
		}},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			TextLabel{AssignTo: &a.privilegeStatus, Name: "PrivilegeContext", MinSize: Size{Width: 700}, StretchFactor: 1, Text: privilegeHint(a.elevated, nil)}, HSpacer{},
			PushButton{AssignTo: &a.adminRestart, Name: "RestartAdministrator", Text: "Restart as administrator…", Enabled: !a.elevated, MinSize: Size{Width: 240, Height: 32}, MaxSize: Size{Width: 240, Height: 32}, ToolTipText: "Optional Windows UAC restart. Save a profile first; the new window is blank and does not replay a job. No automatic elevation.", OnClicked: a.restartAdministrator},
		}},
		Label{Text: "Reliable Windows replication engine. Real-time telemetry with zero concealed command flags."},
		Composite{Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Layout: VBox{Margins: Margins{Left: 18, Top: 14, Right: 18, Bottom: 12}, Spacing: 6}, Children: []Widget{
			Label{Text: "TRANSFER ENDPOINTS", Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "Saved profile"}, ComboBox{AssignTo: &a.profiles, StretchFactor: 1, MinSize: Size{Width: 180}},
				PushButton{AssignTo: &a.profileSave, Text: "Save New…", OnClicked: a.saveProfile},
				PushButton{AssignTo: &a.profileLoad, Text: "Load", OnClicked: a.loadProfile},
				PushButton{AssignTo: &a.profileDelete, Text: "Delete…", OnClicked: a.deleteProfile},
			}},
			Composite{Layout: Grid{Columns: 5, Spacing: 10, MarginsZero: true}, Children: []Widget{
				Label{Text: "Source Path", ColumnSpan: 2}, HSpacer{}, Label{Text: "Destination Target", ColumnSpan: 2},
				LineEdit{AssignTo: &a.src, CueBanner: "Source folder", MinSize: Size{Height: 32}, OnTextChanged: a.invalidate}, PushButton{AssignTo: &a.bs, Text: "Browse Source…", MinSize: Size{Height: 32}, OnClicked: func() { a.browse(a.src) }},
				PushButton{AssignTo: &a.swap, Text: "⇄ Swap", MinSize: Size{Height: 32}, OnClicked: func() { s, d := a.src.Text(), a.dst.Text(); a.src.SetText(d); a.dst.SetText(s) }},
				LineEdit{AssignTo: &a.dst, CueBanner: "Destination folder", MinSize: Size{Height: 32}, OnTextChanged: a.invalidate}, PushButton{AssignTo: &a.bd, Text: "Browse Destination…", MinSize: Size{Height: 32}, OnClicked: func() { a.browse(a.dst) }},
			}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				CheckBox{AssignTo: &a.mirror, Text: "/MIR Mirror", Accessibility: Accessibility{Name: "/MIR Mirror (deletes extras)"}, ToolTipText: "Mirror directory tree; deletes destination extras.", MinSize: Size{Width: 150, Height: 26}, OnCheckedChanged: a.invalidate},
				CheckBox{AssignTo: &a.restart, Text: "/Z Restartable", MinSize: Size{Width: 160, Height: 26}, Checked: true, OnCheckedChanged: a.invalidate},
				CheckBox{AssignTo: &a.threaded, Text: "/MT Threads", Accessibility: Accessibility{Name: "/MT Threads"}, ToolTipText: "MT shows aggregate process I/O activity, not per-file progress or verified copied bytes.", MinSize: Size{Width: 130, Height: 26}, Checked: false, OnCheckedChanged: a.invalidate},
				Label{Text: "Threads"}, NumberEdit{AssignTo: &a.threads, Name: "MTWorkers", Accessibility: Accessibility{Name: "MT worker count (1–128)"}, MinSize: Size{Width: 72, Height: 32}, MaxSize: Size{Width: 82}, MinValue: 1, MaxValue: 128, Decimals: 0, Increment: 1, SpinButtonsVisible: true, Value: float64(core.RecommendedThreads(runtime.NumCPU())), OnValueChanged: a.invalidate},
				Label{Text: fmt.Sprintf("%d logical CPUs · suggested %d", runtime.NumCPU(), core.RecommendedThreads(runtime.NumCPU()))}, HSpacer{},
				PushButton{AssignTo: &a.advanced, Text: "Advanced Flags…", MinSize: Size{Height: 32}, OnClicked: a.flagsDialog},
			}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{AssignTo: &a.threadState, Text: "MT off — single-threaded telemetry available with a preview."}, HSpacer{},
				PushButton{AssignTo: &a.useThreads, Text: "Use thread selector", MinSize: Size{Height: 32}, Visible: false, OnClicked: a.adoptRawThreads},
			}},
			Label{Text: "Options / file specs — editable Windows argument syntax; quotes preserve spaces. Defaults are visible below."},
			LineEdit{AssignTo: &a.raw, Text: "/E /R:2 /W:2 /XJ /COPY:DAT /DCOPY:DAT /BYTES /FP", Font: Font{Family: "Consolas", PointSize: 10}, MinSize: Size{Height: 32}, OnTextChanged: a.invalidate},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				CheckBox{AssignTo: &a.expert, Text: "Expert full command", Accessibility: Accessibility{Name: "Expert full command (includes endpoints): JOB / unknown flags; managed safety checks bypassed"}, ToolTipText: "Include endpoints in the command. JOB / unknown flags allowed; managed safety checks bypassed.", MinSize: Size{Width: 220, Height: 26}, OnCheckedChanged: a.invalidate},
				Label{Text: "Includes endpoints; bypasses managed safety checks.", EllipsisMode: EllipsisEnd}, HSpacer{},
			}},
		}},
		Label{Text: "EXACT COMMAND  •  Native log capture is explicit below; arguments run without a shell"},
		LineEdit{AssignTo: &a.command, ReadOnly: true, MinSize: Size{Height: 32}, Font: Font{Family: "Consolas", PointSize: 10}},
		TextLabel{AssignTo: &a.danger, Visible: false, MinSize: Size{Width: 300}, Background: SolidColorBrush{Color: walk.RGB(67, 32, 21)}, Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}},
		Composite{Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Layout: VBox{Margins: Margins{Left: 18, Top: 16, Right: 18, Bottom: 14}, Spacing: 8}, Children: []Widget{
			Label{AssignTo: &a.status, Text: "Engine Ready — preview first; review destructive options carefully.", Font: Font{Family: "Segoe UI", PointSize: 12, Bold: true}},
			Label{Text: "Pipeline Progress  •  ST: estimated copied bytes  /  MT: I/O-budget proxy, NOT completion"},
			ProgressBar{AssignTo: &a.progress, MinValue: 0, MaxValue: 1000, MinSize: Size{Height: 12}, MaxSize: Size{Height: 14}},
			Composite{MinSize: Size{Height: 104}, MaxSize: Size{Height: 104}, Layout: Grid{Columns: 4, Spacing: 12, MarginsZero: true}, Children: []Widget{
				metric("COPIED / I/O ACTIVITY", &a.amount, "—"), metric("REMAINING QUEUE", &a.remaining, "—"), metric("RATE / PEAK (SEE SOURCE)", &a.speed, "—"), metric("ETA / ITEMS", &a.eta, "—"),
			}},
			Label{AssignTo: &a.details, EllipsisMode: EllipsisEnd, Text: "Choose endpoints, then Preview Mode (/L). Single-threaded live estimates enabled; /MT is opt-in."},
			Label{AssignTo: &a.current, EllipsisMode: EllipsisPath, Text: "Final authority: Robocopy exit status and raw summary. Not a content verification tool."},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{AssignTo: &a.start, Text: "Run Sync", MinSize: Size{Width: 160, Height: 38}, OnClicked: a.startClick},
				PushButton{AssignTo: &a.previewButton, Text: "Preview Mode (/L)", MinSize: Size{Width: 160, Height: 38}, OnClicked: a.previewClick},
				PushButton{AssignTo: &a.cancel, Text: "Abort / Stop", MinSize: Size{Width: 130, Height: 38}, Enabled: false, OnClicked: a.cancelClick}, HSpacer{},
				CheckBox{AssignTo: &a.show, Text: "Show console / audit", Accessibility: Accessibility{Name: "Show live console & audit grid"}, MinSize: Size{Width: 230, Height: 28}, Checked: true, OnCheckedChanged: func() {
					if a.lower != nil {
						a.lower.SetVisible(a.show.Checked())
					}
				}},
			}},
		}},
		Composite{AssignTo: &a.lower, StretchFactor: 1, Background: SolidColorBrush{Color: walk.RGB(20, 24, 34)}, Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8}, Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{Text: "Summary Table", MinSize: Size{Height: 32}, OnClicked: func() { a.summary.SetVisible(true); a.console.SetVisible(false) }},
				PushButton{Text: "Raw Console Output", MinSize: Size{Height: 32}, OnClicked: func() {
					a.summary.SetVisible(false)
					a.console.SetVisible(true)
				}}, HSpacer{},
				PushButton{Text: "Copy Report", ToolTipText: "Explicitly copy up to 1 MiB of sensitive paths/commands to the clipboard. Windows history/sync and other apps may retain it; no automatic clearing.", MinSize: Size{Height: 32}, OnClicked: func() {
					if e := walk.Clipboard().SetText(a.report()); e != nil {
						a.fail(e)
					}
				}},
			}},
			TextEdit{AssignTo: &a.summary, ReadOnly: true, VScroll: true, MinSize: Size{Height: 140}, Font: Font{Family: "Consolas", PointSize: 10}, Text: "METRIC         TOTAL        COPIED       SKIPPED      MISMATCH     FAILED       EXTRAS\r\n\r\nNo native summary yet. Localized output remains available in Raw Console Output."},
			TextEdit{AssignTo: &a.console, ReadOnly: true, VScroll: true, Visible: false, MinSize: Size{Height: 140}, Font: Font{Family: "Consolas", PointSize: 10}},
		}},
		TextLabel{MinSize: Size{Width: 300}, Text: "Logs contain sensitive paths, not encrypted. Audit limits: 128 MiB/run, 1 GiB/store; no automatic pruning. Flags Doc may inherit elevation."},
	}}).Create()
	if err != nil {
		return err
	}
	a.initializing = false
	darken(a.mw)
	darkTitleBar(a.mw.Handle())
	title.SetTextColor(walk.RGB(242, 246, 252))
	a.status.SetTextColor(walk.RGB(72, 205, 176))
	a.amount.SetTextColor(walk.RGB(72, 205, 176))
	a.speed.SetTextColor(walk.RGB(38, 188, 239))
	a.danger.SetTextColor(walk.RGB(255, 189, 114))
	a.refreshProfiles()
	a.invalidate()
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if a.busy {
			*canceled = true
			if themedMsgBox(a.mw, "Cancel and close?", "Stop Robocopy and close after the process exits? Partially copied files may remain.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) == walk.DlgCmdYes {
				a.closing = true
				a.stop()
				a.status.SetText("Stopping Robocopy…")
			}
		}
	})
	quit := make(chan struct{})
	a.mw.Disposing().Attach(func() {
		close(quit)
		if a.exportCancel != nil {
			a.exportCancel()
			<-a.exportDone
		}
		if a.fullLog != nil {
			a.fullLog.Close()
		}

		// Walk's mainLoop checks the HWND only before GetMessage. A synchronous
		// cross-thread WM_CLOSE can dispose this window inside GetMessage, which
		// then keeps waiting for a queued message instead of rechecking the HWND.
		// Wake that loop with WM_QUIT on this UI thread, not os.Exit. Disposing
		// only follows an accepted close; the busy-copy Closing handler above
		// still cancels the close until Robocopy has exited and reported done.
		walk.App().Exit(0)
	})
	go func() {
		tick := time.NewTicker(150 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
			}
			ack := make(chan struct{})
			a.mw.Synchronize(func() {
				defer close(ack)
				select {
				case <-quit:
					return
				default:
				}
				if a.exportResults != nil {
					select {
					case result := <-a.exportResults:
						<-a.exportDone
						a.exportCancel()
						a.exportCancel = nil
						a.exportDone = nil
						a.exportResults = nil
						if result.err != nil {
							a.fail(result.err)
						} else {
							themedMsgBox(a.mw, "Run log exported", result.path, walk.MsgBoxIconInformation)
						}
					default:
					}
				}
				for i := 0; i < 100; i++ {
					// A queued callback can outlive disposal (or handle a done
					// event that closes the window). Never touch disposed controls.
					select {
					case <-quit:
						return
					default:
					}
					select {
					case e := <-a.events:
						a.handle(e)
					default:
						return
					}
				}
			})
			select {
			case <-quit:
				return
			case <-ack:
			}
		}
	}()
	return nil
}
func (a *app) browse(target *walk.LineEdit) {
	d := walk.FileDialog{Title: "Choose a folder", FilePath: target.Text()}
	if ok, err := d.ShowBrowseFolder(a.mw); err != nil {
		a.fail(err)
	} else if ok {
		target.SetText(d.FilePath)
	}
}
func (a *app) fail(err error) {
	themedMsgBox(a.mw, "Cannot continue", err.Error(), walk.MsgBoxIconError)
}
func (a *app) bind(lock bool) {
	a.profiles.SetEnabled(!lock)
	a.profileSave.SetEnabled(!lock)
	a.profileLoad.SetEnabled(!lock)
	a.profileDelete.SetEnabled(!lock)
	a.src.SetEnabled(!lock)
	a.dst.SetEnabled(!lock)
	a.mirror.SetEnabled(!lock)
	a.bs.SetEnabled(!lock)
	a.bd.SetEnabled(!lock)
	a.cancel.SetEnabled(lock)
	a.swap.SetEnabled(!lock)
	a.advanced.SetEnabled(!lock)
	a.raw.SetEnabled(!lock)
	a.expert.SetEnabled(!lock)
	a.restart.SetEnabled(!lock)
	a.threaded.SetEnabled(!lock)
	a.updateThreadControls(lock)
	a.previewButton.SetEnabled(!lock)
	if a.adminRestart != nil {
		a.adminRestart.SetEnabled(!lock && !a.elevated)
	}
}

// Resolve existing ancestors so a destination not created yet is still checked
// against junction/symlink aliases of the source. No directories are created.
func resolved(p string) (string, error) {
	p, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	var tail []string
	for {
		_, e := os.Lstat(p)
		if e == nil {
			// Resolve the opened directory's DOS path, not merely reparse points:
			// EvalSymlinks preserves SUBST drive spellings and misses nesting.
			q, e := canonicalDirectory(p)
			if e != nil {
				return "", e
			}
			for i := len(tail) - 1; i >= 0; i-- {
				q = filepath.Join(q, tail[i])
			}
			return q, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", e
		}
		tail = append(tail, filepath.Base(p))
		p = parent
	}
}
func validate(src, dst string) error {
	if e := core.ValidatePaths(src, dst); e != nil {
		return e
	}
	s, e := os.Stat(src)
	if e != nil {
		return e
	}
	if !s.IsDir() {
		return fmt.Errorf("source is not a folder")
	}
	if d, e := os.Stat(dst); e == nil && !d.IsDir() {
		return fmt.Errorf("destination is not a folder")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	a, e := resolved(src)
	if e != nil {
		return e
	}
	b, e := resolved(dst)
	if e != nil {
		return e
	}
	return core.ValidatePaths(a, b)
}
func (a *app) options() (core.Options, error) {
	extra, e := core.SplitCommandLine(a.raw.Text())
	if e != nil {
		return core.Options{}, e
	}
	o := core.Options{Source: strings.TrimSpace(a.src.Text()), Destination: strings.TrimSpace(a.dst.Text()), Custom: true, Expert: a.expert.Checked()}
	if !o.Expert {
		var quick []string
		if a.mirror.Checked() {
			quick = append(quick, "/MIR")
		}
		if a.restart.Checked() {
			quick = append(quick, "/Z")
		}
		if a.threaded.Checked() && !core.Has(extra, "MT") && a.threads.Value() != float64(int(a.threads.Value())) {
			return core.Options{}, fmt.Errorf("/MT must be an integer 1–128")
		}
		extra, e = core.ManagedThreads(extra, a.threaded.Checked(), int(a.threads.Value()))
		if e != nil {
			return core.Options{}, e
		}
		extra = append(quick, extra...)
	}
	o.Extra = extra
	_, e = core.Build(o, false)
	return o, e
}
func (a *app) invalidate() {
	if a.initializing || a.command == nil || a.busy {
		return
	}
	a.ready = false
	a.lastPreview = ""
	a.updateThreadControls(false)
	rawArgs, rawErr := core.SplitCommandLine(a.raw.Text())
	if a.privilegeStatus != nil {
		a.privilegeStatus.SetText(privilegeHint(a.elevated, rawArgs))
	}
	if a.mirror.Checked() && !a.expert.Checked() {
		rawArgs = append(rawArgs, "/MIR")
	}
	warning := core.DangerBanner(rawArgs, a.expert.Checked(), a.dst.Text())
	if rawErr != nil {
		warning = "OPTIONS INVALID — cannot determine deletion scope; correct arguments before running."
	}
	a.danger.SetText(warning)
	a.danger.SetVisible(warning != "")
	o, e := a.options()
	if e != nil {
		a.command.SetText("Invalid: " + e.Error())
		return
	}
	args, _ := core.Build(o, false)
	sys, _ := windows.GetSystemDirectory()
	a.command.SetText(core.Command(append([]string{filepath.Join(sys, "robocopy.exe")}, args...)))
}
func (a *app) previewClick() { a.launch(true) }
func (a *app) startClick()   { a.launch(false) }
func (a *app) launch(preview bool) {
	if a.busy {
		return
	}
	o, e := a.options()
	if e != nil {
		a.fail(e)
		return
	}
	args, e := core.Build(o, preview)
	if e != nil {
		a.fail(e)
		return
	}
	fingerprint := core.Command(o.Extra) + o.Source + "\x00" + o.Destination
	if !preview && !o.Expert && core.Destructive(o.Extra) && (!a.ready || a.lastPreview != fingerprint) {
		a.fail(fmt.Errorf("Destructive options require a successful Preview Mode (/L) of these exact endpoints and flags first"))
		return
	}
	if o.Expert {
		if themedMsgBox(a.mw, "Expert direct execution — confirm scope", "Full-command mode bypasses endpoint/alias safety. JOB files may override endpoints, enable deletion, write logs, registry or saved jobs. Inspect the RCJ file yourself. Preview is NOT a general side-effect sandbox. Monitoring runs until stopped.\n\n"+core.Command(args)+"\n\nExecute this exact command?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
			return
		}
	} else if core.SideEffects(o.Extra) || (!preview && core.Destructive(o.Extra)) {
		if themedMsgBox(a.mw, "Confirm destructive / persistent side effects", "Source: "+o.Source+"\nDestination: "+o.Destination+"\n\n"+core.Command(args)+"\n\nMIR/PURGE delete destination data; MOV/MOVE delete source data; M resets archive bits. REG/SAVE/log switches WRITE even with /L. Files may change since preview. Continue?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
			return
		}
	}
	a.opts = o
	sys, _ := windows.GetSystemDirectory()
	a.command.SetText(core.Command(append([]string{filepath.Join(sys, "robocopy.exe")}, args...)))
	if !preview && a.lastPreview != fingerprint {
		a.plan = core.NewPlan()
	}
	a.tracker = nil
	a.peak = 0
	if a.fullLog != nil {
		a.fullLog.Close()
		a.fullLog = nil
	}
	a.storeBytes, e = a.storeUsage()
	if e != nil {
		a.fail(e)
		return
	}
	// Reserve room for terminal JSON and both native diagnostic spools.
	if a.storeBytes > auditio.StoreBytes-3*auditio.RunBytes-(8<<20) {
		a.fail(fmt.Errorf("insufficient audit storage budget; explicitly delete selected old history (no automatic pruning)"))
		return
	}
	a.auditBytes = 0
	id, e := core.NewID()
	if e != nil {
		a.fail(e)
		return
	}
	a.fullLog, e = a.store.CreateLog(id)
	if e != nil {
		a.fail(e)
		return
	}
	a.runLogPath = a.fullLog.Name()
	a.auditErr = nil
	a.record = core.RunRecord{SchemaVersion: core.StoreVersion, ID: id, Status: "running", Started: time.Now().UTC(), ExitCode: -1, Source: o.Source, Destination: o.Destination, Command: a.command.Text(), Preview: preview, Expert: o.Expert, MT: core.Has(o.Extra, "MT"), PlannedBytes: a.plan.Bytes, PlannedFiles: len(a.plan.Files)}
	if e = a.store.SaveRun(a.record); e != nil {
		a.fullLog.Close()
		a.fullLog = nil
		a.fail(e)
		return
	}
	a.logText = ""
	a.console.SetText("")
	a.progress.SetValue(0)
	a.amount.SetText("—")
	a.remaining.SetText("—")
	a.speed.SetText("—")
	a.eta.SetText("—")
	a.summary.SetText("Waiting for native summary. Suppressed or translated summaries are not guessed.")
	a.begin(preview)
}
func (a *app) begin(preview bool) {
	a.busy = true
	a.bind(true)
	a.start.SetEnabled(false)
	a.cancel.SetText("Abort / Stop")
	ctx, cancel := context.WithCancel(context.Background())
	a.stop = cancel
	a.began = time.Now()
	a.lastSample = a.began
	a.lastSampleBytes = 0
	if preview {
		a.status.SetText("Previewing /L — no file transfer; approved log/job/registry writes may occur")
	} else {
		if (!a.opts.Custom || core.Telemetry(a.opts.Extra)) && !a.opts.Expert && !a.plan.Limited {
			a.tracker = core.NewTracker(a.plan, a.opts.Source)
		}
		if a.tracker != nil && len(a.plan.Files) > 0 {
			a.details.SetText("Live estimates use the approved preview. Final counters come from the native summary, not content verification.")
		} else {
			a.details.SetText("MT uses aggregate process-write activity (not copied bytes). Other unavailable telemetry remains unknown. Final counters use native summary.")
		}
		a.status.SetText("Copying — progress is estimated; keep the summary for final status")
	}
	a.progress.SetMarqueeMode(preview || a.tracker == nil || len(a.plan.Files) == 0)
	go a.run(ctx, a.opts, preview)
}
func (a *app) cancelClick() {
	if a.busy {
		a.stop()
		a.cancel.SetEnabled(false)
		a.status.SetText("Stopping — partial files may remain…")
	} else if a.ready {
		a.reset()
		a.status.SetText("Preview discarded. Choose folders or preview again.")
	}
}
func (a *app) reset() {
	a.busy = false
	a.ready = false
	a.bind(false)
	a.start.SetEnabled(true)
	a.start.SetText("Run Sync")
	a.cancel.SetText("Abort / Stop")
}
func (a *app) handle(e event) {
	if e.telemetryLimited {
		a.tracker = nil
		a.progress.SetMarqueeMode(true)
		a.details.SetText("Detailed telemetry unavailable: oversized native record. Raw captured output is retained subject to audit limits.")
	}
	if e.commandText != "" {
		a.record.Command = e.commandText
		if err := a.store.SaveRun(a.record); err != nil && a.auditErr == nil {
			a.auditErr = err
			a.stop()
		}
	}
	if e.ioValid {
		a.showActivity(e.ioBytes)
	}
	if f, ok := core.FailureLine(e.line); ok {
		a.record.Failures = core.AppendFailure(a.record.Failures, f)
	}
	if e.text != "" {
		if a.fullLog != nil && a.auditErr == nil {
			if a.auditBytes+int64(len(e.text)) > auditio.RunBytes || a.storeBytes+a.auditBytes+int64(len(e.text))+2*auditio.RunBytes+(8<<20) > auditio.StoreBytes {
				a.auditErr = fmt.Errorf("128 MiB audit transcript budget reached; capture incomplete; no automatic pruning")
				a.stop()
			} else {
				n, err := a.fullLog.WriteString(e.text)
				a.auditBytes += int64(n)
				if err != nil {
					a.auditErr = err
					a.stop()
				}
			}
		}
		a.logText = core.BoundedConsole(a.logText, e.text)
		a.console.SetText(a.logText)
		a.console.SetTextSelection(a.console.TextLength(), a.console.TextLength())
		a.console.ScrollToCaret()
	}
	if e.line != "" && a.tracker != nil && !e.preview && len(a.plan.Files) > 0 {
		a.tracker.Feed(e.line)
		b, f := a.tracker.Estimate()
		if a.plan.Bytes > 0 {
			a.progress.SetValue(int(float64(b) / float64(a.plan.Bytes) * 1000))
		}
		left := a.plan.Bytes - b
		if left < 0 {
			left = 0
		}
		a.amount.SetText("≈ " + core.Bytes(b))
		a.remaining.SetText("≈ " + core.Bytes(left))
		rate := float64(b) / time.Since(a.began).Seconds()
		eta := "—"
		if rate > 0 {
			eta = (time.Duration(float64(left)/rate) * time.Second).Round(time.Second).String()
		}
		if interval := time.Since(a.lastSample).Seconds(); interval >= 0.15 {
			active := float64(b-a.lastSampleBytes) / interval
			if active > a.peak {
				a.peak = active
			}
			a.speed.SetText(fmt.Sprintf("≈ %s/s\r\nPeak ≈ %s/s", core.Bytes(int64(active)), core.Bytes(int64(a.peak))))
			a.lastSample = time.Now()
			a.lastSampleBytes = b
		}
		a.eta.SetText(fmt.Sprintf("≈ %s\r\nFiles %d / %d", eta, f, len(a.plan.Files)))
		if a.tracker.Current != "" {
			a.current.SetText(a.tracker.Current)
		}
	}
	if !e.done {
		return
	}
	a.busy = false
	a.progress.SetMarqueeMode(false)
	a.updateSummary(e)
	auditErr := a.finishAudit(e)
	if auditErr != nil {
		a.auditErr = auditErr
	}
	if a.auditErr != nil {
		if !a.closing {
			a.fail(fmt.Errorf("Persistent audit incomplete: %w", a.auditErr))
		}
		e.err = errors.Join(e.err, a.auditErr)
	}
	a.stop()
	a.cancel.SetEnabled(true)
	if a.closing {
		a.mw.Close()
		return
	}
	if e.err != nil {
		a.reset()
		a.status.SetText("Stopped / could not run — partial files may remain")
		a.fail(e.err)
		return
	}
	if e.code >= 8 {
		a.reset()
		a.status.SetText(fmt.Sprintf("%s  •  elapsed %s", core.Outcome(e.code), time.Since(a.began).Round(time.Millisecond)))
		a.show.SetChecked(true)
		a.summary.SetVisible(false)
		a.console.SetVisible(true)
		return
	}
	if e.preview {
		a.plan = e.plan
		a.ready = !e.plan.Limited
		a.lastPreview = ""
		if a.ready {
			a.lastPreview = core.Command(a.opts.Extra) + a.opts.Source + "\x00" + a.opts.Destination
		}
		a.bind(false)
		a.tracker = nil
		a.start.SetEnabled(true)
		a.start.SetText("Run Sync")
		a.cancel.SetEnabled(false)
		a.status.SetText(fmt.Sprintf("Preview ready — inspect console, then Run Sync • exit %d • elapsed %s", e.code, time.Since(a.began).Round(time.Millisecond)))
		a.amount.SetText("Preview only")
		a.remaining.SetText("≈ " + core.Bytes(e.plan.Bytes))
		a.details.SetText(fmt.Sprintf("Planned files: %d • Estimates exclude deletion work and retries", len(e.plan.Files)))
		if e.plan.Limited {
			a.status.SetText("Large preview cannot approve destructive operation; detailed telemetry unavailable")
			a.details.SetText(e.plan.LimitReason)
			a.remaining.SetText("Unavailable")
		}
		if len(e.plan.Files) == 0 && !e.plan.Limited {
			a.remaining.SetText("Unavailable")
			a.details.SetText("No transfer entries parsed — progress unavailable; review preview. Copy still uses Robocopy.")
		}
		a.show.SetChecked(true)
		a.summary.SetVisible(false)
		a.console.SetVisible(true)
		return
	}
	a.reset()
	a.status.SetText(fmt.Sprintf("%s  •  elapsed %s", core.Outcome(e.code), time.Since(a.began).Round(time.Millisecond)))
	a.current.SetText("Final authority: Robocopy's exit code and summary below (estimated counters are not verification).")
	if core.Has(a.opts.Extra, "L") || core.Has(a.opts.Extra, "QUIT") {
		a.status.SetText(fmt.Sprintf("List / parameter-only run finished — exit %d • elapsed %s", e.code, time.Since(a.began).Round(time.Millisecond)))
	}
	a.show.SetChecked(true)
	a.summary.SetVisible(true)
	a.console.SetVisible(false)
}
func (a *app) run(ctx context.Context, o core.Options, preview bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := core.NewPlan()
	var captureErr error
	finish := func(code int, err error) {
		a.events <- event{done: true, code: code, err: err, auditError: captureErr, preview: preview, plan: p}
	}
	if e := func() error {
		if o.Expert {
			return nil
		}
		return validate(o.Source, o.Destination)
	}(); e != nil {
		finish(-1, e)
		return
	}
	if ctx.Err() != nil {
		finish(-1, ctx.Err())
		return
	}
	sys, e := windows.GetSystemDirectory()
	if e != nil {
		finish(-1, e)
		return
	}
	exe := filepath.Join(sys, "robocopy.exe")
	f, e := os.CreateTemp(filepath.Join(a.store.Root, "runs"), ".native-*.pending")
	if e != nil {
		finish(-1, e)
		return
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Close(); e != nil {
		finish(-1, e)
		return
	}
	o.Log = name
	o.Preview = preview
	args, buildErr := core.Build(o, preview)
	if buildErr != nil {
		finish(-1, buildErr)
		return
	}
	actualCommand := core.Command(append([]string{exe}, args...))
	a.events <- event{commandText: actualCommand, preview: preview}
	auditio.EmitChunks("\r\n"+actualCommand+"\r\n", func(chunk string) bool {
		select {
		case a.events <- event{text: chunk, preview: preview}:
			return true
		case <-ctx.Done():
			return false
		}
	})
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.WaitDelay = 2 * time.Second
	selected := core.SelectedLog(args)
	// Validate every explicit log switch without silently removing any flags.
	for _, arg := range args {
		if key := core.SwitchName(arg); key == "LOG" || key == "LOG+" || key == "UNILOG" || key == "UNILOG+" {
			_, path, ok := strings.Cut(arg, ":")
			if !ok {
				finish(-1, fmt.Errorf("native log path missing"))
				return
			}
			if err := auditio.SafeLogTarget(path); err != nil {
				finish(-1, err)
				return
			}
			if st, err := os.Stat(path); err == nil && !st.Mode().IsRegular() {
				finish(-1, fmt.Errorf("native log must be a regular file"))
				return
			}
		}
	}
	if !strings.EqualFold(selected.Path, name) {
		a.events <- event{text: "\r\nExplicit native log paths are user-managed and may grow outside app storage limits. Captured transcript remains limited to 128 MiB.\r\n", preview: preview}
	}
	// cmd.Dir is empty: relative (including drive-relative) log paths resolve
	// exactly as they do for the inherited Robocopy working directory.
	var offset int64
	if selected.Append {
		if st, err := os.Stat(selected.Path); err == nil {
			offset = st.Size()
		}
		offset = core.LogOffset(args, offset, func(left, right string) bool {
			l, le := filepath.Abs(left)
			r, re := filepath.Abs(right)
			return le == nil && re == nil && strings.EqualFold(l, r)
		})
	}
	// Keep stdout only for diagnostics when the native log cannot be opened.
	// It is NOT a reliable Unicode telemetry stream, even with /UNICODE.
	fallback, e := os.CreateTemp(filepath.Join(a.store.Root, "runs"), ".stdout-*.pending")
	if e != nil {
		finish(-1, e)
		return
	}
	defer os.Remove(fallback.Name())
	defer fallback.Close()
	spool := auditio.NewBudgetWriter(fallback, auditio.RunBytes, cancel)
	cmd.Stdout, cmd.Stderr = spool, spool
	if e = cmd.Start(); e != nil {
		finish(-1, e)
		return
	}
	// Query only this owned native process; never infer transfer from file sizes.
	var activityHandle windows.Handle
	var activityBaseline uint64
	activityEnabled := !preview && !o.Expert && core.Has(o.Extra, "MT") && !core.OpenEnded(o.Extra) && !core.Has(o.Extra, "L") && !core.Has(o.Extra, "QUIT")
	if activityEnabled {
		activityHandle, e = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
		if e == nil {
			defer windows.CloseHandle(activityHandle)
			activityBaseline, activityEnabled = readProcessWrites(activityHandle)
		} else {
			activityEnabled = false
		}
		if !activityEnabled {
			a.events <- event{text: "\r\nMT process I/O counters unavailable; copied bytes / ETA remain unknown.\r\n"}
		}
	}
	sampleActivity := func() {
		if activityEnabled {
			if n, ok := readProcessWrites(activityHandle); ok && n >= activityBaseline {
				a.events <- event{ioValid: true, ioBytes: n - activityBaseline}
			}
		}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var reader *os.File
	defer func() {
		if reader != nil {
			reader.Close()
		}
	}()
	decoder := core.LogDecoder{Unicode: selected.Unicode, ANSI: decodeANSI}
	var records auditio.Records
	var readErr error
	var captured bool
	emitText := func(text string) {
		auditio.EmitChunks(text, func(chunk string) bool {
			select {
			case a.events <- event{text: chunk, preview: preview}:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}
	consumeLine := func(line string) {
		if preview && !o.Expert && (!o.Custom || (!core.Has(o.Extra, "V") && !core.Has(o.Extra, "IS"))) {
			p.Add(line, o.Source)
		}
		select {
		case a.events <- event{line: line, preview: preview}:
		case <-ctx.Done():
		}
	}
	consumeText := func(text string, final bool) {
		emitText(text)
		if records.Feed(text, final, consumeLine) {
			p = core.Plan{Limited: true, LimitReason: "Native record exceeds the 32 KiB detailed telemetry budget; full captured text remains available subject to audit limits."}
			select {
			case a.events <- event{telemetryLimited: true, preview: preview}:
			case <-ctx.Done():
			}
		}
	}
	diagnostics := func() {
		f, err := os.Open(fallback.Name())
		if err != nil {
			return
		}
		defer f.Close()
		b, err := auditio.Tail(f)
		if err != nil {
			return
		}
		emitText("\r\nNative stdout diagnostics (bounded last 256 KiB, best effort; not telemetry):\r\n" + decodeANSI(b))
	}
	read := func(final bool) {
		if reader == nil {
			reader, e = os.Open(selected.Path)
			if e != nil {
				if final {
					diagnostics()
					readErr = fmt.Errorf("could not open native Robocopy log %q: %w", selected.Path, e)
				}
				return // Robocopy may not have created the log yet; retry next tick.
			}
			if _, err := auditio.Regular(reader); err != nil {
				readErr = err
				return
			}
			if _, e = reader.Seek(offset, io.SeekStart); e != nil {
				readErr = fmt.Errorf("could not seek Robocopy log: %w", e)
				return
			}
		}
		// Snapshot before the loop: concurrent growth cannot extend this drain.
		st, err := auditio.Regular(reader)
		if err != nil {
			readErr = err
			return
		}
		pos, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			readErr = err
			return
		}
		budget := st.Size() - pos
		if budget < 0 {
			readErr = fmt.Errorf("native log shrank during capture; audit incomplete")
			return
		}
		if !final && budget > 1<<20 {
			budget = 1 << 20
		}
		if budget > auditio.RunBytes {
			readErr = fmt.Errorf("native capture backlog exceeds 128 MiB")
			return
		}
		for budget > 0 {
			if ctx.Err() != nil {
				break
			}
			buf := make([]byte, 32768)
			if int64(len(buf)) > budget {
				buf = buf[:budget]
			}
			n, err := reader.Read(buf)
			budget -= int64(n)
			if n > 0 {
				captured = true
				text := decoder.Feed(buf[:n], final && err == io.EOF)
				if decoder.Err != nil {
					readErr = decoder.Err
					return
				}
				consumeText(text, false)
			}
			if err != nil {
				if err != io.EOF {
					readErr = fmt.Errorf("could not read Robocopy log: %w", err)
					a.events <- event{text: "\r\nLog read warning: " + err.Error() + "\r\n"}
				} else if budget > 0 {
					readErr = fmt.Errorf("native log changed before snapshot drain: %w", io.ErrUnexpectedEOF)
				}
				break
			}
			if n == 0 {
				break
			}
		}
		if final {
			text := decoder.Feed(nil, true)
			if decoder.Err != nil {
				readErr = decoder.Err
				return
			}
			consumeText(text, true)
		}
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	checkSpools := func() {
		for _, path := range []string{name, fallback.Name()} {
			if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() || st.Size() > auditio.RunBytes {
				captureErr = fmt.Errorf("native spool unsafe, unavailable or above 128 MiB; audit incomplete")
				cancel()
			}
		}
	}
	for {
		select {
		case <-tick.C:
			// Owned native writes are monitored; up to one interval of size
			// overshoot is possible before the owned process is stopped/waited.
			checkSpools()
			sampleActivity()
			read(false)
			if readErr != nil {
				captureErr = errors.Join(captureErr, readErr)
				cancel()
			}
		case err := <-done:
			checkSpools()
			captureErr = errors.Join(captureErr, spool.Err())
			if ctx.Err() != nil {
				captureErr = errors.Join(captureErr, fmt.Errorf("native capture interrupted before complete final drain"))
			}
			sampleActivity()
			read(true)
			if reader != nil && (!captured || readErr != nil) {
				diagnostics()
			}
			if readErr != nil {
				captureErr = errors.Join(captureErr, readErr)
				finish(-1, readErr)
				return
			}
			if ctx.Err() != nil {
				finish(-1, fmt.Errorf("cancelled; partial files can be resumed with /Z"))
				return
			}
			code := cmd.ProcessState.ExitCode()
			if err != nil && code < 0 {
				finish(code, err)
			} else {
				finish(code, nil)
			}
			return
		}
	}
}
