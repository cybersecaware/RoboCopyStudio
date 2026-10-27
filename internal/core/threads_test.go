package core

import (
	"reflect"
	"testing"
)

func TestRecommendedThreads(t *testing.T) {
	for _, tc := range []struct{ cpu, want int }{{0, 1}, {1, 1}, {2, 2}, {4, 4}, {8, 8}, {32, 8}} {
		if got := RecommendedThreads(tc.cpu); got != tc.want {
			t.Fatalf("cpu %d: got %d want %d", tc.cpu, got, tc.want)
		}
	}
}

func TestManagedThreadsEffectiveAndRawEquivalence(t *testing.T) {
	for _, n := range []int{1, 2, 4, 128} {
		flag, err := ThreadFlag(n)
		if err != nil {
			t.Fatal(err)
		}
		base := []string{"/E", "/BYTES", "/FP", "/R:2"}
		quick, err := ManagedThreads(base, true, n)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := ManagedThreads(append([]string{flag}, base...), false, 8)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(quick, raw) {
			t.Fatalf("quick/raw mismatch: %v %v", quick, raw)
		}
		o := Options{Source: `C:\src`, Destination: `D:\dst`, Custom: true, Extra: quick}
		cmd, err := Build(o, false)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, arg := range cmd {
			if SwitchName(arg) == "MT" {
				count++
				if arg != flag {
					t.Fatalf("wrong effective flag %s", arg)
				}
			}
		}
		if count != 1 {
			t.Fatalf("got %d MT switches", count)
		}
		if Telemetry(quick) {
			t.Fatal("MT must disable per-file estimates")
		}
	}
}

func TestManagedThreadsOverridePreservesOthers(t *testing.T) {
	args := []string{"*.txt", "/MT:2", "/XF", "a b.tmp", "/mt:4", "/LOG+:audit.log", "/SAVE:job"}
	want := []string{"*.txt", "/XF", "a b.tmp", "/MT:4", "/LOG+:audit.log", "/SAVE:job"}
	got, err := ManagedThreads(args, true, 128)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v err %v", got, err)
	}
	n, present, err := RawThreads([]string{"/MT"})
	if n != 8 || !present || err != nil {
		t.Fatalf("bare MT: %d %v %v", n, present, err)
	}
	got, err = ManagedThreads([]string{"/E"}, false, 0)
	if err != nil || !reflect.DeepEqual(got, []string{"/E"}) {
		t.Fatal(got, err)
	}
}

func TestManagedThreadsRejectInvalid(t *testing.T) {
	for _, n := range []int{0, 129} {
		if _, err := ThreadFlag(n); err == nil {
			t.Fatalf("accepted selector %d", n)
		}
		if _, err := ManagedThreads(nil, true, n); err == nil {
			t.Fatalf("accepted effective %d", n)
		}
	}
	for _, arg := range []string{"/MT:0", "/MT:129", "/MT:", "/MT:1.5", "/MT:-1", "/MT:two"} {
		if _, _, err := RawThreads([]string{arg}); err == nil {
			t.Fatalf("accepted raw %s", arg)
		}
		if _, err := ManagedThreads([]string{arg, "/MT:4"}, true, 2); err == nil {
			t.Fatalf("silently hid invalid duplicate %s", arg)
		}
	}
}
