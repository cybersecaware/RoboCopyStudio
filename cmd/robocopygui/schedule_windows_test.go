//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"robocopygui/internal/core"
	"robocopygui/internal/scheduling"
)

func scheduleTestJob(t *testing.T) scheduling.Job {
	t.Helper()
	id, err := core.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return scheduling.Job{Version: scheduling.Version, ID: id, Created: time.Now().UTC(), Schedule: scheduling.Spec{Kind: "once", Date: "2099-12-31", Time: "12:00"}, Options: core.Options{Source: `C:\source`, Destination: `D:\destination`, Custom: true, Extra: []string{"/E", "/R:0", "/W:0", "/BYTES", "/FP"}}}
}
func TestScheduleDPAPISnapshot(t *testing.T) {
	s := core.Store{Root: filepath.Join(t.TempDir(), "protected-jobs")}
	if err := s.Secure(); err != nil {
		t.Fatal(err)
	}
	j := scheduleTestJob(t)
	j.Options.Extra = append(j.Options.Extra, "/MIR")
	j.Authorized = true
	if err := saveScheduledJob(s, j); err != nil {
		t.Fatal(err)
	}
	p, err := s.Path("runs", j.ID, ".json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("authorized")) || bytes.Contains(raw, []byte(j.Options.Source)) {
		t.Fatal("plaintext approval/options persisted")
	}
	loaded, err := loadScheduledJob(s, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Authorized || loaded.ID != j.ID {
		t.Fatal("snapshot lost authorization/identity")
	}
	if err := saveScheduledJob(s, j); err == nil {
		t.Fatal("immutable snapshot overwritten")
	}
	raw[len(raw)/2] ^= 1
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadScheduledJob(s, j.ID); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
}
func TestScheduleWorkerRejectsMalformedID(t *testing.T) {
	for _, id := range []string{"", "../unsafe", strings.Repeat("A", 32)} {
		if RunScheduledJob(id) == 0 {
			t.Fatalf("accepted malformed ID %q", id)
		}
	}
}

// Explicit opt-in only. This creates a far-future OWN task targeting the test
// executable and removes it; it NEVER asks Task Scheduler to run a task.
// Run both as a standard user and explicitly elevated; registration must remain LeastPrivilege.
// ROBOCOPY_SCHEDULE_NATIVE_FIXTURE=1 <test.exe> -test.run TestScheduleNativeRegistration
func TestScheduleNativeRegistration(t *testing.T) {
	if os.Getenv("ROBOCOPY_SCHEDULE_NATIVE_FIXTURE") != "1" {
		t.Skip("explicit native Task Scheduler fixture opt-in required")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	for _, kind := range []string{"once", "daily", "weekly"} {
		t.Run(kind, func(t *testing.T) {
			j := scheduleTestJob(t)
			j.Schedule.Kind = kind
			if kind == "weekly" {
				j.Schedule.Days = []int{1, 3, 5}
			}
			if err := RegisterScheduledJob(j); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := DeleteScheduledJob(j.ID); err != nil {
					t.Error(err)
				}
			}()
			level, err := queryScheduledJobRunLevelContext(context.Background(), j.ID)
			if err != nil || level != "LeastPrivilege" {
				t.Fatalf("want registered LeastPrivilege, got %q: %v", level, err)
			}
			if present, err := scheduledTaskPresent(j.ID); err != nil || !present {
				t.Fatalf("registration not found: %v", err)
			}
		})
	}
}

// Explicit opt-in, disposable local copy only; no scheduled task is created/run.
func TestScheduleNativeWorker(t *testing.T) {
	if os.Getenv("ROBOCOPY_SCHEDULE_NATIVE_FIXTURE") != "1" {
		t.Skip("explicit native Robocopy fixture opt-in required")
	}
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata"))
	if err := os.Mkdir(filepath.Join(root, "appdata"), 0700); err != nil {
		t.Fatal(err)
	}
	src, dst := filepath.Join(root, "source"), filepath.Join(root, "destination")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "unicode-文件.txt"), []byte("scheduled fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	j := scheduleTestJob(t)
	j.Options.Source = src
	j.Options.Destination = dst
	s, err := scheduleStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = saveScheduledJob(s, j); err != nil {
		t.Fatal(err)
	}
	if code := RunScheduledJob(j.ID); code != 0 {
		t.Fatalf("worker returned %d", code)
	}
	b, err := os.ReadFile(filepath.Join(dst, "unicode-文件.txt"))
	if err != nil || string(b) != "scheduled fixture\n" {
		t.Fatalf("copy mismatch: %q %v", b, err)
	}
	// Second execution exercises Robocopy native no-copy success (exit 0), too.
	if code := RunScheduledJob(j.ID); code != 0 {
		t.Fatalf("second worker returned %d", code)
	}
	store, err := defaultStore()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.IDs("runs")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 {
		t.Fatalf("want four preflight/copy records, got %d", len(ids))
	}
	for _, id := range ids {
		r, err := store.LoadRun(id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == "running" || r.AuditError != "" || r.ExitCode < 0 || r.ExitCode >= 8 {
			t.Fatalf("bad history: %+v", r)
		}
	}
}
