package core

import (
	"fmt"
	"strconv"
	"strings"
)

// RecommendedThreads is an initial I/O-worker suggestion, not a CPU-count cap.
func RecommendedThreads(logicalCPU int) int {
	if logicalCPU < 1 {
		return 1
	}
	if logicalCPU > 8 {
		return 8
	}
	return logicalCPU
}

func ThreadFlag(workers int) (string, error) {
	if workers < 1 || workers > 128 {
		return "", fmt.Errorf("/MT must be 1–128")
	}
	return fmt.Sprintf("/MT:%d", workers), nil
}

// RawThreads validates every raw MT occurrence, including overridden duplicates.
// Bare /MT uses Robocopy's documented default of eight workers.
func RawThreads(args []string) (workers int, present bool, err error) {
	for _, arg := range args {
		if SwitchName(arg) != "MT" {
			continue
		}
		present = true
		workers = 8
		if p := strings.SplitN(arg, ":", 2); len(p) == 2 {
			n, e := strconv.ParseUint(p[1], 10, 8)
			if e != nil || n < 1 || n > 128 {
				return 0, true, fmt.Errorf("/MT must be an integer 1–128")
			}
			workers = int(n)
		}
	}
	return
}

// ManagedThreads preserves non-MT arguments in order and canonicalizes repeated
// raw MT switches to their last value at the last occurrence. Raw MT explicitly
// overrides the quick selector; the GUI exposes that override and an adoption action.
func ManagedThreads(args []string, enabled bool, workers int) ([]string, error) {
	raw, present, err := RawThreads(args)
	if err != nil {
		return nil, err
	}
	if !present {
		out := append([]string{}, args...)
		if enabled {
			flag, err := ThreadFlag(workers)
			if err != nil {
				return nil, err
			}
			out = append([]string{flag}, out...)
		}
		return out, nil
	}
	last := -1
	for i, arg := range args {
		if SwitchName(arg) == "MT" {
			last = i
		}
	}
	flag, _ := ThreadFlag(raw)
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if SwitchName(arg) == "MT" {
			if i == last {
				out = append(out, flag)
			}
			continue
		}
		out = append(out, arg)
	}
	return out, nil
}
