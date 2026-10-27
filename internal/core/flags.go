package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Flag is a documented Robocopy option. Kind is bool, number, size, text,
// optional-number, optional-size, or list. Newer flags depend on engine version.
type Flag struct {
	Name, Group, Kind, Description string
	Newer                          bool
}

var Catalog = []Flag{
	{"S", "Copy", "bool", "Subdirectories excluding empty directories", false},
	{"E", "Copy", "bool", "Subdirectories including empty directories", false},
	{"LEV", "Copy", "number", "Maximum directory depth", false},
	{"Z", "Copy", "bool", "Restartable copy", false}, {"B", "Copy", "bool", "Backup privilege mode", false}, {"ZB", "Copy", "bool", "Restartable with backup fallback", false},
	{"J", "Copy", "bool", "Unbuffered I/O for large files", false}, {"EFSRAW", "Copy", "bool", "Encrypted files in EFS RAW mode", false},
	{"COPY", "Metadata", "text", "File metadata: D A T X S O U", false}, {"DCOPY", "Metadata", "text", "Directory metadata: D A T E X", false},
	{"SEC", "Metadata", "bool", "File data, attributes, timestamps and ACLs", false}, {"COPYALL", "Metadata", "bool", "All file metadata DATSOU", false}, {"NOCOPY", "Metadata", "bool", "No file information", false},
	{"SECFIX", "Metadata", "bool", "Fix security even on skipped files; also select security COPY flags", false}, {"TIMFIX", "Metadata", "bool", "Fix timestamps even on skipped files", false},
	{"PURGE", "Copy", "bool", "DELETES destination extras", false}, {"MIR", "Copy", "bool", "Mirror tree; DELETES destination extras", false}, {"MOV", "Copy", "bool", "DELETE source files after copying", false}, {"MOVE", "Copy", "bool", "DELETE source files and directories after copying", false},
	{"A+", "Metadata", "text", "Add attributes RASHCNET", false}, {"A-", "Metadata", "text", "Remove attributes RASHCNETO", false}, {"CREATE", "Copy", "bool", "Create tree and zero-length files only", false}, {"FAT", "Copy", "bool", "Use 8.3 destination names", false}, {"256", "Copy", "bool", "Disable long paths", false},
	{"MON", "Scheduling / Jobs", "number", "Monitor changes; open-ended until stopped", false}, {"MOT", "Scheduling / Jobs", "number", "Monitor interval in minutes; open-ended", false}, {"RH", "Scheduling / Jobs", "text", "Allowed start window hhmm-hhmm", false}, {"PF", "Scheduling / Jobs", "bool", "Check run window per file", false},
	{"IPG", "Performance", "number", "Inter-packet gap milliseconds; incompatible with MT", false}, {"SJ", "Copy", "bool", "Copy junction itself, not target", true}, {"SL", "Copy", "bool", "Copy symbolic link itself", false}, {"MT", "Performance", "optional-number", "Threads 1–128; blank = 8; no IPG/EFSRAW", false},
	{"NODCOPY", "Metadata", "bool", "Do not copy directory metadata", false}, {"NOOFFLOAD", "Performance", "bool", "Disable Windows copy offload", false}, {"COMPRESS", "Performance", "bool", "Request network compression", true}, {"SPARSE", "Performance", "text", "Retain sparse state: Y or N", true}, {"NOCLONE", "Performance", "bool", "Disable block cloning optimization", true},
	{"IOMAXSIZE", "Performance", "size", "Maximum I/O size, optional K/M/G suffix", true}, {"IORATE", "Performance", "size", "I/O rate bytes/s, optional K/M/G; minimum throttle 524288 B/s", true}, {"THRESHOLD", "Performance", "size", "Minimum file size to throttle, optional K/M/G", true},
	{"A", "Selection", "bool", "Archive attribute files only", false}, {"M", "Selection", "bool", "Archive files and RESET source archive bit", false}, {"IA", "Selection", "text", "Include attributes RASHCNETO", false}, {"XA", "Selection", "text", "Exclude attributes RASHCNETO", false},
	{"XF", "Selection", "list", "Exclude file names/paths; quote spaces; multiple values", false}, {"XD", "Selection", "list", "Exclude directories; quote spaces; multiple values", false},
	{"XC", "Selection", "bool", "Exclude same-time different-size files", false}, {"XN", "Selection", "bool", "Exclude newer source files", false}, {"XO", "Selection", "bool", "Exclude older source files", false}, {"XX", "Selection", "bool", "Exclude extras; not a deletion request", false}, {"XL", "Selection", "bool", "Exclude source-only entries", false}, {"IM", "Selection", "bool", "Include changed change-times", false}, {"IS", "Selection", "bool", "Include same files", false}, {"IT", "Selection", "bool", "Include attribute tweaks", false},
	{"MAX", "Selection", "number", "Maximum bytes", false}, {"MIN", "Selection", "number", "Minimum bytes", false}, {"MAXAGE", "Selection", "number", "Exclude older than days or YYYYMMDD", false}, {"MINAGE", "Selection", "number", "Exclude newer than days or YYYYMMDD", false}, {"MAXLAD", "Selection", "number", "Exclude last accessed before days/date", false}, {"MINLAD", "Selection", "number", "Exclude last accessed since days/date", false},
	{"XJ", "Selection", "bool", "Exclude junction points", false}, {"FFT", "Selection", "bool", "Two-second FAT timestamp tolerance", false}, {"DST", "Selection", "bool", "One-hour DST tolerance", false}, {"XJD", "Selection", "bool", "Exclude directory junctions", false}, {"XJF", "Selection", "bool", "Exclude file junctions", false},
	{"R", "Retry", "number", "Failed-copy retries (native default one million)", false}, {"W", "Retry", "number", "Seconds between retries (native default 30)", false}, {"REG", "Retry", "bool", "WRITE retry defaults to registry, even in preview", false}, {"TBD", "Retry", "bool", "Wait for shares to become defined", false}, {"LFSM", "Retry", "optional-size", "Low free space floor; blank = 10%; no MT/EFSRAW", true},
	{"L", "Logging", "bool", "List only; no copy/delete/timestamp changes", false}, {"X", "Logging", "bool", "Report all extras", false}, {"V", "Logging", "bool", "Verbose including skipped files", false}, {"TS", "Logging", "bool", "Source timestamps", false}, {"FP", "Logging", "bool", "Full paths for telemetry", false}, {"BYTES", "Logging", "bool", "Exact byte sizes for telemetry", false}, {"NS", "Logging", "bool", "Suppress sizes; telemetry unavailable", false}, {"NC", "Logging", "bool", "Suppress file classes", false}, {"NFL", "Logging", "bool", "Suppress file names; telemetry unavailable", false}, {"NDL", "Logging", "bool", "Suppress directory names", false}, {"NP", "Logging", "bool", "Suppress progress; live estimates unavailable", false}, {"ETA", "Logging", "bool", "Native ETA output", false},
	{"LOG", "Logging", "text", "Overwrite ANSI log path (writes even during preview)", false}, {"LOG+", "Logging", "text", "Append ANSI log path", false}, {"UNILOG", "Logging", "text", "Overwrite Unicode log path", false}, {"UNILOG+", "Logging", "text", "Append Unicode log path", false}, {"TEE", "Logging", "bool", "Console plus selected log (always enabled for capture)", false}, {"NJH", "Logging", "bool", "Suppress header", false}, {"NJS", "Logging", "bool", "Suppress summary; final counters unavailable", false}, {"UNICODE", "Logging", "bool", "Unicode console (always enabled for capture)", false},
	{"JOB", "Scheduling / Jobs", "text", "Load RCJ options/endpoints: requires expert full-command mode", false}, {"SAVE", "Scheduling / Jobs", "text", "WRITE RCJ job; must be last; confirmation required", false}, {"QUIT", "Scheduling / Jobs", "bool", "Quit after parameter processing", false}, {"NOSD", "Scheduling / Jobs", "bool", "No source: requires expert full-command mode", false}, {"NODD", "Scheduling / Jobs", "bool", "No destination: requires expert full-command mode", false}, {"IF", "Scheduling / Jobs", "list", "Include file specifications", false},
}

