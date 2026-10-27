package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStoreSecureAndAliasNoCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	s := Store{Root: root}
	if e := s.Secure(); e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{root, filepath.Join(root, "profiles"), filepath.Join(root, "runs")} {
			st, e := os.Stat(p)
			if e != nil || st.Mode().Perm() != 0700 {
				t.Fatalf("permissions %s: %v %v", p, st, e)
			}
		}
	}
	outside := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(outside, alias); e != nil {
		t.Skip(e)
	}
	bad := Store{Root: filepath.Join(alias, "must-not-create")}
	if _, e := bad.IDs("runs"); e == nil {
		t.Fatal("accepted aliased parent")
	}
	if _, e := os.Stat(filepath.Join(outside, "must-not-create")); !os.IsNotExist(e) {
		t.Fatal("created store outside scope", e)
	}
}
func TestRunDeleteRejectsLogAliasBeforeDeletingManifest(t *testing.T) {
	s := Store{Root: t.TempDir()}
	id, _ := NewID()
	if e := s.SaveRun(RunRecord{SchemaVersion: 1, ID: id}); e != nil {
		t.Fatal(e)
	}
	log, _ := s.Path("runs", id, ".log")
	outside := filepath.Join(t.TempDir(), "outside.log")
	os.WriteFile(outside, []byte("keep"), 0600)
	if e := os.Symlink(outside, log); e != nil {
		t.Skip(e)
	}
	if e := s.DeleteRun(id); e == nil {
		t.Fatal("removed aliased log")
	}
	if _, e := s.LoadRun(id); e != nil {
		t.Fatal("manifest should remain", e)
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "keep" {
		t.Fatal("outside changed")
	}
}
func TestFailureEvidenceBudgetAndDedupe(t *testing.T) {
	f := Failure{Evidence: "one", Path: "one"}
	v := AppendFailure(nil, f)
	v = AppendFailure(v, f)
	if len(v) != 1 {
		t.Fatal("duplicate")
	}
	if len(AppendFailure(v, Failure{Evidence: strings.Repeat("x", 1<<20)})) != 1 {
		t.Fatal("evidence byte budget")
	}
	many := make([]Failure, 1000)
	if len(AppendFailure(many, f)) != 1000 {
		t.Fatal("record limit")
	}
}
func TestExpertBannerDoesNotClaimInactiveDestination(t *testing.T) {
	s := DangerBanner([]string{"/MIR"}, true, `D:\inactive-stale-path`)
	if strings.Contains(s, `D:\inactive-stale-path`) || !strings.Contains(s, "JOB destination") {
		t.Fatal(s)
	}
}
func TestOversizedManifestRejected(t *testing.T) {
	s := Store{Root: t.TempDir()}
	id, _ := NewID()
	if e := s.SaveRun(RunRecord{SchemaVersion: 1, ID: id, Command: strings.Repeat("x", maxManifestSize)}); e == nil {
		t.Fatal("oversized manifest written")
	}
	p, _ := s.Path("profiles", id, ".json")
	if e := os.WriteFile(p, []byte(strings.Repeat(" ", maxManifestSize)+"{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.LoadProfile(id); e == nil {
		t.Fatal("oversized manifest read")
	}
}
func TestRunDescriptionAuditAndCounters(t *testing.T) {
	r := RunRecord{Status: "finished", AuditError: "disk full", Summary: map[string][]string{"Bytes": {"100", "50", "50", "0", "0", "0"}}}
	s := RunDescription(r)
	if !strings.Contains(s, "AUDIT INCOMPLETE") || !strings.Contains(s, "50 B native copied") {
		t.Fatal(s)
	}
}
