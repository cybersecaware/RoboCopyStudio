package scheduling

import "time"

// FromSelection preserves the native optional pickers' unset state. Dates are
// displayed day-first in the GUI, but stored ISO to keep existing jobs compatible.
// Use the selected wall-clock fields as-is, without converting time zones.
func FromSelection(kind string, date, clock time.Time) Spec {
	s := Spec{Kind: kind}
	if !date.IsZero() {
		s.Date = date.Format("2006-01-02")
	}
	if !clock.IsZero() {
		s.Time = clock.Format("15:04")
	}
	return s
}

func DisplayDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02/01/2006")
}
