//go:build windows

package models

import (
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
