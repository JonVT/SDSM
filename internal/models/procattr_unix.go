//go:build !windows

package models

import (
	"os/exec"
	"syscall"
)

// setDetachedProcessGroup ensures the started process is placed into its own
// process group (Unix only) so it does not automatically receive signals sent
// to the parent. On Windows this is a no-op (see procattr_windows.go).
func setDetachedProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return
	}
	cmd.SysProcAttr.Setpgid = true
}
