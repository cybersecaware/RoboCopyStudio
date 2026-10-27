//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"reflect"
	"robocopygui/internal/core"
	"strings"
	"time"
)

// smoke uses exactly the same process/log/parser path as the GUI. All copy and
// deletion targets are inside a freshly created temporary fixture directory.
func smoke(output string) int {
	result := map[string]any{"ok": false, "platform": "windows/amd64", "started": time.Now().UTC()}
	elevated, privilegeErr := currentProcessElevated()
	if privilegeErr != nil {
		result["privilege_error"] = privilegeErr.Error()
	}
	result["elevated"] = elevated
	var failure error
	err := func() error {
		if privilegeErr != nil {
			return fmt.Errorf("read process privileges: %w", privilegeErr)
		}
		root, e := os.MkdirTemp("", "robocopy-studio-smoke-*")
		if e != nil {
			return e
		}
		defer os.RemoveAll(root)
		src, dst := filepath.Join(root, "source"), filepath.Join(root, "destination")
		os.MkdirAll(filepath.Join(src, "nested"), 0700)
		os.MkdirAll(dst, 0700)
		fixtures := map[string][]byte{"hello.txt": []byte("RoboCopy Studio smoke test\r\n"), "empty.bin": {}, "nested/日本語 space.txt": bytes.Repeat([]byte("test-content-"), 8192)}
		for name, b := range fixtures {
			if e := os.WriteFile(filepath.Join(src, name), b, 0600); e != nil {
				return e
			}
		}
		if e := os.WriteFile(filepath.Join(dst, "delete-only-in-smoke.txt"), []byte("extra"), 0600); e != nil {
			return e
		}
		if e := validate(src, dst); e != nil {
			return e
		}
		a := &app{events: make(chan event, 32)}
		a.store = core.Store{Root: filepath.Join(root, "disposable store")}
		if e = a.store.Secure(); e != nil {
			return fmt.Errorf("store ACL setup: %w", e)
		}
		profileID, e := core.NewID()
		if e != nil {
			return e
		}
		profile := core.Profile{SchemaVersion: 1, Name: "Unicode 日本語", Source: src, Destination: dst, Raw: "/E /BYTES /FP", Mirror: true, Restart: true, MT: true, Workers: 4}
		if e = a.store.SaveProfile(profileID, profile); e != nil {
			return e
		}
		loaded, e := a.store.LoadProfile(profileID)
		if e != nil || !reflect.DeepEqual(profile, loaded) {
			return fmt.Errorf("native profile roundtrip: %v", e)
		}
		profile.Workers = 128
		if e = a.store.SaveProfile(profileID, profile); e != nil {
			return fmt.Errorf("native atomic replacement: %w", e)
		}
		loaded, e = a.store.LoadProfile(profileID)
		if e != nil || loaded.Workers != 128 {
			return fmt.Errorf("profile replacement not retained: %v", e)
		}
		if e = a.store.DeleteProfile(profileID); e != nil {
			return e
		}
		result["profile_roundtrip_atomic_replace_delete"] = true
		var ioSamples int
		var maxIO uint64
		auditRuns := 0
		o := core.Options{Source: src, Destination: dst, Custom: true, Extra: []string{"/E", "/Z", "/R:2", "/W:2", "/XJ", "/BYTES", "/FP", "/COPY:DAT", "/DCOPY:DAT", "/MIR"}}
		p := core.NewPlan()
		var tracker *core.Tracker
		var transcript string
		run := func(preview bool) (int, error) {
			transcript = ""
			id, e := core.NewID()
			if e != nil {
				return -1, e
			}
			a.fullLog, e = a.store.CreateLog(id)
			if e != nil {
				return -1, e
			}
			a.runLogPath = a.fullLog.Name()
			a.record = core.RunRecord{SchemaVersion: 1, ID: id, Started: time.Now().UTC(), Status: "running", ExitCode: -1, Source: o.Source, Destination: o.Destination, Preview: preview, MT: core.Has(o.Extra, "MT"), PlannedBytes: p.Bytes, PlannedFiles: len(p.Files)}
			if e = a.store.SaveRun(a.record); e != nil {
				return -1, e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			go a.run(ctx, o, preview)
			for {
				ev := <-a.events
				transcript += ev.text
				if ev.commandText != "" {
					a.record.Command = ev.commandText
				}
				if ev.text != "" {
					if _, e = a.fullLog.WriteString(ev.text); e != nil {
						return -1, e
					}
				}
				if f, ok := core.FailureLine(ev.line); ok {
					a.record.Failures = core.AppendFailure(a.record.Failures, f)
				}
				if ev.ioValid {
					ioSamples++
					if ev.ioBytes > maxIO {
						maxIO = ev.ioBytes
					}
				}
				if !preview && tracker != nil && ev.line != "" {
					tracker.Feed(ev.line)
				}
				if ev.done {
					if e = a.finishAudit(ev); e != nil {
						return -1, e
					}
					persisted, e := a.store.LoadRun(id)
					if e != nil || persisted.Status == "running" || persisted.Command == "" {
						return -1, fmt.Errorf("native run manifest not finalized: %v", e)
					}
					full, e := os.ReadFile(a.runLogPath)
					if e != nil || string(full) != transcript {
						return -1, fmt.Errorf("persistent native log differs from complete transcript: %v", e)
					}
					auditRuns++
					if preview {
						p = ev.plan
					}
					return ev.code, ev.err
				}
			}
		}
		code, e := run(true)
		result["preview_exit"] = code
		if e != nil {
			return e
		}
		if code >= 8 {
			return fmt.Errorf("preview exit %d", code)
		}
		result["planned_files"] = len(p.Files)
		result["planned_bytes"] = p.Bytes
		var total int64
		for _, b := range fixtures {
			total += int64(len(b))
		}
		if len(p.Files) != len(fixtures) || p.Bytes != total {
			return fmt.Errorf("localized preflight parser mismatch: got %d files / %d bytes; want %d / %d", len(p.Files), p.Bytes, len(fixtures), total)
		}
		if _, e := os.Stat(filepath.Join(dst, "hello.txt")); !os.IsNotExist(e) {
			return fmt.Errorf("preview unexpectedly created a destination file")
		}
		if _, e := os.Stat(filepath.Join(dst, "delete-only-in-smoke.txt")); e != nil {
			return fmt.Errorf("preview unexpectedly removed extra file: %v", e)
		}
		tracker = core.NewTracker(p, src)
		code, e = run(false)
		result["copy_exit"] = code
		if e != nil {
			return e
		}
		if code >= 8 {
			return fmt.Errorf("copy exit %d", code)
		}
		for name, b := range fixtures {
			got, e := os.ReadFile(filepath.Join(dst, name))
			if e != nil {
				return e
			}
			if !bytes.Equal(got, b) {
				return fmt.Errorf("content mismatch: %s", name)
			}
		}
		if _, e := os.Stat(filepath.Join(dst, "delete-only-in-smoke.txt")); !os.IsNotExist(e) {
			return fmt.Errorf("mirror did not delete fixture extra")
		}
		progressBytes, progressFiles := tracker.Estimate()
		result["progress_bytes"] = progressBytes
		result["progress_files"] = progressFiles
		if progressBytes != total || progressFiles != len(fixtures) {
			return fmt.Errorf("native progress mismatch: got %d files / %d bytes; want %d / %d", progressFiles, progressBytes, len(fixtures), total)
		}
		result["verified_files"] = len(fixtures)
		result["verified_bytes"] = total
		result["preview_read_only"] = true
		result["mirror_deletion_verified"] = true
		// Exercise the real GUI custom argument/capture path, not only formatting.
		// Give process counters a nontrivial workload without using destination
		// file length as evidence: allocation is not proof of completed transfer.
		fixtures["aggregate activity.bin"] = bytes.Repeat([]byte("MT activity fixture\n"), 1<<20)
		if e = os.WriteFile(filepath.Join(src, "aggregate activity.bin"), fixtures["aggregate activity.bin"], 0600); e != nil {
			return e
		}
		total += int64(len(fixtures["aggregate activity.bin"]))
		os.MkdirAll(filepath.Join(src, "excluded dir"), 0700)
		for _, name := range []string{"skip.tmp", "old.txt", "excluded dir/hidden.txt"} {
			if e := os.WriteFile(filepath.Join(src, name), []byte("must not copy"), 0600); e != nil {
				return e
			}
		}
		old := time.Now().Add(-30 * 24 * time.Hour)
		if e := os.Chtimes(filepath.Join(src, "old.txt"), old, old); e != nil {
			return e
		}
		dst2 := filepath.Join(root, "filtered destination 日本語")
		nativeLog := filepath.Join(root, "user selected log 日本語.txt")
		o = core.Options{Source: src, Destination: dst2, Custom: true, Extra: []string{"*.*", "/E", "/Z", "/R:2", "/W:2", "/XJ", "/BYTES", "/FP", "/COPY:DAT", "/DCOPY:T", "/MT:16", "/XF", "*.tmp", "/XD", filepath.Join(src, "excluded dir"), "/MAXAGE:7", "/UNILOG:" + nativeLog}}
		if core.Telemetry(o.Extra) {
			return fmt.Errorf("MT must disable per-file estimates")
		}
		tracker = nil
		args, e := core.Build(o, false)
		if e != nil {
			return e
		}
		result["advanced_command"] = core.Command(args)
		code, e = run(true)
		if e != nil || code >= 8 {
			return fmt.Errorf("advanced preview exit %d: %v", code, e)
		}
		result["advanced_preview_exit"] = code
		if _, e := os.Stat(filepath.Join(dst2, "hello.txt")); !os.IsNotExist(e) {
			return fmt.Errorf("advanced preview wrote file")
		}
		code, e = run(false)
		if e != nil || code >= 8 {
			return fmt.Errorf("advanced copy exit %d: %v", code, e)
		}
		result["advanced_copy_exit"] = code
		for name, b := range fixtures {
			got, e := os.ReadFile(filepath.Join(dst2, name))
			if e != nil {
				return e
			}
			if !bytes.Equal(got, b) {
				return fmt.Errorf("advanced content mismatch: %s", name)
			}
		}
		for _, name := range []string{"skip.tmp", "old.txt", "excluded dir/hidden.txt"} {
			if _, e := os.Stat(filepath.Join(dst2, name)); !os.IsNotExist(e) {
				return fmt.Errorf("advanced filters failed: %s", name)
			}
		}
		logBytes, e := os.ReadFile(nativeLog)
		if e != nil || len(logBytes) < 2 {
			return fmt.Errorf("user Unicode log was not written: %v", e)
		}
		if !strings.Contains(core.DecodeLog(logBytes), "日本語 space.txt") {
			return fmt.Errorf("selected log lost Unicode filename")
		}
		result["advanced_summary"] = core.Summary(transcript)
		summary := core.Summary(transcript)
		if len(summary["Files"]) != 6 || summary["Files"][1] != fmt.Sprint(len(fixtures)) || len(summary["Bytes"]) != 6 || summary["Bytes"][1] != fmt.Sprint(total) {
			return fmt.Errorf("advanced native capture summary mismatch: %v; want %d copied files / %d copied bytes", summary, len(fixtures), total)
		}
		if !strings.Contains(transcript, "日本語 space.txt") {
			return fmt.Errorf("native capture lost Unicode filename")
		}
		result["advanced_filters_verified"] = []string{"XF *.tmp", "XD excluded dir", "MAXAGE 7"}
		result["advanced_verified_files"] = len(fixtures)
		result["advanced_verified_bytes"] = total
		result["mt_live_estimates_disabled"] = true
		result["mt_io_samples"] = ioSamples
		result["mt_process_write_activity_bytes"] = maxIO
		result["mt_activity_is_not_copied_bytes"] = true
		if ioSamples == 0 || maxIO == 0 {
			return fmt.Errorf("no process-write activity observed during native MT fixture: %d samples / %d bytes", ioSamples, maxIO)
		}
		// A real locked source fixture produces native failed-file evidence, not
		// synthetic native-looking output. Only this disposable tree is involved.
		failureSource := filepath.Join(root, "locked source")
		failureDest := filepath.Join(root, "failed destination")
		if e = os.MkdirAll(failureSource, 0700); e != nil {
			return e
		}
		lockedPath := filepath.Join(failureSource, "locked 日本語.txt")
		if e = os.WriteFile(lockedPath, []byte("locked fixture"), 0600); e != nil {
			return e
		}
		wide, e := windows.UTF16PtrFromString(lockedPath)
		if e != nil {
			return e
		}
		locked, e := windows.CreateFile(wide, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if e != nil {
			return e
		}
		o = core.Options{Source: failureSource, Destination: failureDest, Custom: true, Extra: []string{"/E", "/R:0", "/W:0", "/BYTES", "/FP"}}
		code, e = run(false)
		windows.CloseHandle(locked)
		if e != nil || code < 8 {
			return fmt.Errorf("locked fixture should fail natively: exit %d: %v", code, e)
		}
		if len(a.record.Failures) == 0 {
			return fmt.Errorf("no recognized native failed-file path evidence for locked fixture")
		}
		foundLocked := false
		for _, f := range a.record.Failures {
			if strings.EqualFold(f.Path, lockedPath) {
				foundLocked = true
			}
		}
		if !foundLocked {
			return fmt.Errorf("native failure evidence did not identify scoped locked fixture: %v", a.record.Failures)
		}
		result["native_failed_file_evidence_verified"] = true
		result["locked_fixture_exit"] = code
		result["persistent_native_audit_runs"] = auditRuns
		ids, e := a.store.IDs("runs")
		if e != nil || len(ids) != auditRuns {
			return fmt.Errorf("history inventory mismatch: %v", e)
		}
		for _, id := range ids {
			if e = a.store.DeleteRun(id); e != nil {
				return e
			}
		}
		ids, e = a.store.IDs("runs")
		if e != nil || len(ids) != 0 {
			return fmt.Errorf("history deletion failed: %v", e)
		}
		result["history_log_roundtrip_and_scoped_delete"] = true
		result["user_unicode_log_verified"] = true
		return nil
	}()
	failure = err
	if failure != nil {
		result["error"] = failure.Error()
	} else {
		result["ok"] = true
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	b = append(b, '\n')
	fmt.Print(string(b))
	if output != "" {
		if e := os.WriteFile(output, b, 0600); e != nil {
			return 2
		}
	}
	if failure != nil {
		return 1
	}
	return 0
}
