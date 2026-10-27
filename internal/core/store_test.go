package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtureProfile() Profile {
	return Profile{SchemaVersion: 1, Name: "日本語 daily copy", Source: `C:\source space`, Destination: `D:\destination`, Raw: `*.txt /E /BYTES /FP /XF "skip me.txt"`, Restart: true, MT: true, Workers: 4}
}
func TestRunListTitleStaysCompact(t *testing.T) {
	r := RunRecord{Started: time.Unix(0, 0), Source: strings.Repeat("source", 1000), Destination: strings.Repeat("destination", 1000), Status: strings.Repeat("verbose", 1000), ExitCode: 9}
	for _, want := range []string{"Failed", "Unfinished", "Stopped / error"} {
		title := RunListTitle(r)
		if len(title) > 100 || !strings.Contains(title, want) || strings.ContainsAny(title, "\r\n") {
			t.Fatalf("invalid compact title %q; want %q", title, want)
		}
		r.Status = "running"
		if want == "Unfinished" {
			r.Status = "Stopped"
			r.Error = "context canceled"
		}
	}
	r.Preview = true
	r.AuditError = "write failure"
	if title := RunListTitle(r); !strings.Contains(title, "Preview") || !strings.Contains(title, "audit incomplete") || len(title) > 100 {
		t.Fatal(title)
	}
}

