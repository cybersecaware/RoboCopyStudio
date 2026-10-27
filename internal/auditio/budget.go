package auditio

import (
	"fmt"
	"io"
	"sync"
)

// BudgetWriter prevents diagnostic stdout/stderr from exhausting storage.
// It is not used for telemetry. Exceeding the budget cancels the owned process.
type BudgetWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int64
	stop      func()
	err       error
}

func NewBudgetWriter(w io.Writer, limit int64, stop func()) *BudgetWriter {
	return &BudgetWriter{writer: w, remaining: limit, stop: stop}
}
func (w *BudgetWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if int64(len(b)) > w.remaining {
		w.err = fmt.Errorf("native diagnostic spool budget exceeded; audit incomplete")
		w.stop()
		return 0, w.err
	}
	n, err := w.writer.Write(b)
	w.remaining -= int64(n)
	if err != nil {
		w.err = err
		w.stop()
	}
	return n, err
}
func (w *BudgetWriter) Err() error { w.mu.Lock(); defer w.mu.Unlock(); return w.err }
