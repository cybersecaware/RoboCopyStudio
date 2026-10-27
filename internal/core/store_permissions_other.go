//go:build !windows

package core

import "os"

func (s Store) Secure() error {
	p, e := s.dir("profiles")
	if e != nil {
		return e
	}
	r, e := s.dir("runs")
	if e != nil {
		return e
	}
	for _, path := range []string{s.Root, p, r} {
		if e = os.Chmod(path, 0700); e != nil {
			return e
		}
	}
	return nil
}