func TestProfilePersistence(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "store")}
	id, e := NewID()
	if e != nil {
		t.Fatal(e)
	}
	p := fixtureProfile()
	if e = s.SaveProfile(id, p); e != nil {
		t.Fatal(e)
	}
	got, e := s.LoadProfile(id)
	if e != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("roundtrip %v %v", got, e)
	}
	p.Name = "new snapshot"
	if e = s.SaveProfile(id, p); e != nil {
		t.Fatal("atomic replacement", e)
	}
	got, e = s.LoadProfile(id)
	if e != nil || got.Name != p.Name {
		t.Fatal(got, e)
	}
	ids, e := s.IDs("profiles")
	if e != nil || len(ids) != 1 || ids[0] != id {
		t.Fatal(ids, e)
	}
	if e = s.DeleteProfile(id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadProfile(id); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestProfileRejectsInvalid(t *testing.T) {
	for _, mutate := range []func(*Profile){func(p *Profile) { p.SchemaVersion = 2 }, func(p *Profile) { p.Workers = 0 }, func(p *Profile) { p.Workers = 129 }, func(p *Profile) { p.Name = "" }, func(p *Profile) { p.Raw = `/MT:129` }, func(p *Profile) { p.Raw = `/JOB:foo` }, func(p *Profile) { p.Destination = p.Source }, func(p *Profile) { p.Raw = "\"unterminated" }} {
		p := fixtureProfile()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted invalid: %#v", p)
		}
	}
	p := fixtureProfile()
	p.Expert = true
	p.Raw = `/JOB:"C:\jobs\daily"`
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestStoreRejectsTraversalAndAliases(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "store")}
	for _, id := range []string{"../elsewhere", strings.Repeat("a", 32) + "/../../x", "", strings.Repeat("A", 32)} {
		if _, e := s.Path("runs", id, ".log"); e == nil {
			t.Fatal(id)
		}
	}
	id, _ := NewID()
	p, e := s.Path("profiles", id, ".json")
	if e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if e = os.WriteFile(outside, []byte("untouched"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, p); e != nil {
		t.Skip("symlinks unavailable", e)
	}
	if _, e = s.LoadProfile(id); e == nil {
		t.Fatal("read alias")
	}
	if e = s.SaveProfile(id, fixtureProfile()); e == nil {
		t.Fatal("write alias")
	}
	if e = s.DeleteProfile(id); e == nil {
		t.Fatal("delete alias")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "untouched" {
		t.Fatal("outside changed")
	}
	root := filepath.Join(t.TempDir(), "alias")
	if e = os.Symlink(filepath.Dir(outside), root); e != nil {
		t.Fatal(e)
	}
	if _, e = (Store{Root: root}).IDs("runs"); e == nil {
		t.Fatal("root alias accepted")
	}
}
func TestRunPersistenceAndScopedDeletion(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "store")}
	id, _ := NewID()
	r := RunRecord{SchemaVersion: 1, ID: id, Status: "running", ExitCode: -1, Started: time.Now().UTC(), Source: `C:\source`, Destination: `D:\dest`}
	if e := s.SaveRun(r); e != nil {
		t.Fatal(e)
	}
	f, e := s.CreateLog(id)
	if e != nil {
		t.Fatal(e)
	}
	text := strings.Repeat("日本語 output\r\n", 10000)
	f.WriteString(text)
	f.Sync()
	f.Close()
	if _, e = s.CreateLog(id); e == nil {
		t.Fatal("log clobber permitted")
	}
	r.Status = "failure"
	r.ExitCode = 8
	r.Ended = time.Now().UTC()
	r.Summary = map[string][]string{"Bytes": {"100", "10", "0", "0", "90", "0"}}
	r.Failures = []Failure{{Code: "5", Path: `C:\source\failed.txt`, Evidence: "native evidence"}}
	if e = s.SaveRun(r); e != nil {
		t.Fatal(e)
	}
	got, e := s.LoadRun(id)
	if e != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("%#v %v", got, e)
	}
	p, _ := s.Path("runs", id, ".log")
	data, e := os.ReadFile(p)
	if e != nil || string(data) != text {
		t.Fatal("full log truncated", e)
	}
	r.Status = "running"
	if !strings.Contains(RunDescription(r), "not a success") {
		t.Fatal("interrupted mislabelled")
	}
	if e = s.DeleteRun(id); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestStoreMalformedRecord(t *testing.T) {
	s := Store{Root: t.TempDir()}
	id, _ := NewID()
	p, _ := s.Path("profiles", id, ".json")
	for _, text := range []string{`{"schemaVersion":99}`, `{} {}`, `{"unknown":true}`, `{broken`} {
		os.WriteFile(p, []byte(text), 0600)
		if _, e := s.LoadProfile(id); e == nil {
			t.Fatal(text)
		}
	}
	rid, _ := NewID()
	p, _ = s.Path("runs", rid, ".json")
	os.WriteFile(p, []byte(`{"schemaVersion":1,"id":"wrong"}`), 0600)
	if _, e := s.LoadRun(rid); e == nil {
		t.Fatal("identity mismatch")
	}
}
func TestFailureEvidence(t *testing.T) {
	for _, line := range []string{`2026/10/26 10:00:00 ERROR 5 (0x00000005) Copying File C:\source\日本語 failed.txt`, `ERROR 32 (0x00000020) Accessing Source Directory \\server\share\source\`} {
		f, ok := FailureLine(line)
		if !ok || f.Path == "" || f.Code == "" || f.Evidence != line {
			t.Fatal(line, f, ok)
		}
	}
	for _, line := range []string{`New File 123 C:\source\x`, `100%`, `ERROR copying file C:\source\x`, `ERROR 5 (0x00000005) Copying File ../relative`, `Files : 10 0 0 0 10 0`, `FEHLER 5 (0x00000005) Kopiere Datei C:\source\x`} {
		if _, ok := FailureLine(line); ok {
			t.Fatal("invented evidence", line)
		}
	}
}
func TestDangerBanner(t *testing.T) {
	for _, f := range []string{"/MIR", "/purge"} {
		s := DangerBanner([]string{f}, false, `D:\precious`)
		if !strings.Contains(s, "DESTINATION DELETE") || !strings.Contains(s, `D:\precious`) {
			t.Fatal(s)
		}
	}
	if !strings.Contains(DangerBanner([]string{"/MOVE"}, false, ""), "SOURCE DELETE") {
		t.Fatal("move")
	}
	if !strings.Contains(DangerBanner(nil, true, ""), "EXPERT COMMAND") {
		t.Fatal("expert")
	}
	if DangerBanner([]string{"/E", "/Z"}, false, "") != "" {
		t.Fatal("false warning")
	}
}
func TestActivityEstimateNeverCompletion(t *testing.T) {
	for _, tc := range []struct {
		written uint64
		planned int64
		value   int
		ok      bool
	}{{0, 0, 0, false}, {0, -1, 0, false}, {0, 100, 0, true}, {50, 100, 500, true}, {95, 100, 950, true}, {100, 100, 950, true}, {^uint64(0), 100, 950, true}} {
		v, ok := ActivityEstimate(tc.written, tc.planned)
		if v != tc.value || ok != tc.ok {
			t.Fatal(tc, v, ok)
		}
	}
}
