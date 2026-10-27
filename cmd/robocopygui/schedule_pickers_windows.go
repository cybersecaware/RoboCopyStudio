//go:build windows

package main

import (
	"fmt"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// Walk exposes DateEdit, but not a time-style constructor. Create the same
// native widget with DTS_TIMEFORMAT at creation (it includes DTS_UPDOWN).
// Changing this style after creation is not a supported way to make a clock.
// Reuse Walk's date/time conversion, layout, notifications and disposal.
type scheduleClockEdit struct{ DateEdit }

func (c scheduleClockEdit) Create(builder *Builder) error {
	w := new(walk.DateEdit)
	if err := walk.InitWidget(w, builder.Parent(), "SysDateTimePick32", win.WS_TABSTOP|win.WS_VISIBLE|win.DTS_TIMEFORMAT|win.DTS_SHOWNONE, 0); err != nil {
		return err
	}
	if c.AssignTo != nil {
		*c.AssignTo = w
	}
	w.MustRegisterProperty("Date", walk.NewProperty(
		func() interface{} { return w.Date() },
		func(value interface{}) error {
			t, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("time picker requires a time value")
			}
			return w.SetDate(t)
		},
		w.DateChanged(),
	))
	return builder.InitWidget(c.DateEdit, w, func() error {
		if err := w.SetFormat("HH:mm"); err != nil {
			return err
		}
		if err := w.SetDate(time.Time{}); err != nil {
			return err
		}
		if c.OnDateChanged != nil {
			w.DateChanged().Attach(c.OnDateChanged)
		}
		return nil
	})
}
