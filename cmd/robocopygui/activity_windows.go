//go:build windows

package main

import (
	"fmt"

	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"robocopygui/internal/core"
)

var processIOProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessIoCounters")

type processIO struct{ ReadOperations, WriteOperations, OtherOperations, ReadBytes, WriteBytes, OtherBytes uint64 }

func readProcessWrites(h windows.Handle) (uint64, bool) {
	var v processIO
	r, _, _ := processIOProc.Call(uintptr(h), uintptr(unsafe.Pointer(&v)))
	return v.WriteBytes, r != 0
}
func (a *app) showActivity(written uint64) {
	if !a.busy || !core.Has(a.opts.Extra, "MT") {
		return
	}
	a.amount.SetText("I/O " + core.Bytes(int64(written)))
	a.remaining.SetText("Not measured")
	now := time.Now()
	interval := now.Sub(a.lastSample).Seconds()
	if interval > 0 {
		active := float64(int64(written)-a.lastSampleBytes) / interval
		if active < 0 {
			active = 0
		}
		if active > a.peak {
			a.peak = active
		}
		a.speed.SetText(fmt.Sprintf("I/O %s/s\r\nPeak %s/s", core.Bytes(int64(active)), core.Bytes(int64(a.peak))))
		a.lastSample = now
		a.lastSampleBytes = int64(written)
	}
	a.eta.SetText("ETA unavailable\r\nMT aggregate I/O")
	if value, ok := core.ActivityEstimate(written, a.plan.Bytes); ok {
		a.progress.SetMarqueeMode(false)
		a.progress.SetValue(value)
		a.details.SetText(fmt.Sprintf("MT I/O-budget proxy ≈ %.1f%% (capped 95%%). Writes include logs, metadata and retries; NOT copied bytes or completion.", float64(value)/10))
	} else {
		a.details.SetText("MT live process-write activity only: includes logs, metadata and retries. No approved byte budget; completion / ETA unknown.")
	}
	a.current.SetText("No per-file attribution for interleaved MT output. Final copied counters replace this I/O proxy from native summary.")
}
