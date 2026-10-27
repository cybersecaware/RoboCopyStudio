package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const StoreVersion = 1
const maxManifestSize = 4 << 20

// MaxStoreEntries limits managed JSON manifests per category. Enumeration is
// batched; excess entries fail explicitly without returning a truncated list.
// Archiving/deleting old history and controlling log disk growth remain manual.
const MaxStoreEntries = 1000

type Profile struct {
	SchemaVersion int    `json:"schemaVersion"`
	Name          string `json:"name"`
	Source        string `json:"source"`
	Destination   string `json:"destination"`
	Raw           string `json:"rawFlags"`
	Mirror        bool   `json:"mirror"`
	Restart       bool   `json:"restartable"`
	MT            bool   `json:"multithreaded"`
	Workers       int    `json:"workers"`
	Expert        bool   `json:"expert"`
}

func (p Profile) Validate() error {
	if p.SchemaVersion != StoreVersion || len(p.Name) == 0 || len(p.Name) > 80 || strings.TrimSpace(p.Name) != p.Name || strings.ContainsAny(p.Name, "\x00\r\n") {
		return errors.New("invalid profile name or schema version")
	}
	if p.Workers < 1 || p.Workers > 128 {
		return errors.New("invalid profile worker count")
	}
	args, e := SplitCommandLine(p.Raw)
	if e != nil {
		return e
	}
	if e = ValidateFlags(args, p.Expert); e != nil {
		return e
	}
	if !p.Expert {
		return ValidatePaths(p.Source, p.Destination)
	}
	return nil
}

type Failure struct {
	Code     string `json:"code"`
	Path     string `json:"path"`
	Evidence string `json:"evidence"`
}
type RunRecord struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	Status        string              `json:"status"`
	Started       time.Time           `json:"started"`
	Ended         time.Time           `json:"ended,omitempty"`
	ExitCode      int                 `json:"exitCode"`
	Error         string              `json:"error,omitempty"`
	AuditError    string              `json:"auditError,omitempty"`
	Source        string              `json:"source"`
	Destination   string              `json:"destination"`
	Command       string              `json:"command"`
	Preview       bool                `json:"preview"`
	Expert        bool                `json:"expert"`
	MT            bool                `json:"multithreaded"`
	PlannedBytes  int64               `json:"plannedBytes"`
	PlannedFiles  int                 `json:"plannedFiles"`
	Summary       map[string][]string `json:"nativeSummary,omitempty"`
	Failures      []Failure           `json:"failureEvidence,omitempty"`
}
type Store struct{ Root string }

var safeID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func NewID() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}