// SplitCommandLine implements Windows backslash/quote rules, not shell parsing.
func SplitCommandLine(s string) ([]string, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("arguments must be valid Unicode")
	}
	var out []string
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			break
		}
		var b strings.Builder
		quoted := false
		for i < len(s) {
			if !quoted && (s[i] == ' ' || s[i] == '\t') {
				break
			}
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				b.WriteString(strings.Repeat("\\", n/2))
				if n%2 == 1 {
					b.WriteByte('"')
				} else if quoted && i+1 < len(s) && s[i+1] == '"' {
					b.WriteByte('"')
					i++
				} else {
					quoted = !quoted
				}
				i++
				continue
			}
			b.WriteString(strings.Repeat("\\", n))
			if i < len(s) && !quoted && (s[i] == ' ' || s[i] == '	') {
				break
			}
			if i < len(s) {
				b.WriteByte(s[i])
				i++
			}
		}
		if quoted {
			return nil, fmt.Errorf("unclosed quote")
		}
		out = append(out, b.String())
	}
	for _, a := range out {
		if strings.ContainsAny(a, "\x00\r\n") {
			return nil, fmt.Errorf("NUL/newline is not allowed in arguments")
		}
	}
	return out, nil
}
func QuoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	n := 0
	for _, r := range s {
		if r == '\\' {
			n++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat("\\", 2*n+1))
		} else {
			b.WriteString(strings.Repeat("\\", n))
		}
		n = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat("\\", 2*n))
	b.WriteByte('"')
	return b.String()
}
func Command(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = QuoteArg(a)
	}
	return strings.Join(q, " ")
}
func SwitchName(s string) string {
	if !strings.HasPrefix(s, "/") {
		return ""
	}
	return strings.ToUpper(strings.SplitN(s[1:], ":", 2)[0])
}
func Has(args []string, n string) bool {
	for _, a := range args {
		if SwitchName(a) == n {
			return true
		}
	}
	return false
}
func Destructive(args []string) bool {
	for _, n := range []string{"MIR", "PURGE", "MOV", "MOVE", "M"} {
		if Has(args, n) {
			return true
		}
	}
	return false
}
func SideEffects(args []string) bool {
	for _, n := range []string{"REG", "SAVE", "LOG", "LOG+", "UNILOG", "UNILOG+"} {
		if Has(args, n) {
			return true
		}
	}
	return false
}
func OpenEnded(args []string) bool { return Has(args, "MON") || Has(args, "MOT") }
func Telemetry(args []string) bool {
	for _, n := range []string{"MT", "NP", "NFL", "NS", "MON", "MOT", "V", "IS", "CREATE", "QUIT", "L"} {
		if Has(args, n) {
			return false
		}
	}
	return Has(args, "BYTES") && Has(args, "FP")
}
func FormatFlag(f Flag, value string) ([]string, error) {
	if f.Kind == "bool" {
		return []string{"/" + f.Name}, nil
	}
	if f.Kind == "list" {
		a, e := SplitCommandLine(value)
		if e != nil {
			return nil, e
		}
		if len(a) == 0 {
			return nil, fmt.Errorf("/%s needs values", f.Name)
		}
		return append([]string{"/" + f.Name}, a...), nil
	}
	if value == "" && (f.Kind == "optional-number" || f.Kind == "optional-size" || f.Name == "SPARSE") {
		return []string{"/" + f.Name}, nil
	}
	if value == "" {
		return nil, fmt.Errorf("/%s needs a value", f.Name)
	}
	return []string{"/" + f.Name + ":" + value}, nil
}

