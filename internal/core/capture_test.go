package core

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16Fixture(s string) []byte {
	b := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func TestNativeCaptureBuild(t *testing.T) {
	base := Options{Source: `C:\src`, Destination: `D:\dst`, Custom: true, Extra: []string{"/BYTES", "/FP", "/SAVE:job"}}
	for _, path := range []string{"", `C:\temp dir\capture 日本語.log`} {
		o := base
		o.Log = path
		a, e := Build(o, true)
		if e != nil {
			t.Fatal(e)
		}
		want := path
		if want == "" {
			want = "<temporary Unicode capture log>"
		}
		if SelectedLog(a).Path != want || !SelectedLog(a).Unicode || a[len(a)-1] != "/SAVE:job" || Has(a, "UNICODE") || !Has(a, "TEE") || !Has(a, "L") {
			t.Fatal(a)
		}
	}
	for _, last := range []string{"/LOG:relative spaced.log", "/LOG+:../append.log", "/UNILOG:日本語.log", "/UNILOG+:日本語.log"} {
		o := base
		o.Log = "must-not-be-added"
		o.Extra = []string{"/UNILOG:first.log", last, "/SAVE:job"}
		a, e := Build(o, false)
		if e != nil {
			t.Fatal(e)
		}
		want := append([]string{o.Source, o.Destination}, o.Extra[:2]...)
		want = append(want, "/TEE", "/SAVE:job")
		if !reflect.DeepEqual(a, want) {
			t.Fatalf("%q != %q", a, want)
		}
		target := SelectedLog(a)
		if target.Path != strings.SplitN(last, ":", 2)[1] || target.Append != strings.Contains(last, "+:") || target.Unicode != strings.HasPrefix(last, "/UNI") {
			t.Fatal(target)
		}
	}
}

func TestNativeUnicodeTailEveryChunkBoundary(t *testing.T) {
	text := "New File 42 C:\\src\\日本語 😀.txt\r100%\r\nFiles : 1 1 0 0 0 0\r\nBytes : 42 42 0 0 0 0\r\n"
	b := utf16Fixture(text)
	for size := 1; size < 40; size++ {
		d := LogDecoder{Unicode: true}
		var out string
		for start := 0; start < len(b); start += size {
			end := start + size
			if end > len(b) {
				end = len(b)
			}
			out += d.Feed(b[start:end], false)
		}
		out += d.Feed(nil, true)
		if out != text || Summary(out)["Bytes"][1] != "42" {
			t.Fatalf("chunk %d: %q", size, out)
		}
	}
}

func TestNativeANSITailPreservesMultibyteRecords(t *testing.T) {
	// CP932 lead/trail split between reads must reach the native decoder together.
	var records [][]byte
	d := LogDecoder{ANSI: func(b []byte) string { records = append(records, append([]byte{}, b...)); return string(b) }}
	if got := d.Feed([]byte{0x93}, false); got != "" {
		t.Fatal(got)
	}
	if got := d.Feed([]byte{0xfa, '\r'}, false); got != string([]byte{0x93, 0xfa, '\r'}) {
		t.Fatal(got)
	}
	d.Feed([]byte("no final newline"), false)
	if got := d.Feed(nil, true); got != "no final newline" {
		t.Fatal(got)
	}
	if len(records) != 4 {
		t.Fatal(records)
	}
}

func TestNativeLogOffsets(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int64
	}{
		{[]string{"/LOG:x"}, 0},
		{[]string{"/LOG+:x"}, 123},
		{[]string{"/UNILOG+:x"}, 123},
		{[]string{"/LOG:y", "/UNILOG+:x"}, 123},
		{[]string{"/LOG:X", "/UNILOG+:x"}, 0},
		{[]string{"/UNILOG+:x", "/LOG:x", "/UNILOG+:x"}, 0},
	} {
		if got := LogOffset(tc.args, 123, strings.EqualFold); got != tc.want {
			t.Fatalf("%q offset %d want %d", tc.args, got, tc.want)
		}
	}
}

func TestAppendCaptureExcludesOldSummary(t *testing.T) {
	old := utf16Fixture("Files : 99 99 0 0 0 0\r\n")
	fresh := utf16Fixture("Files : 3 3 0 0 0 0\r\nBytes : 106524 106524 0 0 0 0\r\n")
	disk := append(append([]byte{}, old...), fresh...)
	d := LogDecoder{Unicode: true}
	got := d.Feed(disk[len(old):], true)
	if strings.Contains(got, "99") || Summary(got)["Files"][1] != "3" {
		t.Fatal(got)
	}
}
