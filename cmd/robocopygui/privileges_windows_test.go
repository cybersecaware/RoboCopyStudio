//go:build windows

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivilegeContext(t *testing.T) {
	elevated, err := currentProcessElevated()
	if err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("ROBOCOPY_EXPECT_ELEVATED"); expected != "" && elevated != (expected == "1") {
		t.Fatalf("elevation=%v, expected %s", elevated, expected)
	}
	if !strings.Contains(privilegeTitle(elevated), "RoboCopy Studio") {
		t.Fatal("missing title")
	}
	t.Logf("Actual process elevated: %v", elevated)
}

func TestPrivilegeHints(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		advisory bool
	}{
		{[]string{"/E", "/COPY:DAT"}, false},
		{[]string{"/MIR", "/Z"}, false},
		{[]string{"/B"}, true}, {[]string{"/zb"}, true},
		{[]string{"/COPYALL"}, true}, {[]string{"/SEC"}, true},
		{[]string{"/SECFIX"}, true}, {[]string{"/copy:datso"}, true},
		{[]string{"/COPY:U"}, true},
	} {
		hint := privilegeHint(false, tc.args)
		if strings.Contains(hint, "may need extra rights") != tc.advisory {
			t.Fatalf("wrong privilege advice for %v: %s", tc.args, hint)
		}
	}
	if !strings.Contains(privilegeHint(true, nil), "Administrator") {
		t.Fatal("missing elevated indicator")
	}
}

func TestPrivilegeElevationTransition(t *testing.T) {
	launchErr := errors.New("fixture launch failure")
	for _, tc := range []struct {
		name                     string
		consent                  bool
		launchErr                error
		launched, closed, failed bool
	}{
		{"decline", false, nil, false, false, false},
		{"uac-cancel", true, windows.ERROR_CANCELLED, true, false, false},
		{"launch-failure", true, launchErr, true, false, true},
		{"accepted-launch", true, nil, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launched, closed := false, false
			err := elevationTransition(func() bool { return tc.consent }, func() error { launched = true; return tc.launchErr }, func() { closed = true })
			if launched != tc.launched || closed != tc.closed || (err != nil) != tc.failed {
				t.Fatalf("launched=%v closed=%v error=%v", launched, closed, err)
			}
		})
	}
}
