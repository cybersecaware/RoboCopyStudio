// Package core contains platform-independent command and progress logic.
package core

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

type Options struct {
	Source, Destination, Log string
	Mirror, Preview          bool
	Custom, Expert           bool
	Extra                    []string
}

func Args(o Options) []string {
	a := []string{o.Source, o.Destination, "/E", "/Z", "/R:2", "/W:2", "/XJ", "/BYTES", "/FP", "/COPY:DAT", "/DCOPY:DAT"}
	if o.Mirror {
		a = append(a, "/MIR")
	}
	if o.Preview {
		a = append(a, "/L")
	}
	return append(a, "/UNILOG:"+o.Log)
}

// CleanWindows rejects device namespaces and relative paths. Canonical filesystem
// alias checks are additionally performed by the Windows front end.
func CleanWindows(s string) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "/", `\`)
	for _, part := range strings.Split(s, `\`) {
		if part != "." && part != ".." && (strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ")) {
			return "", errors.New("folder components ending in a dot or space are unsupported")
		}
	}
	if strings.ContainsAny(s, "\x00\r\n\"") || strings.HasPrefix(s, `\\?\`) || strings.HasPrefix(s, `\\.\`) {
		return "", errors.New("unsupported path syntax")
	}
	unc := strings.HasPrefix(s, `\\`)
	if !unc && !(len(s) >= 3 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) && s[1] == ':' && s[2] == '\\') {
		return "", errors.New("choose an absolute drive or UNC folder")
	}
	if strings.Contains(s[2:], ":") {
		return "", errors.New("alternate data stream paths are not folders")
	}
	q := strings.ReplaceAll(s, `\`, "/")
	q = path.Clean(q)
	if unc {
		q = "/" + q
		if len(strings.Split(strings.Trim(q, "/"), "/")) < 2 {
			return "", errors.New("UNC path requires server and share")
		}
	}
	s = strings.TrimRight(strings.ReplaceAll(q, "/", `\`), `\`)
	return strings.ToLower(s), nil
}
func ValidatePaths(src, dst string) error {
	a, e := CleanWindows(src)
	if e != nil {
		return fmt.Errorf("source: %w", e)
	}
	b, e := CleanWindows(dst)
	if e != nil {
		return fmt.Errorf("destination: %w", e)
	}
	if a == b || strings.HasPrefix(a, b+`\`) || strings.HasPrefix(b, a+`\`) {
		return errors.New("source and destination must be separate, non-nested folders")
	}
	return nil
}

var beforePath = regexp.MustCompile(`(?:^|\s)([0-9]+)\s+$`)
var percent = regexp.MustCompile(`^\s*([0-9]+(?:[.,][0-9]+)?)%\s*$`)

// FileLine deliberately does not depend on translated New File/Older labels.
// Only full paths under the source are accepted; destination extras are excluded.
func FileLine(line, src string) (string, int64, bool) {
	root := strings.TrimRight(src, `\/`) + `\`
	// Search the original string: Unicode case conversion can change UTF-8
	// byte lengths, so indices into a lowercased copy are not safe slice offsets.
	loc := regexp.MustCompile("(?i)" + regexp.QuoteMeta(root)).FindStringIndex(line)
	if loc == nil {
		return "", 0, false
	}
	i := loc[0]
	name := strings.TrimSpace(line[i:])
	if strings.HasSuffix(name, `\`) {
		return "", 0, false
	}
	m := beforePath.FindStringSubmatch(line[:i])
	if m == nil {
		return "", 0, false
	}
	n, e := strconv.ParseInt(m[1], 10, 64)
	return strings.ToLower(name), n, e == nil
}

const (
	MaxPlanFiles         = 100000
	MaxPlanFilenameBytes = 32 << 20
)

// Plan is a detailed preview, not an authorization when Limited is true.
// Exceeding either budget discards all details/totals rather than silently
// approving a truncated preview. The raw native log remains authoritative.
type Plan struct {
	Files         map[string]int64
	Bytes         int64
	Limited       bool
	LimitReason   string
	filenameBytes int
}

func NewPlan() Plan { return Plan{Files: map[string]int64{}} }
func (p *Plan) Add(line, src string) {
	if p.Limited {
		return
	}
	k, n, ok := FileLine(line, src)
	// ParseInt returns its saturated value with ErrRange for an oversized
	// positive decimal. Do not silently omit such a recognized file record.
	if !ok && k != "" && n == math.MaxInt64 {
		p.limit("preview file size exceeds int64; inspect raw log; preview not approved")
		return
	}
	if ok {
		old, exists := p.Files[k]
		reason := ""
		if !exists && len(p.Files) >= MaxPlanFiles {
			reason = "preview exceeds 100000 detailed files; inspect raw log; preview not approved"
		} else if !exists && len(k) > MaxPlanFilenameBytes-p.filenameBytes {
			reason = "preview exceeds 32 MiB of filenames; inspect raw log; preview not approved"
		} else if n > math.MaxInt64-(p.Bytes-old) {
			reason = "preview byte total overflows int64; inspect raw log; preview not approved"
		}
		if reason != "" {
			p.limit(reason)
			return
		}
		if p.Files == nil {
			p.Files = make(map[string]int64)
		}
		if !exists {
			p.filenameBytes += len(k)
		}
		// Own just the filename, not a substring retaining its entire input record.
		p.Files[strings.Clone(k)] = n
		p.Bytes = p.Bytes - old + n
	}
}

func (p *Plan) limit(reason string) {
	p.Limited, p.LimitReason = true, reason
	p.Files, p.Bytes, p.filenameBytes = nil, 0, 0
}

type Tracker struct {
	Plan            Plan
	Source, Current string
	Fraction        map[string]float64
	Seen            map[string]bool
}

func NewTracker(p Plan, src string) *Tracker {
	if p.Limited {
		p.Files, p.Bytes = nil, 0
	}
	return &Tracker{Plan: p, Source: src, Fraction: map[string]float64{}, Seen: map[string]bool{}}
}
func (t *Tracker) Feed(line string) {
	if t.Plan.Limited {
		t.Current = ""
		return
	}
	k, _, ok := FileLine(line, t.Source)
	if k != "" {
		// An unplanned record must also clear attribution of later percentages.
		t.Current = ""
		if !ok {
			return
		}
		if _, approved := t.Plan.Files[k]; !approved {
			return
		}
		// Detach from the log record so a small key cannot pin a huge prefix.
		k = strings.Clone(k)
		t.Current = k
		t.Seen[k] = true
		return
	}
	if m := percent.FindStringSubmatch(line); m != nil && t.Current != "" {
		f, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		f = math.Min(1, math.Max(0, f/100))
		if f > t.Fraction[t.Current] {
			t.Fraction[t.Current] = f
		}
	}
}
func (t *Tracker) Estimate() (int64, int) {
	var b float64
	var f int
	for k, n := range t.Plan.Files {
		v := t.Fraction[k]
		b += float64(n) * v
		if v >= 1 || (n == 0 && t.Seen[k]) {
			f++
		}
	}
	return int64(b), f
}
func Outcome(code int) string {
	switch {
	case code < 0:
		return "Could not run Robocopy"
	case code >= 8:
		return fmt.Sprintf("FAILED — Robocopy exit %d; inspect the console", code)
	case code >= 4:
		return fmt.Sprintf("Finished with mismatches — exit %d; review the summary", code)
	case code >= 2:
		return fmt.Sprintf("Finished; extra destination entries reported — exit %d", code)
	case code == 1:
		return "Finished — Robocopy reports files copied (exit 1)"
	default:
		return "Finished — already up to date (exit 0)"
	}
}
func Bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	for _, u := range units {
		v /= 1024
		if v < 1024 || u == "PiB" {
			return fmt.Sprintf("%.2f %s", v, u)
		}
	}
	return ""
}

// DecodeLog decodes /UNILOG UTF-16LE output, safely ignoring a partial final unit.
func DecodeLog(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		v := uint16(b[i]) | uint16(b[i+1])<<8
		if i == 0 && v == 0xfeff {
			continue
		}
		u = append(u, v)
	}
	return string(utf16.Decode(u))
}
func Lines(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' })
}

const ConsoleLimit = 60000

func BoundedConsole(old, add string) string {
	s := old + add
	if len(s) > ConsoleLimit {
		s = s[len(s)-ConsoleLimit:]
		for len(s) > 0 && (s[0]&0xc0) == 0x80 {
			s = s[1:]
		}
		s = "[earlier output trimmed]\r\n" + s
	}
	return s
}
