//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"robocopygui/internal/auditio"
)

const documentationURL = "https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/robocopy"

// canonicalDirectory follows reparse points and resolves SUBST/drive mappings.
// It is a point-in-time guardrail, not a pinned traversal or cross-server
// identity guarantee. A directory handle is always closed, including API errors.
func canonicalDirectory(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	b := make([]uint16, 32768)
	n, readErr := windows.GetFinalPathNameByHandle(h, &b[0], uint32(len(b)), 0)
	closeErr := windows.CloseHandle(h)
	if err = errors.Join(readErr, closeErr); err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(b)) {
		return "", fmt.Errorf("resolved directory path exceeds Windows path budget")
	}
	path = windows.UTF16ToString(b[:n])
	if strings.HasPrefix(strings.ToUpper(path), `\\?\UNC\`) {
		path = `\\` + path[8:]
	} else {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("resolved directory has no absolute DOS/UNC path")
	}
	return filepath.Clean(path), nil
}

func documentationCommand() (*exec.Cmd, error) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	return exec.Command(filepath.Join(sys, "rundll32.exe"), filepath.Join(sys, "url.dll")+",FileProtocolHandler", documentationURL), nil
}
func (a *app) openAuditLog(id string) (*os.File, error) {
	path, err := a.store.Path("runs", id, ".log")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	_, statErr := auditio.Regular(f)
	var info windows.ByHandleFileInformation
	linkErr := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info)
	if err = errors.Join(statErr, linkErr); err == nil && info.NumberOfLinks != 1 {
		err = fmt.Errorf("managed audit hardlink rejected")
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// The store is ACL-protected, not encrypted. Refuse unsafe entries and stop at
// finite budgets; never prune evidence or enumerate outside these directories.
func (a *app) storeUsage() (int64, error) {
	var total int64
	entries := 0
	for _, kind := range []string{"", "runs", "profiles"} {
		dir := filepath.Join(a.store.Root, kind)
		info, err := os.Lstat(dir)
		if err != nil {
			return 0, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return 0, fmt.Errorf("unsafe audit directory")
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return 0, err
		}
		abs, err := filepath.Abs(dir)
		if err != nil || !strings.EqualFold(resolved, abs) {
			return 0, fmt.Errorf("audit directory alias rejected")
		}
		f, err := os.Open(dir)
		if err != nil {
			return 0, err
		}
		scanErr := func() error {
			defer f.Close()
			for {
				batch, err := f.ReadDir(64)
				for _, entry := range batch {
					entries++
					if entries > auditio.StoreEntries {
						return fmt.Errorf("audit store entry budget reached (%d); review and explicitly delete old history", auditio.StoreEntries)
					}
					path := filepath.Join(dir, entry.Name())
					info, err := os.Lstat(path)
					if err != nil {
						return err
					}
					if kind == "" && (entry.Name() == "runs" || entry.Name() == "profiles") && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
						continue
					}
					if !info.Mode().IsRegular() {
						return fmt.Errorf("nonregular audit store entry rejected")
					}
					file, err := os.Open(path)
					if err != nil {
						return err
					}
					st, statErr := auditio.Regular(file)
					var identity windows.ByHandleFileInformation
					linkErr := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &identity)
					closeErr := file.Close()
					if err := errors.Join(statErr, linkErr, closeErr); err != nil {
						return err
					}
					if identity.NumberOfLinks != 1 {
						return fmt.Errorf("audit store hardlink rejected")
					}
					if !os.SameFile(info, st) {
						return fmt.Errorf("audit entry identity changed during inspection")
					}
					if st.Size() < 0 || st.Size() > auditio.StoreBytes-total {
						return fmt.Errorf("audit store exceeds 1 GiB; explicitly delete selected old history (no automatic pruning)")
					}
					total += st.Size()
				}
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
			}
		}()
		if scanErr != nil {
			return 0, scanErr
		}
	}
	return total, nil
}

// All paths inside the managed store (including state JSON and pending files)
// are excluded as export destinations. Existing hardlinks/aliases are rejected.
func (a *app) validateExport(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := resolved(filepath.Dir(abs))
	if err != nil {
		return err
	}
	dest := filepath.Join(parent, filepath.Base(abs))
	root, err := resolved(a.store.Root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dest)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cannot export onto managed audit/state storage")
	}
	st, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("export destination must be an ordinary file")
	}
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 {
		return fmt.Errorf("export destination hardlink rejected")
	}
	return nil
}

func (a *app) exportSnapshot(ctx context.Context, source *os.File, size int64, header, dest string, original os.FileInfo) error {
	defer source.Close()
	if err := a.validateExport(dest); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".robocopy-export-*.pending")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	err = auditio.Snapshot(ctx, tmp, header, source, size)
	err = auditio.Finalize(tmp, err)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = a.validateExport(dest); err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	current, statErr := os.Lstat(dest)
	if original != nil {
		if statErr != nil || !os.SameFile(original, current) || original.Size() != current.Size() || !original.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("export destination changed since overwrite confirmation")
		}
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("export destination appeared after selection; choose and confirm again")
	}
	from, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	// Same-directory atomic replacement: no removal/truncation of an existing
	// destination until all bytes have copied and the temporary file is synced.
	return windows.MoveFileEx(from, to, flags)
}
