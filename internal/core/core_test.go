package core

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestPaths(t *testing.T) {
	for _, p := range [][2]string{{`C:\data`, `c:\DATA`}, {`C:\data`, `C:\data\child`}, {`C:\data\child`, `C:\data`}, {`C:\`, `C:\other`}, {`\\srv\share`, `\\srv\share\sub`}, {`C:\one\..\data`, `C:\data`}, {`relative`, `D:\dest`}, {`\\?\C:\data`, `D:\dest`}, {`C:\file:stream`, `D:\dest`}} {
		if ValidatePaths(p[0], p[1]) == nil {
			t.Errorf("accepted %q", p)
		}
	}
	for _, p := range [][2]string{{`C:\data`, `D:\data`}, {`C:\data`, `C:\database`}, {`\\srv\share-a`, `\\srv\share-b`}} {
		if e := ValidatePaths(p[0], p[1]); e != nil {
			t.Errorf("rejected %q: %v", p, e)
		}
	}
}
func TestArgs(t *testing.T) {
	o := Options{Source: `C:\a & whoami`, Destination: `D:\x`, Log: `C:\temp\file name.log`, Mirror: true, Preview: true}
	a := Args(o)
	want := []string{o.Source, o.Destination, "/E", "/Z", "/R:2", "/W:2", "/XJ", "/BYTES", "/FP", "/COPY:DAT", "/DCOPY:DAT", "/MIR", "/L", "/UNILOG:" + o.Log}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("%q", a)
	}
	b := strings.Join(Args(Options{}), " ")
	if strings.Contains(b, "/MIR") || strings.Contains(b, "/L ") {
		t.Fatal(b)
	}
}
func TestLocalizedFixtures(t *testing.T) {
	src := `C:\source`
	p := NewPlan()
	fixtures := []string{"\tNew File\t 1024\tC:\\source\\hello.txt", "\t新しいファイル\t 2048\tC:\\source\\日本語.txt", "\tNeue Datei\t 0\tC:\\source\\leer.txt", "\t*EXTRA File\t 999\tD:\\dest\\extra.txt", "\tNew Dir\t 3\tC:\\source\\nested\\", "unrecognized localized summary: 900 800 700"}
	for _, s := range fixtures {
		p.Add(s, src)
	}
	if len(p.Files) != 3 || p.Bytes != 3072 {
		t.Fatalf("%+v", p)
	}
	p.Add(fixtures[0], src)
	if p.Bytes != 3072 {
		t.Fatal("duplicate counted")
	}
}
func TestProgress(t *testing.T) {
	p := NewPlan()
	p.Add(" New File 100 C:\\src\\one", `C:\src`)
	p.Add(" New File 200 C:\\src\\two", `C:\src`)
	tr := NewTracker(p, `C:\src`)
	for _, l := range []string{" New File 100 C:\\src\\one", "50,0%", "100%", "100%", " New File 200 C:\\src\\two", "25.0%"} {
		tr.Feed(l)
	}
	b, f := tr.Estimate()
	if b != 150 || f != 1 {
		t.Fatalf("%d %d", b, f)
	}
	tr.Feed("10%")
	b, _ = tr.Estimate()
	if b != 150 {
		t.Fatal("regression")
	}
	tr.Feed("ERROR 5 (0x00000005) Access denied")
	b, _ = tr.Estimate()
	if b != 150 {
		t.Fatal("error counted")
	}
}
func TestUTF16AndLines(t *testing.T) {
	s := "\ufeffNew File 0 C:\\src\\日本語\r100%\r\n"
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	b = append(b, 3)
	got := DecodeLog(b)
	if got != strings.TrimPrefix(s, "\ufeff") {
		t.Fatalf("%q", got)
	}
	if len(Lines(got)) != 2 {
		t.Fatal(Lines(got))
	}
}
func TestOutcomeAndConsole(t *testing.T) {
	for _, n := range []int{8, 16, 32} {
		if !strings.Contains(Outcome(n), "FAILED") {
			t.Fatal(Outcome(n))
		}
	}
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		if strings.Contains(Outcome(n), "FAILED") {
			t.Fatal(Outcome(n))
		}
	}
	s := BoundedConsole(strings.Repeat("x", ConsoleLimit), "new output")
	if !strings.HasSuffix(s, "new output") || len(s) > ConsoleLimit+30 {
		t.Fatalf("length %d", len(s))
	}
	if Bytes(1024) != "1.00 KiB" {
		t.Fatal(Bytes(1024))
	}
}
func FuzzFileLine(f *testing.F) {
	f.Add(" New File 0 C:\\src\\empty")
	f.Add("日本語 12 C:\\src\\test")
	f.Fuzz(func(t *testing.T, s string) { FileLine(s, `C:\src`); DecodeLog([]byte(s)) })
}
