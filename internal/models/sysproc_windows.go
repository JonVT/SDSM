//go:build windows

package models

import "syscall"

// newSysProcAttrDetached returns a Windows-appropriate SysProcAttr.
// Windows does not support POSIX Setpgid; return empty attributes.
func newSysProcAttrDetached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
