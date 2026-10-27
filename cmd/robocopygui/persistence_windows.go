//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
	"robocopygui/internal/core"
)

func defaultStore() (core.Store, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return core.Store{}, fmt.Errorf("LOCALAPPDATA is unavailable; persistent audit storage is required")
	}
	s := core.Store{Root: filepath.Join(root, "RoboCopyStudio")}
	return s, s.Secure()
}
func (a *app) refreshProfiles() {
	ids, e := a.store.IDs("profiles")
	if e != nil {
		a.fail(e)
		return
	}
	a.profileIDs = nil
	var names []string
	for _, id := range ids {
		p, e := a.store.LoadProfile(id)
		if e != nil {
			names = append(names, "[invalid profile] "+id)
		} else {
			names = append(names, p.Name)
		}
		a.profileIDs = append(a.profileIDs, id)
	}
	a.profiles.SetModel(names)
	if len(names) > 0 {
		a.profiles.SetCurrentIndex(0)
	}
}
func (a *app) selectedProfile() string {
	i := a.profiles.CurrentIndex()
	if i < 0 || i >= len(a.profileIDs) {
		return ""
	}
	return a.profileIDs[i]
}
func (a *app) saveProfile() {
	if a.busy {
		return
	}
	var d *walk.Dialog
	var name *walk.LineEdit
	p := core.Profile{SchemaVersion: core.StoreVersion, Name: "New profile", Source: a.src.Text(), Destination: a.dst.Text(), Raw: a.raw.Text(), Mirror: a.mirror.Checked(), Restart: a.restart.Checked(), MT: a.threaded.Checked(), Workers: int(a.threads.Value()), Expert: a.expert.Checked()}
	if e := p.Validate(); e != nil {
		a.fail(e)
		return
	}
	e := (Dialog{AssignTo: &d, Title: "Save profile (new snapshot)", Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Size: Size{Width: 520, Height: 220}, Font: Font{Family: "Segoe UI", PointSize: 10}, Layout: VBox{}, Children: []Widget{
		TextLabel{Text: "Saves endpoints and flags only. No approval or execution is saved. Use a new name; credentials must not be entered in flags."},
		LineEdit{AssignTo: &name, Text: "New profile"},
		Composite{Layout: HBox{}, Children: []Widget{HSpacer{}, PushButton{Text: "Save", OnClicked: func() {
			usage, err := a.storeUsage()
			if err != nil {
				a.fail(err)
				return
			}
			if usage > auditio.StoreBytes-(4<<20) {
				a.fail(fmt.Errorf("audit store budget reached; explicitly delete selected old history"))
				return
			}
			p.Name = strings.TrimSpace(name.Text())
			id, e := core.NewID()
			if e == nil {
				e = a.store.SaveProfile(id, p)
			}
			if e != nil {
				a.fail(e)
				return
			}
			a.refreshProfiles()
			for i, v := range a.profileIDs {
				if v == id {
					a.profiles.SetCurrentIndex(i)
				}
			}
			d.Accept()
		}}, PushButton{Text: "Cancel", OnClicked: func() { d.Cancel() }}}},
	}}).Create(a.mw)
	if e != nil {
		a.fail(e)
		return
	}
	darken(d)
	darkTitleBar(d.Handle())
	d.Run()
}
func (a *app) loadProfile() {
	if a.busy {
		return
	}
	id := a.selectedProfile()
	if id == "" {
		return
	}
	p, e := a.store.LoadProfile(id)
	if e != nil {
		a.fail(e)
		return
	}
	// Suppress intermediate refresh; always invalidate the old destructive approval.
	a.initializing = true
	a.src.SetText(p.Source)
	a.dst.SetText(p.Destination)
	a.raw.SetText(p.Raw)
	a.mirror.SetChecked(p.Mirror)
	a.restart.SetChecked(p.Restart)
	a.threaded.SetChecked(p.MT)
	a.threads.SetValue(float64(p.Workers))
	a.expert.SetChecked(p.Expert)
	a.initializing = false
	a.plan = core.NewPlan()
	a.invalidate()
	a.status.SetText("Profile loaded — not executed. Preview required again for destructive modes.")
}
func (a *app) deleteProfile() {
	if a.busy {
		return
	}
	id := a.selectedProfile()
	if id == "" {
		return
	}
	if themedMsgBox(a.mw, "Delete saved profile?", "Delete only the selected saved profile? No endpoint data is touched.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
		return
	}
	if e := a.store.DeleteProfile(id); e != nil {
		a.fail(e)
		return
	}
	a.refreshProfiles()
}
func (a *app) historyDialog() {
	var d *walk.Dialog
	var list *walk.ComboBox
	var report *walk.TextEdit
	var records []core.RunRecord
	selected := func() *core.RunRecord {
		if list == nil {
			return nil
		}
		i := list.CurrentIndex()
		if i < 0 || i >= len(records) {
			return nil
		}
		return &records[i]
	}
	display := func() {
		r := selected()
		if r == nil {
			return
		}
		var b strings.Builder
		if r.AuditError != "" {
			b.WriteString("AUDIT INCOMPLETE: " + r.AuditError + "\r\n")
		}
		b.WriteString(core.RunDescription(*r) + "\r\n\r\nCommand: " + r.Command + "\r\nEnded: " + r.Ended.Format(time.RFC3339) + "\r\nError: " + r.Error + fmt.Sprintf("\r\nPlanned: %d files / %d bytes\r\nNative summary: %v\r\n", r.PlannedFiles, r.PlannedBytes, r.Summary))
		b.WriteString("\r\nFailure evidence (English native ERROR records only; unknown/localized failures remain in log):\r\n")
		for _, f := range r.Failures {
			b.WriteString(f.Evidence + "\r\n")
		}
		if len(r.Failures) == 0 {
			b.WriteString("No recognized path evidence. This does not establish that no files failed.\r\n")
		}
		_, e := a.store.Path("runs", r.ID, ".log")
		if e == nil {
			f, err := a.openAuditLog(r.ID)
			if err == nil {
				data, _ := io.ReadAll(io.LimitReader(f, 512<<10))
				f.Close()
				b.WriteString("\r\n--- Log preview (first 512 KiB; Open full log for complete output) ---\r\n" + string(data))
			} else {
				b.WriteString("\r\nLog unavailable: " + err.Error())
			}
		} else {
			b.WriteString("\r\nUnsafe log path rejected: " + e.Error())
		}
		report.SetText(b.String())
	}
	reload := func() {
		ids, e := a.store.IDs("runs")
		if e != nil {
			a.fail(e)
			return
		}
		records = nil
		invalid := 0
		for _, id := range ids {
			r, e := a.store.LoadRun(id)
			if e == nil {
				records = append(records, r)
			} else {
				invalid++
			}
		}
		sort.Slice(records, func(i, j int) bool { return records[i].Started.After(records[j].Started) })
		var names []string
		for _, r := range records {
			names = append(names, core.RunListTitle(r))
		}
		list.SetModel(names)
		if len(records) > 0 {
			list.SetCurrentIndex(0)
			display()
		} else {
			report.SetText("No saved run records.")
		}
		if invalid > 0 {
			a.fail(fmt.Errorf("%d invalid history records skipped; store files were not deleted", invalid))
		}
	}
	e := (Dialog{AssignTo: &d, Title: "Run history — reports only (never reruns)", Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Size: Size{Width: 1000, Height: 700}, MinSize: Size{Width: 760, Height: 500}, Font: Font{Family: "Segoe UI", PointSize: 10}, Layout: VBox{}, Children: []Widget{
		TextLabel{Text: "Persistent per-user logs contain endpoints, filenames and commands. No automatic retention deletion. Running records after a crash mean unfinished, not success."},
		ComboBox{AssignTo: &list, OnCurrentIndexChanged: display}, TextEdit{AssignTo: &report, ReadOnly: true, VScroll: true, StretchFactor: 1, Font: Font{Family: "Consolas", PointSize: 10}},
		Composite{Layout: HBox{}, Children: []Widget{PushButton{Text: "Open full log", OnClicked: func() {
			r := selected()
			if r == nil {
				return
			}
			p, e := a.store.Path("runs", r.ID, ".log")
			if e == nil {
				sys, err := windows.GetSystemDirectory()
				e = err
				if e == nil {
					e = startDetached(exec.Command(filepath.Join(sys, "notepad.exe"), p))
				}
			}
			if e != nil {
				a.fail(e)
			}
		}}, PushButton{Text: "Delete selected history + log", Enabled: !a.busy, OnClicked: func() {
			r := selected()
			if r == nil || a.busy {
				return
			}
			if themedMsgBox(d, "Delete selected audit record?", "Permanently remove only this history record and its managed log? Endpoint files are not touched.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
				return
			}
			if e := a.store.DeleteRun(r.ID); e != nil {
				a.fail(e)
				return
			}
			reload()
		}}, HSpacer{}, PushButton{Text: "Close", OnClicked: func() { d.Accept() }}}},
	}}).Create(a.mw)
	if e != nil {
		a.fail(e)
		return
	}
	darken(d)
	darkTitleBar(d.Handle())
	reload()
	d.Run()
}
func (a *app) finishAudit(e event) error {
	finalErr := a.auditErr
	if a.fullLog != nil {
		finalErr = auditio.Finalize(a.fullLog, finalErr)
		a.fullLog = nil
	}
	if e.auditError != nil {
		finalErr = errors.Join(finalErr, e.auditError)
	}
	a.record.Ended = time.Now().UTC()
	if e.preview {
		a.record.PlannedBytes = e.plan.Bytes
		a.record.PlannedFiles = len(e.plan.Files)
	}
	a.record.ExitCode = e.code
	a.record.Status = core.Outcome(e.code)
	if a.auditErr != nil {
		a.record.AuditError = a.auditErr.Error()
	}
	if e.err != nil {
		a.record.Error = e.err.Error()
		a.record.Status = "Stopped / could not run (partial files may remain)"
	}
	if e.preview && e.err == nil && e.code < 8 {
		a.record.Status = "Preview completed (no transfer)"
	}
	// Read errors must not prevent a terminal manifest from being attempted.
	f, err := a.openAuditLog(a.record.ID)
	if err == nil {
		var b []byte
		b, err = auditio.Tail(f)
		err = errors.Join(err, f.Close())
		if err == nil {
			a.record.Summary = core.Summary(string(b))
		}
	}
	finalErr = errors.Join(finalErr, err)
	if finalErr != nil {
		a.record.AuditError = finalErr.Error()
		a.record.Status = "Audit incomplete (partial capture; review native exit status)"
	}
	a.auditErr = finalErr
	return errors.Join(finalErr, a.store.SaveRun(a.record))
}
