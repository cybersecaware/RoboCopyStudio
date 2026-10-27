//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
	"robocopygui/internal/core"
)

func testAuditApp(t *testing.T) *app {
	t.Helper()
	a := &app{store: core.Store{Root: filepath.Join(t.TempDir(), "store")}}
	if err := a.store.Secure(); err != nil {
		t.Fatal(err)
	}
	id, err := core.NewID()
	if err != nil {
		t.Fatal(err)
	}
	a.record = core.RunRecord{SchemaVersion: core.StoreVersion, ID: id, Started: time.Now().UTC(), Status: "running", ExitCode: -1}
	a.fullLog, err = a.store.CreateLog(id)
	if err != nil {
		t.Fatal(err)
	}
	a.runLogPath = a.fullLog.Name()
	t.Cleanup(func() {
		if a.fullLog != nil {
			a.fullLog.Close()
		}
	})
	return a
}
func TestDocumentationIgnoresHostilePATH(t *testing.T) {
	hostile := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostile, "rundll32.exe"), []byte("inert test fixture; never executed"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hostile)
	cmd, err := documentationCommand()
	if err != nil {
		t.Fatal(err)
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(cmd.Path, filepath.Join(sys, "rundll32.exe")) || len(cmd.Args) != 3 || cmd.Args[1] != filepath.Join(sys, "url.dll")+",FileProtocolHandler" || cmd.Args[2] != documentationURL {
		t.Fatalf("untrusted helper selection: %#v", cmd.Args)
	}
}

// No Robocopy is started: this regression owns only a temporary directory and
// an unused drive-letter mapping, removed before fixture cleanup.
func TestValidateRejectsSUBSTNestedDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(src, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		t.Fatal(err)
	}
	drive := ""
	for letter := 'Z'; letter >= 'Q'; letter-- {
		if mask&(1<<uint(letter-'A')) == 0 {
			drive = string(letter) + ":"
			break
		}
	}
	if drive == "" {
		t.Skip("no unused scratch SUBST drive letter")
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	subst := filepath.Join(sys, "subst.exe")
	if output, err := exec.Command(subst, drive, src).CombinedOutput(); err != nil {
		t.Fatalf("scratch SUBST: %v %s", err, output)
	}
	defer func() {
		if output, err := exec.Command(subst, drive, "/D").CombinedOutput(); err != nil {
			t.Errorf("remove scratch SUBST: %v %s", err, output)
		}
	}()
	for _, dest := range []string{drive + `\child`, drive + `\not-created\child`} {
		if err := validate(src, dest); err == nil {
			t.Errorf("accepted nested SUBST destination %q", dest)
		}
	}
	outside := filepath.Join(root, "separate-destination")
	if err := validate(src, outside); err != nil {
		t.Fatalf("ordinary sibling rejected: %v", err)
	}
}

func TestPaintRegistryReleasesDisposedDialogs(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	baseline := len(paintedButtons)
	for i := 0; i < 120; i++ {
		d, err := walk.NewDialog(owner)
		if err != nil {
			t.Fatal(err)
		}
		b, err := walk.NewPushButton(d)
		if err != nil {
			d.Dispose()
			t.Fatal(err)
		}
		c, err := walk.NewCheckBox(d)
		if err != nil {
			d.Dispose()
			t.Fatal(err)
		}
		paintButton(&b.Button, false, false)
		paintButton(&c.Button, true, false)
		if len(paintedButtons) != baseline+2 {
			d.Dispose()
			t.Fatal("buttons not registered")
		}
		win.SendMessage(c.Handle(), win.BM_SETCHECK, 1, 0)
		if !c.Checked() {
			d.Dispose()
			t.Fatal("native checkbox input chain broken")
		}
		if i%2 == 0 {
			d.Dispose()
		} else {
			if !win.DestroyWindow(d.Handle()) {
				d.Dispose()
				t.Fatal("native DestroyWindow failed")
			}
		}
		if len(paintedButtons) != baseline {
			t.Fatalf("registry retained dialog %d: %d vs %d", i, len(paintedButtons), baseline)
		}
	}
}

func TestFinishAuditPersistsTerminalOnSyncCloseFault(t *testing.T) {
	a := testAuditApp(t)
	prior := errors.New("injected earlier transcript write fault")
	a.auditErr = prior
	// Closed handle deterministically makes both Sync and Close fail.
	if err := a.fullLog.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.finishAudit(event{done: true, code: 1}); err == nil || !errors.Is(err, prior) {
		t.Fatal(err)
	}
	if a.fullLog != nil {
		t.Fatal("retained handle")
	}
	r, err := a.store.LoadRun(a.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ended.IsZero() || r.Status == "running" || r.ExitCode != 1 || !strings.Contains(r.AuditError, prior.Error()) {
		t.Fatalf("terminal failure not persisted: %+v", r)
	}
}
func TestFinishAuditPersistsTerminalOnReadFault(t *testing.T) {
	a := testAuditApp(t)
	if err := a.fullLog.Close(); err != nil {
		t.Fatal(err)
	}
	a.fullLog = nil
	if err := os.Remove(a.runLogPath); err != nil {
		t.Fatal(err)
	}
	if err := a.finishAudit(event{done: true, code: 0}); err == nil {
		t.Fatal("expected missing-log error")
	}
	r, err := a.store.LoadRun(a.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ended.IsZero() || r.AuditError == "" || r.Status == "running" {
		t.Fatalf("terminal read error not persisted: %+v", r)
	}
}
func TestExportAtomicCompleteSnapshotAndFailurePreservesOriginal(t *testing.T) {
	a := testAuditApp(t)
	if _, err := a.fullLog.WriteString("capturedLATER"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "export.txt")
	if err := os.WriteFile(dest, []byte("original destination"), 0600); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(a.runLogPath)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.exportSnapshot(context.Background(), src, 8, "HEADER\n", dest, original); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil || string(b) != "HEADER\ncaptured" {
		t.Fatal(string(b), err)
	}
	src, err = os.Open(a.runLogPath)
	if err != nil {
		t.Fatal(err)
	}
	original, err = os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.exportSnapshot(context.Background(), src, 1000, "", dest, original); err == nil {
		t.Fatal("short export accepted")
	}
	b, err = os.ReadFile(dest)
	if err != nil || string(b) != "HEADER\ncaptured" {
		t.Fatal("failed export damaged original", err)
	}
	pending, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".robocopy-export-*.pending"))
	if err != nil || len(pending) != 0 {
		t.Fatal("partial export retained", pending, err)
	}
	for _, target := range []string{a.runLogPath, filepath.Join(a.store.Root, "runs", a.record.ID+".json"), filepath.Join(a.store.Root, "profiles", "future.json")} {
		if err = a.validateExport(target); err == nil {
			t.Errorf("managed overwrite accepted: %s", target)
		}
	}
}
func TestStoreBudgetCountsPendingAndRejectsHardlinks(t *testing.T) {
	a := testAuditApp(t)
	pending := filepath.Join(a.store.Root, "runs", "unknown.pending")
	f, err := os.Create(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(auditio.StoreBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err = a.storeUsage(); err == nil {
		t.Fatal("pending quota bypass")
	}
	if err = os.Remove(pending); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.log")
	if err = os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(external, pending); err != nil {
		t.Fatal(err)
	}
	if _, err = a.storeUsage(); err == nil {
		t.Fatal("hardlink accepted")
	}
	if err = a.validateExport(external); err == nil {
		t.Fatal("hardlinked export accepted")
	}
}
