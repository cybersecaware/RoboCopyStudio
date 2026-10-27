//go:build windows

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"robocopygui/internal/core"
	"robocopygui/internal/scheduling"
)

func (a *app) scheduleDialog() {
	if a.busy {
		a.fail(fmt.Errorf("finish the current run before scheduling"))
		return
	}
	var d *walk.Dialog
	var kind, list *walk.ComboBox
	var date, clock *walk.DateEdit
	var status *walk.TextLabel
	var save, refresh, remove, closeButton *walk.PushButton
	var days [7]*walk.CheckBox
	var ids []string
	working := false
	setWorking := func(v bool) {
		working = v
		save.SetEnabled(!v)
		refresh.SetEnabled(!v)
		remove.SetEnabled(!v)
		closeButton.SetEnabled(!v)
	}
	// Dialog cannot close while a command owns its completion callback. All
	// schtasks calls and persistent-store enumeration run off the GUI thread.
	background := func(action func() ([]string, []string, error)) {
		setWorking(true)
		status.SetText("Working with Windows Task Scheduler…")
		go func() {
			newIDs, names, err := action()
			d.Synchronize(func() {
				setWorking(false)
				if err != nil {
					status.SetText("Not completed: " + err.Error())
					return
				}
				if newIDs != nil {
					ids = newIDs
					list.SetModel(names)
					if len(ids) > 0 {
						list.SetCurrentIndex(0)
					}
				}
				status.SetText("Completed and verified. Scheduled execution reports appear in History. Keep this executable at its current path.")
			})
		}()
	}
	reload := func() ([]string, []string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s, err := scheduleStore()
		if err != nil {
			return nil, nil, err
		}
		all, err := s.IDs("runs")
		if err != nil {
			return nil, nil, err
		}
		out := make([]string, 0)
		names := make([]string, 0)
		for _, id := range all {
			j, err := loadScheduledJob(s, id)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid scheduled snapshot %s: %w", id, err)
			}
			// Every displayed registration is checked against the exact owner/action.
			level, queryErr := queryScheduledJobRunLevelContext(ctx, id)
			state := "registered / normal-user permissions"
			if queryErr != nil {
				state = "unregistered / changed / unavailable (legacy tasks may require explicit admin access)"
			} else if level == "HighestAvailable" {
				state = "LEGACY HighestAvailable / may run elevated (unchanged)"
			}
			out = append(out, id)
			names = append(names, fmt.Sprintf("%s %s %s | %s | %s", j.Schedule.Kind, scheduling.DisplayDate(j.Schedule.Date), j.Schedule.Time, id[:8], state))
		}
		return out, names, nil
	}
	dayWidgets := []Widget{Label{Text: "Weekly days"}}
	for i, name := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
		dayWidgets = append(dayWidgets, CheckBox{AssignTo: &days[i], Text: name})
	}
	err := (Dialog{AssignTo: &d, Title: "Schedule Jobs", Icon: a.mw.Icon(), Background: SolidColorBrush{Color: walk.RGB(27, 31, 43)}, Size: Size{Width: 850, Height: 640}, MinSize: Size{Width: 820, Height: 620}, Font: Font{Family: "Segoe UI", PointSize: 10}, Layout: VBox{}, Children: []Widget{
		TextLabel{MinSize: Size{Width: 300}, Text: "Your Windows account must be logged in. The app need not stay open. New tasks use your interactive token with normal-user permissions (LeastPrivilege), even if created from an elevated app. No password is stored and schedules never auto-elevate. Protected paths and privileged options require permissions already granted to your normal account; network access must already be available to that session."},
		TextLabel{MinSize: Size{Width: 300}, Text: "Legacy HighestAvailable tasks may still run elevated and are labeled on refresh; they are never silently downgraded or deleted. To replace one, explicitly delete it and create a new schedule (renew destructive consent). Managing a legacy task may require manually restarting as administrator; no automatic elevation is attempted. Schedules use Windows LOCAL time / time zone, including its DST rules. Repeated or skipped clock times are controlled by Windows. Missed runs are not caught up. Overlaps are skipped. Jobs are immutable snapshots, not linked to saved profiles."},
		ComboBox{AssignTo: &kind, Model: []string{"Run once now (no task)", "One time (future)", "Daily", "Weekly"}, CurrentIndex: 0},
		Composite{Layout: Grid{Columns: 4}, Children: []Widget{
			Label{Text: "Start date (DD/MM/YYYY)"},
			DateEdit{AssignTo: &date, Name: "ScheduleStartDate", Accessibility: Accessibility{Name: "Schedule start date DD/MM/YYYY"}, Format: "dd'/'MM'/'yyyy", Optional: true, MinSize: Size{Width: 170, Height: 30}, ToolTipText: "Tick to select a date. Open the arrow for a mini calendar. Untick to clear."},
			Label{Text: "Local time (24-hour)"},
			scheduleClockEdit{DateEdit{AssignTo: &clock, Name: "ScheduleStartTime", Accessibility: Accessibility{Name: "Schedule time hours and minutes"}, MinSize: Size{Width: 135, Height: 30}, ToolTipText: "Tick to select a time. Select hours or minutes, then type or use the up/down arrows. Untick to clear."}},
		}},
		Composite{Layout: HBox{}, Children: dayWidgets},
		TextLabel{MinSize: Size{Width: 300}, Text: "Tick the date/time boxes to select them. Date uses DD/MM/YYYY with a drop-down calendar; time uses 24-hour HH:MM with hour/minute arrows. Leave both unticked for Run once now / One time to run once without a task. Daily/Weekly require both selections; Weekly also requires weekdays. Expert/JOB, monitoring, external logs, REG/SAVE and run-window flags are not supported."},
		PushButton{AssignTo: &save, Text: "Run now / Create schedule from current options…", OnClicked: func() {
			kinds := []string{"now", "once", "daily", "weekly"}
			idx := kind.CurrentIndex()
			if idx < 0 || idx >= len(kinds) {
				return
			}
			spec := scheduling.FromSelection(kinds[idx], date.Date(), clock.Date())
			for i, c := range days {
				if c.Checked() {
					spec.Days = append(spec.Days, i)
				}
			}
			_, now, err := spec.Start(time.Now())
			if err != nil {
				status.SetText(err.Error())
				return
			}
			if now {
				d.Accept()
				a.startClick()
				return
			}
			o, err := a.options()
			if err != nil {
				status.SetText(err.Error())
				return
			}
			if err = scheduling.ValidateOptions(o); err != nil {
				status.SetText(err.Error())
				return
			}
			authorized := false
			if scheduling.NeedsConsent(o) {
				fingerprint := core.Command(o.Extra) + o.Source + "\x00" + o.Destination
				if !a.ready || a.lastPreview != fingerprint {
					status.SetText("Destructive/persistent schedules require a successful Preview Mode (/L) of these exact endpoints and options first. Close this dialog, preview, then return.")
					return
				}
				if themedMsgBox(d, "Authorize scheduled destructive operations?", "Source: "+o.Source+"\nDestination: "+o.Destination+"\nOptions: "+core.Command(o.Extra)+"\n\nThis authorizes these destructive options on EVERY scheduled execution, including recurring runs without another prompt. MIR/PURGE can delete destination data; MOV/MOVE delete source data; M resets archive bits. Files may change after preview. Each execution validates endpoints and runs a bounded fresh preflight, but this is not content verification. Delete this task to revoke future execution (an already-running copy is not stopped).\n\nAuthorize this immutable schedule?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
					return
				}
				authorized = true
			}
			id, err := core.NewID()
			if err != nil {
				status.SetText(err.Error())
				return
			}
			j := scheduling.Job{Version: scheduling.Version, ID: id, Created: time.Now().UTC(), Schedule: spec, Options: o, Authorized: authorized}
			background(func() ([]string, []string, error) {
				if err := installScheduledJob(j); err != nil {
					return nil, nil, err
				}
				return reload()
			})
		}},
		TextLabel{MinSize: Size{Width: 300}, Text: "OWN scheduled snapshots (registration is checked on refresh; changed/foreign tasks cannot be deleted here)"},
		ComboBox{AssignTo: &list},
		TextLabel{MinSize: Size{Width: 300}, AssignTo: &status, Text: "Choose an explicit schedule, or leave the default for a one-off normal run.", StretchFactor: 1},
		Composite{Layout: HBox{}, Children: []Widget{
			PushButton{AssignTo: &refresh, Text: "Refresh own jobs", OnClicked: func() { background(reload) }},
			PushButton{AssignTo: &remove, Text: "Delete selected task…", OnClicked: func() {
				idx := list.CurrentIndex()
				if idx < 0 || idx >= len(ids) {
					return
				}
				id := ids[idx]
				if themedMsgBox(d, "Delete selected scheduled job?", "Delete only this RoboCopy Studio task and its snapshot? No endpoint data or run history is deleted. An already-running copy is NOT stopped.\n\n"+id, walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
					return
				}
				background(func() ([]string, []string, error) {
					if err := deleteScheduledJob(id); err != nil {
						return nil, nil, err
					}
					return reload()
				})
			}}, HSpacer{}, PushButton{AssignTo: &closeButton, Text: "Close", OnClicked: func() { d.Cancel() }},
		}},
	}}).Create(a.mw)
	if err != nil {
		a.fail(err)
		return
	}
	d.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if working {
			*canceled = true
		}
	})
	darken(d)
	darkTitleBar(d.Handle())
	d.Run()
}
