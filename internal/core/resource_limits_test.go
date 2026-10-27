package core

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestANSIDecoderRecordLimit(t *testing.T) {
	for _, final := range []bool{false, true} {
		for _, suffix := range []string{"", "\nvalid later\n"} {
			d := LogDecoder{}
			b := append([]byte("valid first\r\n"), bytes.Repeat([]byte("x"), 4<<20)...)
			b = append(b, suffix...)
			if got := d.Feed(b, final); got != "valid first\r\n" {
				t.Fatalf("got length %d", len(got))
			}
			if d.Err == nil || d.carry != nil {
				t.Fatal("oversized record not rejected/released")
			}
			err := d.Err
			if got := d.Feed([]byte("\nordinary\n"), true); got != "" || d.Err != err {
				t.Fatal("error not sticky")
			}
		}
	}
}

func TestANSIDecoderChunkedLimitAndExactBoundary(t *testing.T) {
	d := LogDecoder{}
	chunk := bytes.Repeat([]byte("x"), 32<<10)
	for i := 0; i < MaxLogRecordBytes/len(chunk); i++ {
		if got := d.Feed(chunk, false); got != "" || d.Err != nil {
			t.Fatal("premature failure")
		}
		if cap(d.carry) > MaxLogRecordBytes+1 {
			t.Fatal("oversized retained capacity")
		}
	}
	if got := d.Feed([]byte("\n"), false); len(got) != MaxLogRecordBytes+1 || d.Err != nil {
		t.Fatal("exact limit rejected")
	}
	if got := d.Feed([]byte("ordinary\r\nlast"), false); got != "ordinary\r\n" {
		t.Fatal(got)
	}
	if got := d.Feed(nil, true); got != "last" || d.carry != nil {
		t.Fatal(got)
	}
	d = LogDecoder{}
	for i := 0; i < MaxLogRecordBytes/len(chunk); i++ {
		d.Feed(chunk, false)
	}
	if got := d.Feed([]byte("x\n"), true); got != "" || d.Err == nil || d.carry != nil {
		t.Fatal("late delimiter bypass")
	}
}

func TestANSIDecoderOrdinaryEveryBoundary(t *testing.T) {
	text := "new file 日本語 😀\r\n100%\r\nlast"
	for size := 1; size < 40; size++ {
		d := LogDecoder{}
		var out strings.Builder
		for start := 0; start < len(text); start += size {
			end := start + size
			if end > len(text) {
				end = len(text)
			}
			out.WriteString(d.Feed([]byte(text[start:end]), false))
		}
		out.WriteString(d.Feed(nil, true))
		if out.String() != text || d.Err != nil {
			t.Fatalf("chunk %d", size)
		}
	}
}

func TestPlanFileBudget(t *testing.T) {
	p := NewPlan()
	for i := 0; i < MaxPlanFiles; i++ {
		p.Add(fmt.Sprintf("1 C:\\src\\%d", i), `C:\src`)
	}
	if p.Limited || len(p.Files) != MaxPlanFiles || p.Bytes != MaxPlanFiles {
		t.Fatal("boundary plan wrong")
	}
	p.Add(`2 C:\src\0`, `C:\src`)
	if p.Limited || p.Bytes != MaxPlanFiles+1 {
		t.Fatal("duplicate update exceeded budget")
	}
	p.Add(`1 C:\src\overflow`, `C:\src`)
	assertLimitedPlan(t, p)
	p.Add(`1 C:\src\later`, `C:\src`)
	assertLimitedPlan(t, p)
	tr := NewTracker(p, `C:\src`)
	tr.Feed(`1 C:\src\later`)
	tr.Feed("100%")
	if len(tr.Seen) != 0 || len(tr.Fraction) != 0 {
		t.Fatal("limited tracker learned files")
	}
}

func assertLimitedPlan(t *testing.T, p Plan) {
	t.Helper()
	if !p.Limited || p.LimitReason == "" || p.Files != nil || p.Bytes != 0 || p.filenameBytes != 0 {
		t.Fatal("plan not explicitly disabled/released")
	}
}

func TestPlanFilenameBudget(t *testing.T) {
	p := NewPlan()
	stem := strings.Repeat("x", 32750)
	for i := 0; !p.Limited && i < 2000; i++ {
		p.Add(fmt.Sprintf("1 C:\\src\\%s%d", stem, i), `C:\src`)
		if p.filenameBytes > MaxPlanFilenameBytes {
			t.Fatal("retained filename budget exceeded")
		}
	}
	assertLimitedPlan(t, p)
	if !strings.Contains(p.LimitReason, "filenames") {
		t.Fatal(p.LimitReason)
	}
}