var sizeRE = regexp.MustCompile(`(?i)^\d+[kmg]?$`)

func ValidateFlags(args []string, expert bool) error {
	known := map[string]Flag{}
	for _, f := range Catalog {
		known[f.Name] = f
	}
	for _, a := range args {
		n := SwitchName(a)
		if n == "" {
			continue
		}
		f, ok := known[n]
		if !ok && n != "" && !expert {
			return fmt.Errorf("unknown /%s: use expert full-command mode for passthrough", n)
		}
		if !ok {
			continue
		} // Expert switches pass through without guessing semantics.
		if !expert && (n == "JOB" || n == "NOSD" || n == "NODD") {
			return fmt.Errorf("/%s changes endpoint semantics: use expert full-command mode", n)
		}
		p := strings.SplitN(a, ":", 2)
		v := ""
		if len(p) == 2 {
			v = p[1]
		}
		if f.Kind == "bool" && len(p) > 1 {
			return fmt.Errorf("/%s does not take a value", n)
		}
		if f.Kind == "number" || f.Kind == "optional-number" {
			if v == "" && f.Kind == "optional-number" {
				continue
			}
			bits := 32
			if n == "MAX" || n == "MIN" {
				bits = 64
			}
			x, e := strconv.ParseUint(v, 10, bits)
			if e != nil {
				return fmt.Errorf("/%s requires a nonnegative %d-bit integer", n, bits)
			}
			if n == "MT" && (x < 1 || x > 128) {
				return fmt.Errorf("/MT must be 1–128")
			}
			if (n == "LEV" || n == "MON" || n == "MOT") && x == 0 {
				return fmt.Errorf("/%s must be positive", n)
			}
			if strings.Contains(n, "AGE") || strings.Contains(n, "LAD") {
				if x >= 1900 {
					if _, e := time.Parse("20060102", v); e != nil {
						return fmt.Errorf("/%s requires days below 1900 or YYYYMMDD", n)
					}
				}
			}
		}
		if f.Kind == "size" || f.Kind == "optional-size" {
			if v == "" && f.Kind == "optional-size" {
				continue
			}
			if !sizeRE.MatchString(v) {
				return fmt.Errorf("/%s requires bytes with optional K/M/G", n)
			}
		}
		if f.Kind == "text" {
			if n == "SPARSE" && len(p) == 1 {
				continue // documented bare /SPARSE defaults to yes
			}
			if v == "" {
				return fmt.Errorf("/%s needs a value", n)
			}
			allowed := ""
			switch n {
			case "COPY":
				allowed = "DATXSOU"
			case "DCOPY":
				allowed = "DATEX"
			case "IA", "XA", "A-":
				allowed = "RASHCNETO"
			case "A+":
				allowed = "RASHCNET"
			case "SPARSE":
				if !strings.EqualFold(v, "Y") && !strings.EqualFold(v, "N") {
					return fmt.Errorf("/SPARSE requires Y or N")
				}
			case "RH":
				if !regexp.MustCompile(`^([01][0-9]|2[0-3])[0-5][0-9]-([01][0-9]|2[0-3])[0-5][0-9]$`).MatchString(v) {
					return fmt.Errorf("/RH requires hhmm-hhmm")
				}
			}
			if allowed != "" {
				for _, r := range strings.ToUpper(v) {
					if !strings.ContainsRune(allowed, r) {
						return fmt.Errorf("invalid /%s metadata/attribute letter", n)
					}
				}
			}
		}
	}
	if Has(args, "MT") && (Has(args, "IPG") || Has(args, "EFSRAW")) {
		return fmt.Errorf("/MT conflicts with /IPG and /EFSRAW")
	}
	if Has(args, "LFSM") && (Has(args, "MT") || Has(args, "EFSRAW")) {
		return fmt.Errorf("/LFSM conflicts with /MT and /EFSRAW")
	}
	for i, a := range args {
		if SwitchName(a) == "SAVE" && i != len(args)-1 {
			return fmt.Errorf("/SAVE must be the final argument")
		}
		if SwitchName(a) == "XF" || SwitchName(a) == "XD" || SwitchName(a) == "IF" {
			if i+1 == len(args) || SwitchName(args[i+1]) != "" {
				return fmt.Errorf("%s needs one or more values", a)
			}
		}
	}
	return nil
}

