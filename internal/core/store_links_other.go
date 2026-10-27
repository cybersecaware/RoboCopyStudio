//go:build !unix && !windows

package core

import (
	"errors"
	"os"
)

func managedLinkCount(_ *os.File, _ os.FileInfo) (uint64, error) {
	return 0, errors.New("managed file link verification unsupported on this platform")
}
