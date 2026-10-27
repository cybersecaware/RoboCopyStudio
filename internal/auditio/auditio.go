// Package auditio provides bounded, cancellation-aware audit I/O.
package auditio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const ClipboardBytes = 1 << 20
const TailBytes = 256 << 10
const ChunkBytes = 32 << 10
const RunBytes int64 = 128 << 20
const StoreBytes int64 = 1 << 30
const StoreEntries = 10000
const Truncated = "\r\n[CLIPBOARD REPORT TRUNCATED at 1 MiB; use Export Run Log for the captured snapshot.]\r\n"

type SyncCloser interface {
	Sync() error
	Close() error
}

// Finalize always closes, including after failed durability or earlier writes.
func Finalize(f SyncCloser, prior error) error {
	if f == nil {
		return prior
	}
	return errors.Join(prior, f.Sync(), f.Close())
}

func Regular(f *os.File) (os.FileInfo, error) {
	s, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !s.Mode().IsRegular() {
		return nil, fmt.Errorf("audit input must be a regular file")
	}
	return s, nil
}

// Tail has a literal read bound, even if another writer keeps extending the file.
func Tail(f *os.File) ([]byte, error) {
	s, err := Regular(f)
	if err != nil {
		return nil, err
	}
	start := s.Size() - TailBytes
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, TailBytes))
}

func Clipboard(header string, r io.Reader) (string, error) {
	// Reserve room for the explicit notice before reading. Peek one byte to
	// distinguish exactly-full reports from truncated ones.
	limit := ClipboardBytes - len(Truncated)

	b, err := io.ReadAll(io.LimitReader(io.MultiReader(strings.NewReader(header), r), int64(limit+1)))
	if len(b) > limit {
		return string(b[:limit]) + Truncated, err
	}
	return string(b), err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// Snapshot writes exactly the click-time captured length; a short source is an
// error, not a silently successful partial export. Caller commits its temp file.
func Snapshot(ctx context.Context, dst io.Writer, header string, src io.Reader, size int64) error {
	if size < 0 {
		return fmt.Errorf("negative snapshot length")
	}
	if _, err := io.WriteString(dst, header); err != nil {
		return err
	}
	n, err := io.CopyBuffer(dst, contextReader{ctx, io.LimitReader(src, size)}, make([]byte, ChunkBytes))
	if err != nil {
		return err
	}
	if n != size {
		return io.ErrUnexpectedEOF
	}
	return ctx.Err()
}

// SafeLogTarget rejects Windows device namespaces, ADS and DOS reserved device
// components, but permits ordinary relative, drive-relative and UNC filenames.
func SafeLogTarget(path string) error {
	p := strings.ReplaceAll(path, "/", "\\")
	u := strings.ToUpper(p)
	if strings.HasPrefix(u, `\\`) {
		parts := strings.Split(strings.TrimPrefix(u, `\\`), `\`)
		if len(parts) > 1 && parts[1] == "PIPE" {
			return fmt.Errorf("native log named-pipe share rejected")
		}
	}
	if p == "" || strings.ContainsAny(p, "\x00\r\n") || strings.HasPrefix(u, `\\.\`) || strings.HasPrefix(u, `\\?\`) || strings.HasPrefix(u, `\??\`) || strings.HasPrefix(u, `\DEVICE\`) {
		return fmt.Errorf("native log device path rejected")
	}
	for i, c := range p {
		if c == ':' && !(i == 1 && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))) {
			return fmt.Errorf("native log alternate stream rejected")
		}
	}
	for _, part := range strings.Split(u, `\`) {
		name := strings.TrimRight(strings.SplitN(part, ".", 2)[0], " ")
		deviceNumber := strings.TrimPrefix(strings.TrimPrefix(name, "COM"), "LPT")
		if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || name == "CONIN$" || name == "CONOUT$" || ((strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && strings.Contains("|1|2|3|4|5|6|7|8|9|¹|²|³|", "|"+deviceNumber+"|")) {
			return fmt.Errorf("native log reserved device rejected")
		}
	}
	return nil
}
