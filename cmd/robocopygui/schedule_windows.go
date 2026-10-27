//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
	"robocopygui/internal/core"
	"robocopygui/internal/scheduling"
)

// Jobs have their own ACL-protected store. Approval and options are additionally
// authenticated/encrypted with current-user DPAPI; ordinary saved profiles carry no approval.
func scheduleStore() (core.Store, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return core.Store{}, fmt.Errorf("LOCALAPPDATA unavailable")
	}
	s := core.Store{Root: filepath.Join(root, "RoboCopyStudioScheduled")}
	return s, s.Secure()
}
func scheduleIdentity() (string, string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	return exe, u.User.Sid.String(), err
}
func scheduleCrypt(b []byte, decrypt bool) ([]byte, error) {
	if len(b) == 0 || len(b) > 4<<20 {
		return nil, fmt.Errorf("invalid scheduled snapshot size")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if out.Size > 4<<20 {
		return nil, fmt.Errorf("scheduled snapshot exceeds budget")
	}
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func saveScheduledJob(s core.Store, j scheduling.Job) error {
	if err := j.Validate(j.ID); err != nil {
		return err
	}
	ids, err := s.IDs("runs")
	if err != nil {
		return err
	}
	if len(ids) >= 1000 {
		return fmt.Errorf("scheduled job limit reached; delete selected jobs explicitly")
	}
	p, err := s.Path("runs", j.ID, ".json")
	if err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	b, err = scheduleCrypt(b, false)
	if err != nil {
		return err
	}
	// Immutable ID: no update or overwrite path exists.
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return auditio.Finalize(f, err)
}
func loadScheduledJob(s core.Store, id string) (scheduling.Job, error) {
	var j scheduling.Job
	p, err := s.Path("runs", id, ".json")
	if err != nil {
		return j, err
	}
	f, err := os.Open(p)
	if err != nil {
		return j, err
	}
	defer f.Close()
	if _, err = auditio.Regular(f); err != nil {
		return j, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return j, err
	}
	if info.NumberOfLinks != 1 {
		return j, fmt.Errorf("scheduled snapshot hardlink rejected")
	}
	b, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return j, err
	}
	b, err = scheduleCrypt(b, true)
	if err != nil {
		return j, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&j); err != nil {
		return j, err
	}
	if d.Decode(new(any)) != io.EOF {
		return j, fmt.Errorf("invalid trailing snapshot data")
	}
	return j, j.Validate(id)
}

// schtasks is invoked directly, never through a shell. Both capture and duration are bounded.
func scheduleCommand(args ...string) ([]byte, error) {
	return scheduleCommandContext(context.Background(), args...)
}
func scheduleCommandContext(parent context.Context, args ...string) ([]byte, error) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(sys, "schtasks.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.WaitDelay = 2 * time.Second
	var b bytes.Buffer
	w := auditio.NewBudgetWriter(&b, 4<<20, cancel)
	cmd.Stdout = w
	cmd.Stderr = w
	err = cmd.Run()
	err = errors.Join(err, w.Err(), ctx.Err())
	if err != nil {
		return b.Bytes(), fmt.Errorf("Task Scheduler command failed (no automatic elevation; normal-user permissions or explicit admin access for legacy tasks may be required): %w (details: %s)", err, core.BoundedConsole("", string(b.Bytes())))
	}
	return b.Bytes(), nil
}
func queryScheduledJob(id string) error {
	return queryScheduledJobContext(context.Background(), id)
}
func queryScheduledJobContext(ctx context.Context, id string) error {
	_, err := queryScheduledJobRunLevelContext(ctx, id)
	return err
}
func queryScheduledJobRunLevelContext(ctx context.Context, id string) (string, error) {
	if !scheduling.ValidID(id) {
		return "", fmt.Errorf("invalid job ID")
	}
	exe, sid, err := scheduleIdentity()
	if err != nil {
		return "", err
	}
	b, err := scheduleCommandContext(ctx, "/Query", "/TN", scheduling.Prefix+id, "/XML")
	if err != nil {
		return "", err
	}
	return scheduling.OwnedRunLevel(b, id, exe, sid)
}
func installScheduledJob(j scheduling.Job) error {
	s, err := scheduleStore()
	if err != nil {
		return err
	}
	exe, sid, err := scheduleIdentity()
	if err != nil {
		return err
	}
	b, err := scheduling.XML(j, exe, sid)
	if err != nil {
		return err
	}
	if err = saveScheduledJob(s, j); err != nil {
		return err
	}
	// Write XML beneath the protected store, not a global temporary directory.
	f, err := os.CreateTemp(filepath.Join(s.Root, "runs"), ".task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	err = auditio.Finalize(f, err)
	if err != nil {
		return err
	}
	if _, err = scheduleCommand("/Create", "/TN", scheduling.Prefix+j.ID, "/XML", f.Name()); err != nil {
		return fmt.Errorf("snapshot retained, task registration failed: %w", err)
	}
	// Verify exact registered action/owner AND least privilege even when this
	// app registered the task from an elevated process. Never repair by rewriting.
	level, err := queryScheduledJobRunLevelContext(context.Background(), j.ID)
	if err != nil {
		return err
	}
	if level != "LeastPrivilege" {
		return fmt.Errorf("registered task is not LeastPrivilege; snapshot/task retained for explicit review")
	}
	return nil
}
func scheduledTaskPresent(id string) (bool, error) {
	if !scheduling.ValidID(id) {
		return false, fmt.Errorf("invalid job ID")
	}
	b, err := scheduleCommand("/Query", "/FO", "CSV", "/NH")
	if err != nil {
		return false, err
	}
	if len(b) > 2 && b[0] == 255 && b[1] == 254 {
		b = []byte(core.DecodeLog(b))
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if len(row) > 0 && strings.EqualFold(row[0], scheduling.Prefix+id) {
			return true, nil
		}
	}
	return false, nil
}
func deleteScheduledJob(id string) error {
	s, err := scheduleStore()
	if err != nil {
		return err
	}
	if _, err = loadScheduledJob(s, id); err != nil {
		return err
	}
	present, err := scheduledTaskPresent(id)
	if err != nil {
		return err
	}
	if present {
		if err = queryScheduledJob(id); err != nil {
			return err
		}
		if _, err = scheduleCommand("/Delete", "/TN", scheduling.Prefix+id, "/F"); err != nil {
			return err
		}
	}
	// A failed per-task query alone cannot prove absence. Also supports explicitly
	// removing a retained snapshot after failed registration, without touching a task.
	present, err = scheduledTaskPresent(id)
	if err != nil {
		return fmt.Errorf("could not verify task absence: %w", err)
	}
	if present {
		return fmt.Errorf("task is still registered")
	}
	p, err := s.Path("runs", id, ".json")
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// RegisterScheduledJob persists a new immutable snapshot, imports its task, and verifies owner/action.
// It never starts the task. Call off the GUI thread.
func RegisterScheduledJob(j scheduling.Job) error { return installScheduledJob(j) }

// DeleteScheduledJob removes only a verified owned task and its snapshot; it does not stop a running copy.
func DeleteScheduledJob(id string) error { return deleteScheduledJob(id) }

// RunScheduledJob is the headless --scheduled-job entrypoint. Native integration
// fixtures may call it directly; it performs real filesystem operations.
func RunScheduledJob(id string) int { return scheduledWorker(id) }

func scheduledWorker(id string) int {
	if !scheduling.ValidID(id) {
		return 2
	}
	s, err := scheduleStore()
	if err != nil {
		return 2
	}
	j, err := loadScheduledJob(s, id)
	if err != nil {
		return 2
	}
	// Cross-session kernel lock; IgnoreNew is also set on each task. Do not run
	// multiple scheduled jobs concurrently against potentially shared endpoints.
	_, sid, err := scheduleIdentity()
	if err != nil {
		return 2
	}
	// Windows mutex ownership is thread-affine, not goroutine-affine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, _ := windows.UTF16PtrFromString(`Global\RoboCopyStudioScheduled-` + sid)
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return 2
	}
	defer windows.CloseHandle(h)
	wait, err := windows.WaitForSingleObject(h, 0)
	if err != nil || (wait != windows.WAIT_OBJECT_0 && wait != windows.WAIT_ABANDONED) {
		return 3
	}
	defer windows.ReleaseMutex(h)
	store, err := defaultStore()
	if err != nil {
		return 2
	}
	// Fresh endpoint/alias validation occurs inside run() before EACH phase.
	// A bounded preflight is required even for non-destructive scheduled copies.
	if scheduledPhase(store, j.Options, true) != 0 {
		return 1
	}
	return scheduledPhase(store, j.Options, false)
}
func scheduledPhase(store core.Store, o core.Options, preview bool) int {
	a := &app{store: store, events: make(chan event, 32)}
	usage, err := a.storeUsage()
	if err != nil || usage > auditio.StoreBytes-3*auditio.RunBytes-(8<<20) {
		return 2
	}
	id, err := core.NewID()
	if err != nil {
		return 2
	}
	a.fullLog, err = store.CreateLog(id)
	if err != nil {
		return 2
	}
	a.record = core.RunRecord{SchemaVersion: core.StoreVersion, ID: id, Status: "running", Started: time.Now().UTC(), ExitCode: -1, Source: o.Source, Destination: o.Destination, Preview: preview, MT: core.Has(o.Extra, "MT")}
	if err = store.SaveRun(a.record); err != nil {
		a.fullLog.Close()
		return 2
	}
	// Preflight has a hard wall-clock bound; copy itself retains native retry policy.
	ctx, cancel := context.WithCancel(context.Background())
	if preview {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	}
	defer cancel()
	var auditBytes int64
	var previewRecords, previewRecordBytes int
	go a.run(ctx, o, preview)
	for e := range a.events {
		if preview && e.line != "" && a.auditErr == nil {
			previewRecords++
			previewRecordBytes += len(e.line)
			if previewRecords > core.MaxPlanFiles || previewRecordBytes > core.MaxPlanFilenameBytes {
				a.auditErr = errors.Join(a.auditErr, fmt.Errorf("scheduled preflight record/byte budget reached"))
				cancel()
			}
		}
		if e.commandText != "" {
			a.record.Command = e.commandText
			if err = store.SaveRun(a.record); err != nil {
				a.auditErr = errors.Join(a.auditErr, err)
				cancel()
			}
		}
		if e.text != "" && a.auditErr == nil {
			if int64(len(e.text)) > auditio.RunBytes-auditBytes {
				a.auditErr = fmt.Errorf("scheduled transcript budget reached; audit incomplete")
				cancel()
			} else {
				n, writeErr := a.fullLog.WriteString(e.text)
				auditBytes += int64(n)
				if writeErr != nil {
					a.auditErr = writeErr
					cancel()
				}
			}
		}
		if f, ok := core.FailureLine(e.line); ok {
			a.record.Failures = core.AppendFailure(a.record.Failures, f)
		}
		if e.telemetryLimited && preview {
			a.auditErr = errors.Join(a.auditErr, fmt.Errorf("preflight record limit exceeded"))
			cancel()
		}
		if !e.done {
			continue
		}
		if preview && e.plan.Limited {
			a.auditErr = errors.Join(a.auditErr, fmt.Errorf("preflight limited: %s", e.plan.LimitReason))
		}
		if ctx.Err() != nil {
			e.err = errors.Join(e.err, ctx.Err())
		}
		err = a.finishAudit(e)
		if err != nil || e.err != nil || e.auditError != nil || e.code < 0 || e.code >= 8 {
			return 1
		}
		return 0
	}
	return 1
}