func TestPlanTotalOverflowFailsClosed(t *testing.T) {
	p := NewPlan()
	p.Add(fmt.Sprintf("%d C:\\src\\one", int64(math.MaxInt64)), `C:\src`)
	p.Add(`1 C:\src\two`, `C:\src`)
	assertLimitedPlan(t, p)
}

func TestPlanOversizedFileSizeFailsClosed(t *testing.T) {
	for _, size := range []string{"9223372036854775808", strings.Repeat("9", 100)} {
		p := NewPlan()
		p.Add(size+` C:\src\huge`, `C:\src`)
		assertLimitedPlan(t, p)
	}
	var zero Plan
	zero.Add(`3 C:\src\ordinary`, `C:\src`)
	if zero.Limited || len(zero.Files) != 1 || zero.Bytes != 3 {
		t.Fatal("zero-value plan failed")
	}
}

func TestTrackerIgnoresUnknownFiles(t *testing.T) {
	p := NewPlan()
	p.Add(`100 C:\src\known`, `C:\src`)
	tr := NewTracker(p, `C:\src`)
	tr.Feed(`100 C:\src\known`)
	tr.Feed("25%")
	for i := 0; i < 2000; i++ {
		tr.Feed(fmt.Sprintf("100 C:\\src\\unknown%d", i))
		tr.Feed("100%")
	}
	b, n := tr.Estimate()
	if b != 25 || n != 0 || len(tr.Seen) != 1 || len(tr.Fraction) != 1 || tr.Current != "" {
		t.Fatal("unknown records changed tracker", b, n)
	}
}

func TestStoreEnumerationLimit(t *testing.T) {
	for _, kind := range []string{"profiles", "runs"} {
		t.Run(kind, func(t *testing.T) {
			s := Store{Root: t.TempDir()}
			dir, err := s.dir(kind)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < MaxStoreEntries; i++ {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%032x.json", i)), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "runs" {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%032x.log", i)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			ids, err := s.IDs(kind)
			if err != nil || len(ids) != MaxStoreEntries || !sort.StringsAreSorted(ids) {
				t.Fatal("boundary enumeration", len(ids), err)
			}
			extra := filepath.Join(dir, fmt.Sprintf("%032x.json", MaxStoreEntries))
			if err := os.WriteFile(extra, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if ids, err = s.IDs(kind); err == nil || ids != nil || !strings.Contains(err.Error(), "archive") {
				t.Fatal("over-limit silently truncated", err)
			}
			if b, err := os.ReadFile(extra); err != nil || string(b) != "keep" {
				t.Fatal("enumeration removed entry")
			}
		})
	}
}

func TestManagedFilesRejectHardlinks(t *testing.T) {
	for _, kind := range []string{"profiles", "runs"} {
		for _, ext := range []string{".json", ".log"} {
			if kind == "profiles" && ext == ".log" {
				continue
			}
			t.Run(kind+ext, func(t *testing.T) {
				s := Store{Root: t.TempDir()}
				id := strings.Repeat("a", 32)
				p, err := s.Path(kind, id, ext)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(outside, p); err != nil {
					t.Skip(err)
				}
				if _, err := s.Path(kind, id, ext); err == nil {
					t.Fatal("hardlink accepted")
				}
				f, err := os.Open(p)
				if err != nil {
					t.Fatal(err)
				}
				err = validateManagedHandle(f)
				f.Close()
				if err == nil {
					t.Fatal("open handle hardlink accepted")
				}
				if kind == "profiles" {
					if _, err := s.LoadProfile(id); err == nil {
						t.Fatal("read accepted")
					}
					if err := s.DeleteProfile(id); err == nil {
						t.Fatal("delete accepted")
					}
				} else {
					if ext == ".log" {
						if err := s.SaveRun(RunRecord{SchemaVersion: StoreVersion, ID: id}); err != nil {
							t.Fatal(err)
						}
					} else if _, err := s.LoadRun(id); err == nil {
						t.Fatal("read accepted")
					}
					if err := s.DeleteRun(id); err == nil {
						t.Fatal("delete accepted")
					}
					if ext == ".log" {
						if _, err := s.LoadRun(id); err != nil {
							t.Fatal("manifest deleted before log validation", err)
						}
					}
					if err := s.SaveRun(RunRecord{SchemaVersion: StoreVersion, ID: id}); ext == ".json" && err == nil {
						t.Fatal("write accepted")
					}
				}
				if b, err := os.ReadFile(outside); err != nil || string(b) != "keep" {
					t.Fatal("outside fixture changed")
				}
				if _, err := os.Lstat(p); err != nil {
					t.Fatal("hardlink removed", err)
				}
			})
		}
	}
}
