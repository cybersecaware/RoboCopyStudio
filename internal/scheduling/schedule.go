// Package scheduling defines immutable scheduled-job snapshots and Task Scheduler XML.
package scheduling

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"robocopygui/internal/core"
)

const Version = 1
const Prefix = `\RoboCopyStudio-`

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }

type Spec struct {
	Kind string `json:"kind"`
	Date string `json:"date"`
	Time string `json:"time"`
	Days []int  `json:"days,omitempty"` // Sunday=0
}
type Job struct {
	Version    int          `json:"version"`
	ID         string       `json:"id"`
	Created    time.Time    `json:"created"`
	Schedule   Spec         `json:"schedule"`
	Options    core.Options `json:"options"`
	Authorized bool         `json:"authorized"` // DPAPI-protected, never stored in ordinary profiles.
}

// Start validates explicit selections. Only an unscheduled/empty one-time selection runs now.
func (s Spec) Start(now time.Time) (time.Time, bool, error) {
	if (s.Kind == "now" || s.Kind == "once") && s.Date == "" && s.Time == "" && len(s.Days) == 0 {
		return time.Time{}, true, nil
	}
	if s.Kind != "once" && s.Kind != "daily" && s.Kind != "weekly" {
		return time.Time{}, false, fmt.Errorf("choose Run once now, One time, Daily or Weekly")
	}
	if s.Date == "" || s.Time == "" {
		return time.Time{}, false, fmt.Errorf("selected schedule requires both a start date and time (HH:MM)")
	}
	text := s.Date + " " + s.Time
	t, err := time.ParseInLocation("2006-01-02 15:04", text, now.Location())
	if err != nil || t.Format("2006-01-02 15:04") != text {
		return time.Time{}, false, fmt.Errorf("invalid local date/time (including nonexistent DST times)")
	}
	// Ambiguous fall-back local times should not silently pick an occurrence.
	for delta := -180; delta <= 180; delta++ {
		if delta != 0 && t.Add(time.Duration(delta)*time.Minute).Format("2006-01-02 15:04") == text {
			return time.Time{}, false, fmt.Errorf("ambiguous DST time; choose another local time")
		}
	}
	if s.Kind == "once" && !t.After(now) {
		return time.Time{}, false, fmt.Errorf("one-time schedule must be in the future")
	}
	if s.Kind != "once" && s.Date < now.Format("2006-01-02") {
		return time.Time{}, false, fmt.Errorf("start date cannot precede today")
	}
	if s.Kind == "weekly" {
		if len(s.Days) == 0 {
			return time.Time{}, false, fmt.Errorf("weekly schedule requires at least one weekday")
		}
		seen := map[int]bool{}
		for _, d := range s.Days {
			if d < 0 || d > 6 || seen[d] {
				return time.Time{}, false, fmt.Errorf("invalid or duplicate weekday")
			}
			seen[d] = true
		}
	} else if len(s.Days) > 0 {
		return time.Time{}, false, fmt.Errorf("weekdays apply only to Weekly")
	}
	return t, false, nil
}

