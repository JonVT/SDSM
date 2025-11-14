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

// isPidAlive attempts to check whether a process with the given PID is alive on Unix
// by sending signal 0. It returns true when the process exists and the caller has
// permission to signal it.
func IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// kill(pid, 0) performs error checking without sending a signal
	err := syscall.Kill(pid, 0)
	return err == nil
}
