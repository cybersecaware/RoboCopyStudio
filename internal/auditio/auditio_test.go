package auditio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

type faultFile struct {
	synced, closed    bool
	syncErr, closeErr error
}

func (f *faultFile) Sync() error  { f.synced = true; return f.syncErr }
func (f *faultFile) Close() error { f.closed = true; return f.closeErr }
func TestFinalizeAllErrorsAndAlwaysClose(t *testing.T) {
	prior, se, ce := errors.New("write fault"), errors.New("sync fault"), errors.New("close fault")
	f := &faultFile{syncErr: se, closeErr: ce}
	err := Finalize(f, prior)
	if !f.synced || !f.closed || !errors.Is(err, prior) || !errors.Is(err, se) || !errors.Is(err, ce) {
		t.Fatalf("missing cleanup/evidence: %+v %v", f, err)
	}
}

type repeatingReader struct {
	left int64
	read int64
}

func (r *repeatingReader) Read(b []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := len(b)
	if int64(n) > r.left {
		n = int(r.left)
	}
	for i := 0; i < n; i++ {
		b[i] = 'x'
	}
	r.left -= int64(n)
	r.read += int64(n)
	return n, nil
}
func TestClipboardLiteralBoundAndNotice(t *testing.T) {
	r := &repeatingReader{left: 1 << 30}
	text, err := Clipboard("header\r\n", r)
	if err != nil || len(text) > ClipboardBytes || !strings.HasSuffix(text, Truncated) || r.read > ClipboardBytes+1 {
		t.Fatalf("bound failed: %d %d %v", len(text), r.read, err)
	}
	text, err = Clipboard(strings.Repeat("h", 2*ClipboardBytes), strings.NewReader(""))
	if err != nil || len(text) > ClipboardBytes || !strings.HasSuffix(text, Truncated) {
		t.Fatal("oversize header lost notice")
	}
	text, err = Clipboard("header", strings.NewReader("small"))
	if err != nil || text != "headersmall" {
		t.Fatal(text, err)
	}
}
func TestStreamingSnapshotLargeCompleteAndGrowthExcluded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const size = 32 << 20
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	// Sparse fixture is larger than both the clipboard and copy buffer budgets.
	out := sha256.New()
	if err = Snapshot(context.Background(), out, "header\n", f, size); err != nil {
		t.Fatal(err)
	}
	expected := sha256.New()
	io.WriteString(expected, "header\n")
	zero := make([]byte, ChunkBytes)
	for written := 0; written < size; written += len(zero) {
		expected.Write(zero)
	}
	if !bytes.Equal(out.Sum(nil), expected.Sum(nil)) {
		t.Fatal("snapshot bytes differ")
	}
	var b bytes.Buffer
	if err = Snapshot(context.Background(), &b, "H", strings.NewReader("originalLATER"), 8); err != nil || b.String() != "Horiginal" {
		t.Fatal(b.String(), err)
	}
	if err = Snapshot(context.Background(), io.Discard, "", strings.NewReader("short"), 10); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = Snapshot(ctx, io.Discard, "", strings.NewReader("data"), 4); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestTailAndRegular(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(8 << 20); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("END"), (8<<20)-3); err != nil {
		t.Fatal(err)
	}
	b, err := Tail(f)
	if err != nil || len(b) != TailBytes || !bytes.HasSuffix(b, []byte("END")) {
		t.Fatal(len(b), err)
	}
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err = Regular(dir); err == nil {
		t.Fatal("accepted directory")
	}
}
func TestLogTargetDeviceRejectionAndValidPaths(t *testing.T) {
	for _, p := range []string{`\\.\pipe\capture`, `\\server\pipe\capture`, `\\?\C:\capture.log`, `\??\C:\capture.log`, `NUL`, `nul.txt`, `C:\logs\COM1.log`, `LPT¹.log`, `C:\file:stream`, `CONOUT$`, `C:\NUL \x.log`} {
		if SafeLogTarget(p) == nil {
			t.Errorf("accepted device %q", p)
		}
	}
	for _, p := range []string{`relative.log`, `..\logs\capture.log`, `C:relative.log`, `C:\logs\capture.log`, `\\server\share\capture.log`, `normal-COM1.log`} {
		if err := SafeLogTarget(p); err != nil {
			t.Errorf("rejected %q: %v", p, err)
		}
	}
}
func TestChunksBoundedUnicodeAndInvalidForwardProgress(t *testing.T) {
	for _, input := range []string{strings.Repeat("日本語😀", 100000), strings.Repeat("\x80", 10*ChunkBytes)} {
		var b strings.Builder
		EmitChunks(input, func(s string) bool {
			if len(s) == 0 || len(s) > ChunkBytes {
				t.Fatal("chunk bound")
			}
			if utf8.ValidString(input) && !utf8.ValidString(s) {
				t.Fatal("split UTF-8 rune")
			}
			b.WriteString(s)
			return true
		})
		if b.String() != input {
			t.Fatal("chunked text changed")
		}
	}
}

func TestRecordsBoundedWithoutStoppingRawCapture(t *testing.T) {
	var r Records
	var lines []string
	emit := func(s string) {
		if len(s) > ChunkBytes {
			t.Fatal("oversized line event")
		}
		lines = append(lines, s)
	}
	if r.Feed("first\rpartial", false, emit) {
		t.Fatal("premature limit")
	}
	if r.Feed(" end\n", false, emit) {
		t.Fatal("premature limit")
	}
	if !r.Feed(strings.Repeat("x", 2*ChunkBytes), false, emit) || !r.Limited || len(r.pending) > ChunkBytes {
		t.Fatal("record did not limit")
	}
	for i := 0; i < 100; i++ {
		if r.Feed(strings.Repeat("x", ChunkBytes), false, emit) {
			t.Fatal("duplicate limit event")
		}
	}
	r.Feed("discard suffix\nlast", true, emit)
	if strings.Join(lines, "|") != "first|partial end|last" {
		t.Fatalf("delimiter/resync: %q", lines)
	}
}

func TestBudgetWriterStopsWithoutOvershoot(t *testing.T) {
	var b bytes.Buffer
	stopped := false
	w := NewBudgetWriter(&b, 8, func() { stopped = true })
	if n, err := w.Write([]byte("12345678")); n != 8 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := w.Write([]byte("9")); n != 0 || err == nil || !stopped || b.Len() != 8 || w.Err() == nil {
		t.Fatal(n, err, stopped, b.Len())
	}
}
