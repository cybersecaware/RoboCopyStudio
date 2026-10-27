//go:build windows

package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"robocopygui/internal/scheduling"
)

func TestScheduleDateTimePickers(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var d *walk.Dialog
	var date, clock *walk.DateEdit
	err := (Dialog{AssignTo: &d, Title: "Picker regression", Layout: VBox{}, Children: []Widget{
		DateEdit{AssignTo: &date, Optional: true, Format: "dd'/'MM'/'yyyy"},
		scheduleClockEdit{DateEdit{AssignTo: &clock}},
	}}).Create(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Dispose()
	if !date.Date().IsZero() || !clock.Date().IsZero() {
		t.Fatal("pickers must start unselected")
	}
	if date.Format() != "dd'/'MM'/'yyyy" || clock.Format() != "HH:mm" {
		t.Fatalf("wrong formats: %s %s", date.Format(), clock.Format())
	}
	if win.GetWindowLong(clock.Handle(), win.GWL_STYLE)&win.DTS_UPDOWN == 0 {
		t.Fatal("native time spinner missing")
	}
	if win.GetWindowLong(date.Handle(), win.GWL_STYLE)&win.DTS_UPDOWN != 0 {
		t.Fatal("calendar must retain drop-down style")
	}
	selected := time.Date(2030, 12, 31, 0, 0, 0, 0, time.Local)
	late := time.Date(2030, 12, 31, 23, 59, 0, 0, time.Local)
	if err = date.SetDate(selected); err != nil {
		t.Fatal(err)
	}
	if err = clock.SetDate(late); err != nil {
		t.Fatal(err)
	}
	s := scheduling.FromSelection("once", date.Date(), clock.Date())
	if s.Date != "2030-12-31" || s.Time != "23:59" {
		t.Fatalf("wrong date/time conversion: %+v", s)
	}
	if err = clock.SetDate(selected); err != nil {
		t.Fatal(err)
	}
	if got := scheduling.FromSelection("once", date.Date(), clock.Date()).Time; got != "00:00" {
		t.Fatalf("midnight lost: %q", got)
	}
	if err = date.SetDate(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err = clock.SetDate(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !date.Date().IsZero() || !clock.Date().IsZero() {
		t.Fatal("clearing pickers did not preserve unset state")
	}
}
