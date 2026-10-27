package scheduling

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"robocopygui/internal/core"
)

func testJob() Job {
	return Job{Version: Version, ID: strings.Repeat("a", 32), Created: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Schedule: Spec{Kind: "once", Date: "2030-12-31", Time: "20:15"}, Options: core.Options{Source: `C:\source & 文件`, Destination: `D:\target`, Custom: true, Extra: []string{"/E", "/R:2", "/W:2", "/BYTES", "/FP"}}}
}
func TestScheduleSelections(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		s               Spec
		immediate, fail bool
	}{
		{"default", Spec{Kind: "now"}, true, false},
		{"empty one time", Spec{Kind: "once"}, true, false},
		{"once", Spec{Kind: "once", Date: "2026-10-27", Time: "08:30"}, false, false},
		{"daily", Spec{Kind: "daily", Date: "2026-10-26", Time: "08:30"}, false, false},
		{"weekly", Spec{Kind: "weekly", Date: "2026-10-26", Time: "08:30", Days: []int{1, 5}}, false, false},
		{"daily empty", Spec{Kind: "daily"}, false, true},
		{"weekly empty", Spec{Kind: "weekly"}, false, true},
		{"partial time", Spec{Kind: "once", Time: "08:30"}, false, true},
		{"partial date", Spec{Kind: "once", Date: "2026-10-27"}, false, true},
		{"invalid day", Spec{Kind: "once", Date: "2026-02-30", Time: "08:30"}, false, true},
		{"invalid hour", Spec{Kind: "once", Date: "2026-10-27", Time: "24:00"}, false, true},
		{"loose time", Spec{Kind: "once", Date: "2026-10-27", Time: "8:30"}, false, true},
		{"past once", Spec{Kind: "once", Date: "2026-10-26", Time: "11:30"}, false, true},
		{"past daily start", Spec{Kind: "daily", Date: "2026-10-25", Time: "11:30"}, false, true},
		{"weekly no days", Spec{Kind: "weekly", Date: "2026-10-26", Time: "13:00"}, false, true},
		{"weekly invalid day", Spec{Kind: "weekly", Date: "2026-10-26", Time: "13:00", Days: []int{7}}, false, true},
		{"weekly duplicate", Spec{Kind: "weekly", Date: "2026-10-26", Time: "13:00", Days: []int{1, 1}}, false, true},
		{"daily with days", Spec{Kind: "daily", Date: "2026-10-26", Time: "13:00", Days: []int{1}}, false, true},
		{"now with date", Spec{Kind: "now", Date: "2026-10-27", Time: "13:00"}, false, true},
		{"unknown", Spec{Kind: "monthly", Date: "2026-10-27", Time: "13:00"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, immediate, err := tt.s.Start(now)
			if (err != nil) != tt.fail {
				t.Fatalf("error=%v expected fail=%v", err, tt.fail)
			}
			if err == nil && immediate != tt.immediate {
				t.Fatalf("immediate=%v", immediate)
			}
		})
	}
}
func TestScheduleDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	for _, s := range []Spec{{Kind: "once", Date: "2026-03-08", Time: "02:30"}, {Kind: "once", Date: "2026-11-01", Time: "01:30"}} {
		if _, _, err := s.Start(now); err == nil {
			t.Fatalf("accepted nonexistent/ambiguous time %+v", s)
		}
	}
	s := Spec{Kind: "daily", Date: "2026-03-08", Time: "03:30"}
	if _, _, err := s.Start(now); err != nil {
		t.Fatal(err)
	}
}
func TestScheduledOptionsRejectOpaqueOrPersistent(t *testing.T) {
	base := testJob().Options
	if err := ValidateOptions(base); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"/JOB:x", "/MON:1", "/MOT:3", "/SAVE:test", "/REG", "/LOG:C:\\x", "/UNILOG+:C:\\x", "/RH:1200-1300", "/PF", "/TBD", "/L", "/QUIT", "/NOSD", "/UNKNOWN", `C:\other`, `\\server\share`} {
		o := base
		o.Extra = append(append([]string{}, base.Extra...), arg)
		if ValidateOptions(o) == nil {
			t.Errorf("accepted %q", arg)
		}
	}
	o := base
	o.Expert = true
	if ValidateOptions(o) == nil {
		t.Fatal("expert accepted")
	}
	for _, flag := range []string{"/MIR", "/PURGE", "/MOVE", "/MOV", "/M"} {
		j := testJob()
		j.Options.Extra = append(j.Options.Extra, flag)
		if !NeedsConsent(j.Options) {
			t.Errorf("missing consent %s", flag)
		}
		if j.Validate(j.ID) == nil {
			t.Errorf("accepted unapproved %s", flag)
		}
		j.Authorized = true
		if err := j.Validate(j.ID); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTaskXMLAndOwnership(t *testing.T) {
	j := testJob()
	exe := `C:\Program Files\Robo & <Studio>\程序.exe`
	sid := "S-1-5-21-100-200-300-1001"
	for _, kind := range []string{"once", "daily", "weekly"} {
		t.Run(kind, func(t *testing.T) {
			j.Schedule.Kind = kind
			j.Schedule.Days = nil
			if kind == "weekly" {
				j.Schedule.Days = []int{0, 1, 6}
			}
			b, err := XML(j, exe, sid)
			if err != nil {
				t.Fatal(err)
			}
			if len(b)%2 != 0 || b[0] != 255 || b[1] != 254 {
				t.Fatal("not UTF16LE with BOM")
			}
			text := core.DecodeLog(b)
			for _, expected := range []string{"IgnoreNew", "InteractiveToken", "<RunLevel>LeastPrivilege</RunLevel>", "<StartWhenAvailable>false", "2030-12-31T20:15:00", "&amp;", "&lt;Studio&gt;"} {
				if !strings.Contains(text, expected) {
					t.Errorf("missing %s", expected)
				}
			}
			if kind == "weekly" && !strings.Contains(text, "<Sunday/><Monday/><Saturday/>") {
				t.Fatal("weekday selection lost")
			}
			if err := Owned(b, j.ID, exe, sid); err != nil {
				t.Fatal(err)
			}
			if err := Owned([]byte(text), j.ID, exe, sid); err != nil {
				t.Fatal(err)
			}
			for _, mutated := range []string{strings.Replace(text, "</Actions>", "<ComHandler><ClassId>{00000000-0000-0000-0000-000000000000}</ClassId></ComHandler></Actions>", 1), strings.Replace(text, "</Actions>", "<SendEmail><To>fixture.invalid</To></SendEmail></Actions>", 1), strings.Replace(text, "--scheduled-job", "--other", 1), strings.Replace(text, "InteractiveToken", "Password", 1), strings.Replace(text, "LeastPrivilege", "Administrator", 1), strings.Replace(text, "<Actions Context=\"Owner\">", "<Actions Context=\"Owner\"><Exec><Command>evil.exe</Command></Exec>", 1)} {
				if Owned([]byte(mutated), j.ID, exe, sid) == nil {
					t.Fatal("accepted changed action/principal")
				}
			}
			if Owned(b, strings.Repeat("b", 32), exe, sid) == nil {
				t.Fatal("accepted foreign ID")
			}
			var task struct {
				Command   string `xml:"Actions>Exec>Command"`
				Arguments string `xml:"Actions>Exec>Arguments"`
			}
			utf8 := bytes.Replace([]byte(text), []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`), 1)
			if err := xml.Unmarshal(utf8, &task); err != nil {
				t.Fatal(err)
			}
			if task.Command != exe {
				t.Fatal("XML escaping did not roundtrip")
			}
			args, err := core.SplitCommandLine(task.Arguments)
			if err != nil || len(args) != 2 || args[1] != j.ID {
				t.Fatalf("bad argv: %v %v", args, err)
			}
		})
	}
	for _, bad := range []string{`relative.exe`, `\\host\app.exe`, `C:\bad".exe`} {
		if _, err := XML(testJob(), bad, sid); err == nil {
			t.Errorf("accepted executable %q", bad)
		}
	}
}
func TestTaskRunLevelCompatibility(t *testing.T) {
	j := testJob()
	exe, sid := `C:\Program Files\RoboCopyStudio.exe`, "S-1-5-21-100-200-300-1001"
	b, err := XML(j, exe, sid)
	if err != nil {
		t.Fatal(err)
	}
	text := core.DecodeLog(b)
	if strings.Contains(text, "HighestAvailable") {
		t.Fatal("new task requests legacy elevation")
	}
	for _, level := range []string{"LeastPrivilege", "HighestAvailable", "Administrator", "System", "highestavailable", "leastprivilege", "0", "1", "", " HighestAvailable "} {
		t.Run("level="+level, func(t *testing.T) {
			x := []byte(strings.Replace(text, "LeastPrivilege", level, 1))
			got, err := OwnedRunLevel(x, j.ID, exe, sid)
			accepted := level == "LeastPrivilege" || level == "HighestAvailable"
			if (err == nil) != accepted || (err == nil && got != level) || (err != nil && got != "") {
				t.Fatalf("level=%q returned %q, %v", level, got, err)
			}
			if (Owned(x, j.ID, exe, sid) == nil) != accepted {
				t.Fatal("ownership wrapper disagrees")
			}
			if !accepted {
				return
			}
			// Compatibility never relaxes exact action, SID or logon checks.
			for _, mutation := range []struct{ old, new string }{
				{sid, "S-1-5-21-100-200-300-1002"},
				{exe, `C:\foreign.exe`},
				{"--scheduled-job", "--other"},
				{"InteractiveToken", "Password"},
				{"</Principals>", "<Principal><UserId>" + sid + "</UserId></Principal></Principals>"},
			} {
				changed := []byte(strings.Replace(string(x), mutation.old, mutation.new, 1))
				if got, err := OwnedRunLevel(changed, j.ID, exe, sid); err == nil || got != "" {
					t.Fatalf("accepted changed principal/action for %s: %s", level, mutation.old)
				}
			}
		})
	}
	// Windows omits the schema-default RunLevel for genuine non-admin users.
	missing := strings.Replace(text, "<RunLevel>LeastPrivilege</RunLevel>", "", 1)
	if got, err := OwnedRunLevel([]byte(missing), j.ID, exe, sid); err != nil || got != "LeastPrivilege" {
		t.Fatalf("omitted schema-default run level returned %q, %v", got, err)
	}
	// Existing v1 snapshots remain valid, without migration or renewal of their
	// DPAPI authorization solely because task run level is now inspected.
	if Version != 1 || j.Validate(j.ID) != nil {
		t.Fatal("version 1 compatibility changed")
	}
}

func TestSnapshotIdentity(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("a", 31), strings.Repeat("A", 32), "../" + strings.Repeat("a", 32), strings.Repeat("0", 33)} {
		if ValidID(id) {
			t.Errorf("bad ID accepted %s", id)
		}
	}
	j := testJob()
	j.Version = 2
	if j.Validate(j.ID) == nil {
		t.Fatal("unknown version accepted")
	}
	j = testJob()
	j.Schedule = Spec{Kind: "now"}
	if j.Validate(j.ID) == nil {
		t.Fatal("immediate snapshot accepted")
	}
}
