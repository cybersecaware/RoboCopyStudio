//go:build windows

package main

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lxn/walk"
	"robocopygui/internal/core"
)

func TestSummaryPlainLanguage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	summary, err := walk.NewTextEdit(owner)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{summary: summary, began: time.Now(), logText: "Files : 2 1 0 0 1 0\r\n"}
	a.updateSummary(event{preview: true, code: 8})
	text := summary.Text()
	for _, required := range []string{"About these results:", "does not separately compare the contents", "File and folder error details:", "No file or folder error details could be listed here.", "Check the FAILED column and the run result above"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing plain-language guidance %q", required)
		}
	}
	for _, obsolete := range []string{"Native counts are reported, not content verification", "Recognized native ERROR path evidence", "No recognized evidence; this does NOT prove no files failed"} {
		if strings.Contains(text, obsolete) {
			t.Fatalf("obsolete wording remains: %s", obsolete)
		}
	}
	// Known errors are still shown verbatim; only explanatory wording changes.
	evidence := "ERROR 5 (0x00000005) Copying File C:\\Fixture\\denied.txt"
	a.record.Failures = []core.Failure{{Evidence: evidence}}
	a.updateSummary(event{preview: true, code: 8})
	text = summary.Text()
	if !strings.Contains(text, evidence) || strings.Contains(text, "No file or folder error details could be listed here.") {
		t.Fatal("known error evidence or empty-state behavior changed")
	}
}
