//go:build !windows

package main

import "net/http"

// startTray no-op for non-Windows platforms.
func startTray(app *App, srv *http.Server, done chan struct{}) {
	close(done)
}
