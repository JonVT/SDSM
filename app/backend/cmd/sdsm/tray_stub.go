//go:build !windows

package main

import "net/http"

// startTray no-op for non-Windows platforms. Do not close 'done' so main does not exit.
func startTray(app *App, srv *http.Server, done chan struct{}) { /* no-op */ }

// trayQuit is a no-op on non-Windows platforms.
func trayQuit() {}
