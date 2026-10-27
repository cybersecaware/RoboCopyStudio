package core

import (
	"errors"
	"os"
)

// Check the actual opened object as well as its pathname. This rejects ordinary
// hardlinks; it does not make later pathname rename/unlink operations race-free
// against an attacker concurrently modifying the store as the same user.
func validateManagedHandle(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("unsafe store file: not regular")
	}
	n, err := managedLinkCount(f, info)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("unsafe store file: hardlinks are not allowed (link count must be 1)")
	}
	return nil
}
