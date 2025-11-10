//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

var (
	modKernel32          = syscall.NewLazyDLL("kernel32.dll")
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow = modKernel32.NewProc("GetConsoleWindow")
	procShowWindow       = modUser32.NewProc("ShowWindow")
)

const (
	swHide = 0
	// swShow = 5 // reserved for potential future use
)

// hideConsoleWindow hides the current process console window if present.
// When running from a console host, this effectively backgrounds the app while tray is active.
func hideConsoleWindow() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	procShowWindow.Call(hwnd, uintptr(swHide))
}

// spawnDetachedIfNeeded starts a detached copy of the current process and returns true
// if the parent should exit immediately. This is used to allow the console to return
// when running with the Windows tray enabled. It will only spawn if a console window
// is present and SDSM_BACKGROUND is not already set.
func spawnDetachedIfNeeded(trayEnabled bool) bool {
	if !trayEnabled {
		return false
	}
	// Avoid loops
	if os.Getenv("SDSM_BACKGROUND") == "1" {
		return false
	}
	// Only detach if a console window exists (i.e., started from a console)
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return false
	}
	args := os.Args[1:]
	cmd := exec.Command(exe, args...)
	env := append(os.Environ(), "SDSM_BACKGROUND=1")
	cmd.Env = env
	// Detach from console and hide any window on the child
	const (
		DETACHED_PROCESS         = 0x00000008
		CREATE_NEW_PROCESS_GROUP = 0x00000200
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err == nil {
		return true
	}
	return false
}