func ValidateOptions(o core.Options) error {
	if o.Expert || !o.Custom || o.Log != "" || o.Preview || o.Mirror {
		return fmt.Errorf("scheduling requires managed explicit options (no expert or opaque job endpoints)")
	}
	if _, err := core.Build(o, false); err != nil {
		return err
	}
	// Persistent writes outside managed capture are intentionally unsupported: /L is not a sandbox.
	for _, k := range []string{"JOB", "NOSD", "NODD", "MON", "MOT", "SAVE", "REG", "LOG", "LOG+", "UNILOG", "UNILOG+", "RH", "PF", "TBD", "L", "QUIT"} {
		if core.Has(o.Extra, k) {
			return fmt.Errorf("/%s is not supported for scheduled jobs", k)
		}
	}
	// Bare file specifications may not smuggle endpoints or credentials.
	for _, a := range o.Extra {
		if core.SwitchName(a) == "" && strings.ContainsAny(a, `\/:`) {
			return fmt.Errorf("scheduled file specifications must be simple names/patterns, not paths")
		}
	}
	return nil
}
func NeedsConsent(o core.Options) bool { return core.Destructive(o.Extra) || core.SideEffects(o.Extra) }
func (j Job) Validate(id string) error {
	if j.Version != Version || j.ID != id || !ValidID(id) || j.Created.IsZero() {
		return fmt.Errorf("invalid scheduled snapshot identity/version")
	}
	if err := ValidateOptions(j.Options); err != nil {
		return err
	}
	if NeedsConsent(j.Options) && !j.Authorized {
		return fmt.Errorf("scheduled destructive authorization missing")
	}
	// Validate historical boundaries without requiring that an old one-time job is still in the future.
	t, err := time.ParseInLocation("2006-01-02", j.Schedule.Date, time.Local)
	if err != nil {
		return err
	}
	_, now, err := j.Schedule.Start(t.Add(-time.Second))
	if err != nil {
		return err
	}
	if now {
		return fmt.Errorf("immediate jobs must not be persisted")
	}
	return nil
}
func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func XML(j Job, executable, sid string) ([]byte, error) {
	if err := j.Validate(j.ID); err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[A-Za-z]:\\`).MatchString(executable) || strings.ContainsAny(executable, "\x00\r\n\"") {
		return nil, fmt.Errorf("scheduled executable must be an absolute local Windows path")
	}
	if !regexp.MustCompile(`^S-1-[0-9-]+$`).MatchString(sid) {
		return nil, fmt.Errorf("invalid current-user SID")
	}
	boundary := escape(j.Schedule.Date + "T" + j.Schedule.Time + ":00")
	trigger := `<TimeTrigger><StartBoundary>` + boundary + `</StartBoundary><Enabled>true</Enabled></TimeTrigger>`
	if j.Schedule.Kind == "daily" {
		trigger = `<CalendarTrigger><StartBoundary>` + boundary + `</StartBoundary><Enabled>true</Enabled><ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger>`
	}
	if j.Schedule.Kind == "weekly" {
		names := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		days := ""
		for _, d := range j.Schedule.Days {
			days += "<" + names[d] + "/>"
		}
		trigger = `<CalendarTrigger><StartBoundary>` + boundary + `</StartBoundary><Enabled>true</Enabled><ScheduleByWeek><WeeksInterval>1</WeeksInterval><DaysOfWeek>` + days + `</DaysOfWeek></ScheduleByWeek></CalendarTrigger>`
	}
	text := `<?xml version="1.0" encoding="UTF-16"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><RegistrationInfo><Description>RoboCopy Studio immutable job v1</Description><URI>` + escape(Prefix+j.ID) + `</URI></RegistrationInfo><Triggers>` + trigger + `</Triggers><Principals><Principal id="Owner"><UserId>` + escape(sid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><AllowHardTerminate>false</AllowHardTerminate><StartWhenAvailable>false</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><Enabled>true</Enabled></Settings><Actions Context="Owner"><Exec><Command>` + escape(executable) + `</Command><Arguments>` + escape(core.Command([]string{"--scheduled-job", j.ID})) + `</Arguments></Exec></Actions></Task>`
	units := utf16.Encode([]rune(text))
	b := make([]byte, 2+len(units)*2)
	binary.LittleEndian.PutUint16(b, 0xfeff)
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2+i*2:], u)
	}
	return b, nil
}

// Owned rejects similarly named foreign tasks and changed actions/principals.
// Legacy HighestAvailable tasks remain manageable without changing their privilege.
func Owned(data []byte, id, executable, sid string) error {
	_, err := OwnedRunLevel(data, id, executable, sid)
	return err
}

// OwnedRunLevel returns the verified task's explicit run level, never an unverified
// elevation claim. Both current LeastPrivilege and legacy HighestAvailable are
// accepted; callers must not silently rewrite or migrate legacy registrations.
func OwnedRunLevel(data []byte, id, executable, sid string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid job ID")
	}
	if len(data) > 2 && (data[0] == 255 && data[1] == 254) {
		data = []byte(core.DecodeLog(data))
	}
	// schtasks may emit a UTF-16 declaration even when its redirected text is UTF-8.
	data = bytes.Replace(data, []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`), 1)
	var t struct {
		URI        string `xml:"RegistrationInfo>URI"`
		Principals []struct {
			SID   string  `xml:"UserId"`
			Logon string  `xml:"LogonType"`
			Level *string `xml:"RunLevel"`
		} `xml:"Principals>Principal"`
		Actions struct {
			Exec []struct {
				Command   string `xml:"Command"`
				Arguments string `xml:"Arguments"`
			} `xml:"Exec"`
			Other []struct{ XMLName xml.Name } `xml:",any"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal(data, &t); err != nil {
		return "", err
	}
	if t.URI != Prefix+id || len(t.Actions.Exec) != 1 || len(t.Actions.Other) != 0 || len(t.Principals) != 1 {
		return "", fmt.Errorf("task is not an owned RoboCopy Studio task")
	}
	a, p := t.Actions.Exec[0], t.Principals[0]
	// RunLevel is optional with schema default LeastPrivilege. The Windows
	// service omits it when exporting tasks for genuinely non-admin accounts.
	// Keep an explicitly empty/unknown element invalid, unlike an absent one.
	level := "LeastPrivilege"
	if p.Level != nil {
		level = *p.Level
	}
	if !strings.EqualFold(a.Command, executable) || a.Arguments != core.Command([]string{"--scheduled-job", id}) || p.SID != sid || p.Logon != "InteractiveToken" || (level != "LeastPrivilege" && level != "HighestAvailable") {
		return "", fmt.Errorf("task action or owner differs; refusing to manage")
	}
	return level, nil
}
