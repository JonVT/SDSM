//go:build !windows

package main

// hideConsoleWindow is a no-op on non-Windows platforms.
func hideConsoleWindow() {}

// spawnDetachedIfNeeded is a no-op on non-Windows; always returns false (do not re-spawn).
func spawnDetachedIfNeeded(trayEnabled bool) bool { return false }
