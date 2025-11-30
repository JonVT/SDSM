//go:build windows

package models

import (
	win "golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

// Windows does not support the Unix SysProcAttr.Setpgid field; this is a no-op.
func setDetachedProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	// CREATE_NEW_PROCESS_GROUP (0x00000200) allows sending Ctrl signals to groups; DETACHED_PROCESS (0x00000008)
	// fully detaches from console. Use CREATE_NEW_PROCESS_GROUP to mirror Unix Setpgid semantics without losing
	// all console association (logs still flow). If full detach desired, add flag 0x00000008.
	const createNewProcessGroup = 0x00000200
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
		return
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

// IsPidAlive attempts to determine if a PID is still running on Windows using
// OpenProcess + GetExitCodeProcess. When the exit code is STILL_ACTIVE (259),
// the process is considered alive.
func IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Minimal rights for querying exit code and synchronization
	const desired = win.PROCESS_QUERY_LIMITED_INFORMATION | win.SYNCHRONIZE
	h, err := win.OpenProcess(desired, false, uint32(pid))
	if err != nil {
		return false
	}
	defer win.CloseHandle(h)
	var code uint32
	if err := win.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const STILL_ACTIVE = 259
	return code == STILL_ACTIVE
}