// Build inserts explicit capture options before SAVE, preserving user log paths.
func Build(o Options, preview bool) ([]string, error) {
	if !o.Custom {
		o.Preview = preview
		if o.Log == "" {
			o.Log = "<temporary Unicode capture log>"
		}
		return Args(o), nil
	}
	a := append([]string{}, o.Extra...)
	if err := ValidateFlags(a, o.Expert); err != nil {
		return nil, err
	}
	if !o.Expert {
		if err := ValidatePaths(o.Source, o.Destination); err != nil {
			return nil, err
		}
		a = append([]string{o.Source, o.Destination}, a...)
	}
	if len(a) == 0 {
		return nil, fmt.Errorf("expert command is empty")
	}
	tail := ""
	if SwitchName(a[len(a)-1]) == "SAVE" {
		tail = a[len(a)-1]
		a = a[:len(a)-1]
	}
	// Redirected /UNICODE stdout can mix ANSI and UTF-16. Tail a real native
	// log instead; never append an internal log after a user's log switches.
	if SelectedLog(a).Path == "" {
		log := o.Log
		if log == "" {
			log = "<temporary Unicode capture log>"
		}
		a = append(a, "/UNILOG:"+log)
	}
	if !Has(a, "TEE") {
		a = append(a, "/TEE")
	}
	if preview {
		if o.Expert && Has(a, "JOB") {
			return nil, fmt.Errorf("JOB preview is not guaranteed read-only; run explicitly in expert mode")
		}
		if !Has(a, "L") {
			a = append(a, "/L")
		}
	}
	if tail != "" {
		a = append(a, tail)
	}
	return a, nil
}

// Summary recognizes English invariant labels only; translated/abbreviated sizes
// remain raw instead of guessing units or locale-dependent decimal separators.
func Summary(text string) map[string][]string {
	out := map[string][]string{}
	for _, line := range Lines(text) {
		p := strings.SplitN(line, ":", 2)
		if len(p) != 2 {
			continue
		}
		name := strings.TrimSpace(p[0])
		if name != "Dirs" && name != "Files" && name != "Bytes" && name != "Times" {
			continue
		}
		v := strings.Fields(p[1])
		if len(v) != 6 {
			continue
		}
		if name != "Times" {
			valid := true
			for _, s := range v {
				if _, e := strconv.ParseUint(s, 10, 64); e != nil {
					valid = false
				}
			}
			if !valid {
				continue
			}
		}
		out[name] = v
	}
	return out
}
