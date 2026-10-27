//go:build unix

package core

import (
	"errors"
	"os"
	"syscall"
)

func managedLinkCount(_ *os.File, info os.FileInfo) (uint64, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("cannot verify managed file link count")
	}
	return uint64(st.Nlink), nil
}
