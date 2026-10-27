//go:build windows

package core

import (
	"golang.org/x/sys/windows"
)

// Secure restricts the per-user store to its current account, SYSTEM and local
// administrators. Unix permission bits alone do not enforce Windows ACLs.
func (s Store) Secure() error {
	profiles, e := s.dir("profiles")
	if e != nil {
		return e
	}
	runs, e := s.dir("runs")
	if e != nil {
		return e
	}
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return e
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if e != nil {
		return e
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	for _, p := range []string{s.Root, profiles, runs} {
		if e = windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
			return e
		}
	}
	return nil
}
