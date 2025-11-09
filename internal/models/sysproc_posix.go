//go:build linux || darwin

package models

import "syscall"

// newSysProcAttrDetached returns a SysProcAttr configured to start the child in a new
// process group (POSIX only). On unsupported platforms callers should treat nil as no-op.
func newSysProcAttrDetached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
