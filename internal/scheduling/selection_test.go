package scheduling

import (
	"testing"
	"time"
)

func TestPickerSelection(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	date := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	clock := time.Date(2026, 10, 26, 23, 59, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, kind         string
		date, clock        time.Time
		wantDate, wantTime string
		immediate, fail    bool
	}{
		{"unset", "now", time.Time{}, time.Time{}, "", "", true, false},
		{"unset once", "once", time.Time{}, time.Time{}, "", "", true, false},
		{"date only", "once", date, time.Time{}, "2026-12-31", "", false, true},
		{"time only", "daily", time.Time{}, clock, "", "23:59", false, true},
		{"midnight selected", "once", date, midnight, "2026-12-31", "00:00", false, false},
		{"last minute", "daily", date, clock, "2026-12-31", "23:59", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := FromSelection(tc.kind, tc.date, tc.clock)
			if s.Date != tc.wantDate || s.Time != tc.wantTime {
				t.Fatalf("unexpected conversion: %+v", s)
			}
			_, immediate, err := s.Start(now)
			if (err != nil) != tc.fail || (err == nil && immediate != tc.immediate) {
				t.Fatalf("immediate=%v err=%v", immediate, err)
			}
		})
	}
	if got := DisplayDate("2026-12-31"); got != "31/12/2026" {
		t.Fatal(got)
	}
	if got := DisplayDate("2028-02-29"); got != "29/02/2028" {
		t.Fatal(got)
	}
}
