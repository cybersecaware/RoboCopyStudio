package core

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWindowsArgumentRoundTrip(t *testing.T) {
	fixtures := [][]string{{`C:\source path\日本語`, `D:\target\`, `/XF`, `a b.txt`, `*.tmp`, `/XD`, `C:\skip dir\`}, {"", `a\"b`, `\\`, "plain", `tail\`}, {`/LOG:C:\my logs\a.txt`, "x&y", "a|b", "$(no shell)"}}
	for _, f := range fixtures {
		got, e := SplitCommandLine(Command(f))
		if e != nil || !reflect.DeepEqual(got, f) {
			t.Fatalf("%q => %q %v", Command(f), got, e)
		}
	}
	got, e := SplitCommandLine(`C:\src\ D:\dst /XF "a b" *.tmp /XF *.bak`)
	if e != nil || len(got) != 7 || got[0] != `C:\src\` {
		t.Fatalf("trailing slash: %q %v", got, e)
	}
	for _, s := range []string{`"broken`, "/LOG:a\x00b", "/R:2\n/MIR"} {
		if _, e := SplitCommandLine(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestEveryCatalogFlagFormat(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Catalog {
		if seen[f.Name] {
			t.Fatal("duplicate", f.Name)
		}
		seen[f.Name] = true
		v := ""
		switch f.Kind {
		case "number":
			v = "2"
		case "size":
			v = "1m"
		case "text":
			v = "x"
		case "list":
			v = `"日本語 space.txt" *.tmp`
		}
		switch f.Name {
		case "COPY", "DCOPY":
			v = "DAT"
		case "IA", "XA", "A+", "A-":
			v = "RA"
		case "RH":
			v = "0900-1730"
		case "SPARSE":
			v = "Y"
		}
		a, e := FormatFlag(f, v)
		if e != nil {
			t.Fatalf("%s %v", f.Name, e)
		}
		if SwitchName(a[0]) != f.Name {
			t.Fatal(a)
		}
		if e = ValidateFlags(a, true); e != nil {
			t.Fatalf("%s: %v", f.Name, e)
		}
		if f.Kind == "list" && (len(a) != 3 || a[1] != "日本語 space.txt") {
			t.Fatal(a)
		}
	}
	for _, name := range []string{"MIR", "COPY", "DCOPY", "MT", "JOB", "SAVE", "IF", "NODD", "NOSD", "IORATE", "LFSM", "SPARSE", "NOCLONE", "UNICODE"} {
		if !seen[name] {
			t.Fatal("missing", name)
		}
	}
}
func TestFlagValidation(t *testing.T) {
	bad := [][]string{{"/MT:0"}, {"/MT:129"}, {"/MT", "/IPG:2"}, {"/MT:16", "/EFSRAW"}, {"/LFSM", "/MT:1"}, {"/LFSM:1g", "/EFSRAW"}, {"/R:-1"}, {"/W:x"}, {"/LEV:0"}, {"/RH:2460-1700"}, {"/COPY:Q"}, {"/SPARSE:maybe"}, {"/MAXAGE:20260231"}, {"/MAXAGE:1900"}, {"/XF"}, {"/XD", "/E"}, {"/SAVE:job", "/Z"}, {"/MIR:1"}, {"/IORATE:nan"}}
	for _, args := range bad {
		if e := ValidateFlags(args, true); e == nil {
			t.Fatal("accepted", args)
		}
	}
	good := [][]string{{"/MT"}, {"/MT:128"}, {"/LFSM"}, {"/LFSM:1g"}, {"/MAX:1099511627776"}, {"/MAXAGE:20260101"}, {"/R:0", "/W:0"}, {"/XF", "*.tmp", "a b", "/XF", "*.bak", "/XD", "foo"}, {"/RH:0000-2359"}, {"/DCOPY:DATEX"}}
	for _, args := range good {
		if e := ValidateFlags(args, false); e != nil {
			t.Fatalf("%q %v", args, e)
		}
	}
	for _, a := range []string{"/JOB:x", "/NOSD", "/NODD", "/SD:C:\\other", "/FUTURE:1"} {
		if ValidateFlags([]string{a}, false) == nil {
			t.Fatal("unsafe managed", a)
		}
		if e := ValidateFlags([]string{a}, true); e != nil {
			t.Fatal(e)
		}
	}
}
func TestSparseOptionalValue(t *testing.T) {
	var sparse Flag
	for _, f := range Catalog {
		if f.Name == "SPARSE" {
			sparse = f
		}
	}
	for _, value := range []string{"", "Y", "N", "y", "n"} {
		args, err := FormatFlag(sparse, value)
		if err != nil || ValidateFlags(args, false) != nil {
			t.Fatalf("%q: %q %v", value, args, err)
		}
		if value == "" && !reflect.DeepEqual(args, []string{"/SPARSE"}) {
			t.Fatal(args)
		}
	}
	for _, arg := range []string{"/SPARSE:", "/SPARSE:maybe"} {
		if ValidateFlags([]string{arg}, false) == nil {
			t.Fatal("accepted", arg)
		}
	}
}

func TestCustomBuildPreservesEveryArgument(t *testing.T) {
	extra := []string{"*.txt", "/COPY:DATS", "/DCOPY:T", "/MT:16", "/XF", "a b.txt", "*.tmp", "/XD", "日本語 space", "/MAXAGE:7", "/LOG+:C:\\log path\\audit.txt", "/SAVE:C:\\job path\\job"}
	o := Options{Source: `C:\source space`, Destination: `D:\日本語 target`, Custom: true, Extra: extra}
	a, e := Build(o, true)
	if e != nil {
		t.Fatal(e)
	}
	want := append([]string{o.Source, o.Destination}, extra[:len(extra)-1]...)
	want = append(want, "/TEE", "/L", extra[len(extra)-1])
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("got %q want %q", a, want)
	}
	o.Expert = true
	o.Extra = []string{"/JOB:job", "/FUTURE:1"}
	if _, e = Build(o, true); e == nil {
		t.Fatal("job preview should not promise sandbox")
	}
	a, e = Build(o, false)
	if e != nil || a[0] != "/JOB:job" || !Has(a, "FUTURE") {
		t.Fatal(a, e)
	}
}
func TestSafetyAndTelemetry(t *testing.T) {
	for _, f := range []string{"MIR", "PURGE", "MOV", "MOVE", "M"} {
		if !Destructive([]string{"/" + f}) {
			t.Fatal(f)
		}
	}
	for _, f := range []string{"REG", "SAVE:x", "LOG:x", "LOG+:x", "UNILOG:x", "UNILOG+:x"} {
		if !SideEffects([]string{"/" + f}) {
			t.Fatal(f)
		}
	}
	base := []string{"/BYTES", "/FP"}
	if !Telemetry(base) {
		t.Fatal("single thread")
	}
	for _, f := range []string{"MT:16", "NP", "NFL", "NS", "MON:2", "MOT:2", "V", "IS", "CREATE", "QUIT", "L"} {
		if Telemetry(append(append([]string{}, base...), "/"+f)) {
			t.Fatal(f)
		}
	}
	if !OpenEnded([]string{"/MON:2"}) || !OpenEnded([]string{"/MOT:1"}) || OpenEnded(base) {
		t.Fatal("monitor")
	}
}
func TestSummaryHonestLocaleHandling(t *testing.T) {
	text := "Dirs : 2 1 1 0 0 0\r\nFiles : 3 2 1 0 0 0\r\nBytes : 106524 106524 0 0 0 0\r\nTimes : 0:00:01 0:00:01 0:00:00 0:00:00 0:00:00 0:00:00\r\n"
	r := Summary(text)
	if len(r) != 4 || r["Bytes"][1] != "106524" {
		t.Fatal(r)
	}
	for _, s := range []string{"Bytes : 1.2 m 1.2 m 0 0 0 0", "Bytes : 1,234 0 0 0 0 0", "Dateien : 2 1 1 0 0 0", "Files : 1 2 3"} {
		if len(Summary(s)) != 0 {
			t.Fatal("guessed localized summary", s)
		}
	}
	if !strings.Contains(Command([]string{`/LOG:C:\a b\log.txt`}), `"/LOG:`) {
		t.Fatal("path quoting")
	}
}
func FuzzArgumentRoundTrip(f *testing.F) {
	for _, s := range []string{"", `C:\space path\`, `quote"and\\slash`, "日本語"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) || strings.ContainsAny(s, "\x00\r\n") {
			return
		}
		got, e := SplitCommandLine(QuoteArg(s))
		if e != nil || len(got) != 1 || got[0] != s {
			t.Fatalf("%q: %q %v", s, got, e)
		}
	})
}