// Managed paths are fixed components plus opaque random IDs, never paths read
// from a manifest. Reject symlinks/reparse aliases before reads and deletions.
// As with all local stores, a concurrent same-user attacker is out of scope.
func (s Store) dir(kind string) (string, error) {
	if kind != "profiles" && kind != "runs" {
		return "", errors.New("invalid store category")
	}
	root, e := filepath.Abs(s.Root)
	if e != nil {
		return "", e
	}
	for _, p := range []string{root, filepath.Join(root, kind)} {
		// Validate the nearest existing ancestor BEFORE MkdirAll, so even a
		// missing store below a tampered parent alias creates nothing outside.
		ancestor := p
		for {
			st, err := os.Lstat(ancestor)
			if err == nil {
				if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
					return "", errors.New("unsafe store ancestor")
				}
				resolved, err := filepath.EvalSymlinks(ancestor)
				if err != nil || !strings.EqualFold(resolved, ancestor) {
					return "", errors.New("aliased store ancestor")
				}
				break
			}
			if !os.IsNotExist(err) {
				return "", err
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				return "", errors.New("no safe store ancestor")
			}
			ancestor = parent
		}
		if e = os.MkdirAll(p, 0700); e != nil {
			return "", e
		}
		resolved, e := filepath.EvalSymlinks(p)
		if e != nil {
			return "", e
		}
		if !strings.EqualFold(filepath.Clean(resolved), p) {
			return "", errors.New("store aliases/symlinks are not allowed")
		}
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("unsafe store directory")
		}
	}
	return filepath.Join(root, kind), nil
}
func (s Store) Path(kind, id, ext string) (string, error) {
	if !safeID.MatchString(id) || (ext != ".json" && !(kind == "runs" && ext == ".log")) {
		return "", errors.New("invalid managed identifier")
	}
	d, e := s.dir(kind)
	if e != nil {
		return "", e
	}
	p := filepath.Join(d, id+ext)
	if st, e := os.Lstat(p); e == nil {
		if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("unsafe store file")
		}
		q, e := filepath.EvalSymlinks(p)
		if e != nil || !strings.EqualFold(q, p) {
			return "", errors.New("unsafe store file alias")
		}
		f, e := os.Open(p)
		if e != nil {
			return "", e
		}
		e = validateManagedHandle(f)
		closeErr := f.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	return p, nil
}
func (s Store) write(kind, id string, v any) error {
	p, e := s.Path(kind, id, ".json")
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if len(b)+1 > maxManifestSize {
		return errors.New("manifest exceeds 4 MiB limit")
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".pending-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(b, '\n')); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), p)
}
func (s Store) read(kind, id string, v any) error {
	p, e := s.Path(kind, id, ".json")
	if e != nil {
		return e
	}
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = validateManagedHandle(f); e != nil {
		return e
	}
	info, e := f.Stat()
	if e != nil {
		return e
	}
	if info.Size() > maxManifestSize {
		return errors.New("manifest exceeds 4 MiB limit")
	}
	dec := json.NewDecoder(io.LimitReader(f, maxManifestSize))
	dec.DisallowUnknownFields()
	if e = dec.Decode(v); e != nil {
		return e
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("invalid trailing JSON")
	}
	return nil
}
func (s Store) IDs(kind string) ([]string, error) {
	d, e := s.dir(kind)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(d)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var ids []string
	for {
		names, err := f.Readdirnames(128)
		for _, name := range names {
			if strings.HasSuffix(name, ".json") {
				id := strings.TrimSuffix(name, ".json")
				if safeID.MatchString(id) {
					if len(ids) >= MaxStoreEntries {
						return nil, fmt.Errorf("%s store exceeds %d manifests; archive entries separately before loading", kind, MaxStoreEntries)
					}
					ids = append(ids, id)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(ids)
	return ids, nil
}
func (s Store) SaveProfile(id string, p Profile) error {
	if e := p.Validate(); e != nil {
		return e
	}
	return s.write("profiles", id, p)
}
func (s Store) LoadProfile(id string) (Profile, error) {
	var p Profile
	e := s.read("profiles", id, &p)
	if e == nil {
		e = p.Validate()
	}
	return p, e
}
func (s Store) DeleteProfile(id string) error {
	p, e := s.Path("profiles", id, ".json")
	if e != nil {
		return e
	}
	return os.Remove(p)
}
func (s Store) SaveRun(r RunRecord) error {
	if r.SchemaVersion != StoreVersion {
		return errors.New("invalid run schema")
	}
	return s.write("runs", r.ID, r)
}
func (s Store) LoadRun(id string) (RunRecord, error) {
	var r RunRecord
	e := s.read("runs", id, &r)
	if e == nil && (r.ID != id || r.SchemaVersion != StoreVersion) {
		e = errors.New("invalid run identity/schema")
	}
	return r, e
}
func (s Store) CreateLog(id string) (*os.File, error) {
	p, e := s.Path("runs", id, ".log")
	if e != nil {
		return nil, e
	}
	return os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}
func (s Store) DeleteRun(id string) error { // Validate both BEFORE removing either.
	p, e := s.Path("runs", id, ".json")
	if e != nil {
		return e
	}
	l, e := s.Path("runs", id, ".log")
	if e != nil {
		return e
	}
	if e = os.Remove(l); e != nil && !os.IsNotExist(e) {
		return e
	}
	return os.Remove(p)
}

// Recognize only explicit native English ERROR code/hex-code + operation/path
// records. Translated/unknown output stays in the log, never guessed as a file.
var nativeError = regexp.MustCompile(`(?i)\bERROR\s+([0-9]+)\s+\(0x[0-9a-f]+\)\s+(?:Copying File|Accessing Source Directory|Accessing Destination Directory|Creating Destination Directory|Deleting File|Deleting Directory)\s+((?:[a-z]:\\|\\\\).+)$`)

func FailureLine(line string) (Failure, bool) {
	m := nativeError.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Failure{}, false
	}
	return Failure{Code: m[1], Path: strings.TrimSpace(m[2]), Evidence: strings.TrimSpace(line)}, true
}

// Bound structured evidence independently from the complete retained log. This
// keeps even long-path failure storms within the manifest read budget.
func AppendFailure(records []Failure, f Failure) []Failure {
	if len(records) >= 1000 {
		return records
	}
	size := len(f.Code) + len(f.Path) + len(f.Evidence)
	for _, old := range records {
		if old.Evidence == f.Evidence {
			return records
		}
		size += len(old.Code) + len(old.Path) + len(old.Evidence)
	}
	if size > 1<<20 {
		return records
	}
	return append(records, f)
}
func DangerBanner(args []string, expert bool, destination string) string {
	var messages []string
	if expert {
		destination = "expert command / JOB destination (inspect command; endpoint fields are inactive)"
	}
	if Has(args, "MIR") || Has(args, "PURGE") {
		messages = append(messages, "DESTINATION DELETE — /MIR or /PURGE can permanently delete extras at: "+destination)
	}
	if Has(args, "MOV") || Has(args, "MOVE") {
		messages = append(messages, "SOURCE DELETE — /MOV or /MOVE deletes source data after copying")
	}
	if Has(args, "M") {
		messages = append(messages, "SOURCE CHANGE — /M resets source archive bits")
	}
	if expert {
		messages = append(messages, "EXPERT COMMAND — may delete data; inspect endpoints, flags and JOB files")
	}
	return strings.Join(messages, "\r\n")
}

// RunListTitle keeps dropdown labels bounded; full paths and evidence belong in
// the report, otherwise native ComboBox ideal sizing can exceed the desktop.
func RunListTitle(r RunRecord) string {
	kind := "Copy"
	if r.Preview {
		kind = "Preview"
	}
	status := "Completed"
	switch {
	case r.Status == "running":
		status = "Unfinished"
	case r.Error != "" || r.ExitCode < 0:
		status = "Stopped / error"
	case r.ExitCode >= 8:
		status = "Failed"
	}
	if r.AuditError != "" {
		status += " / audit incomplete"
	}
	return fmt.Sprintf("%s | %s | %s | exit %d", r.Started.Local().Format("2006-01-02 15:04:05"), kind, status, r.ExitCode)
}

func RunDescription(r RunRecord) string {
	status := r.Status
	if status == "running" {
		status = "Running / unfinished (not a success)"
	}
	if r.AuditError != "" {
		status += "; AUDIT INCOMPLETE"
	}
	copied := "bytes unknown"
	if row := r.Summary["Bytes"]; len(row) == 6 {
		copied = row[1] + " B native copied"
	}
	elapsed := "elapsed unknown"
	if !r.Ended.IsZero() && !r.Ended.Before(r.Started) {
		elapsed = r.Ended.Sub(r.Started).Round(time.Millisecond).String()
	}
	return fmt.Sprintf("%s | %s | exit %d | %s | %s | %s → %s", r.Started.Local().Format("2006-01-02 15:04:05"), status, r.ExitCode, copied, elapsed, r.Source, r.Destination)
}

// ActivityEstimate is an explicitly unverified I/O-budget proxy, NOT copied
// bytes: WriteTransferCount includes log, metadata, retries and other overhead.
// Keep it below 100% until a native summary arrives; no per-file attribution.
func ActivityEstimate(written uint64, planned int64) (int, bool) {
	if planned <= 0 {
		return 0, false
	}
	ratio := float64(written) / float64(planned)
	if ratio >= 0.95 {
		return 950, true
	}
	return int(ratio * 1000), true
}
