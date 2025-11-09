//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os/exec"
	"runtime"
	"sdsm/internal/version"
	"sdsm/ui"
	"time"

	ico "github.com/Kodeworks/golang-image-ico"
	"github.com/getlantern/systray"
)

// startTray implements a Windows system tray icon with basic controls.
func startTray(app *App, srv *http.Server, done chan struct{}) {
	// Load icon from embedded assets. On Windows systray expects .ico bytes.
	// Prefer sdsm.ico; if missing, convert embedded PNG to ICO at runtime.
	iconICO, _ := ui.Assets.ReadFile("static/sdsm.ico")
	iconPNG, _ := ui.Assets.ReadFile("static/sdsm.png")

	onReady := func() {
		// Try .ico directly
		if len(iconICO) > 0 {
			systray.SetIcon(iconICO)
		} else if len(iconPNG) > 0 {
			// Convert PNG -> ICO bytes
			if img, err := png.Decode(bytes.NewReader(iconPNG)); err == nil {
				// Ensure image is in a format ico.Encode accepts
				var buf bytes.Buffer
				if err := encodeICO(&buf, img); err == nil {
					systray.SetIcon(buf.Bytes())
				}
			}
		}
		systray.SetTitle("SDSM")
		systray.SetTooltip(fmt.Sprintf("SDSM %s", version.Version))

		mOpen := systray.AddMenuItem("Open UI", "Open SDSM Web UI")
		mLogs := systray.AddMenuItem("Open Logs Folder", "Open logs directory")
		mStopAll := systray.AddMenuItem("Stop All Servers", "Stop all running servers")
		mRestart := systray.AddMenuItem("Restart SDSM", "Restart the manager process")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop SDSM server")

		go func() {
			var confirmStopAll bool
			var confirmRestart bool
			for {
				select {
				case <-mOpen.ClickedCh:
					proto := "http"
					if app.tlsEnabled {
						proto = "https"
					}
					url := fmt.Sprintf("%s://localhost:%d", proto, app.manager.Port)
					if app.manager != nil && app.manager.Log != nil {
						app.manager.Log.Write("Tray: Open UI")
					}
					_ = launchBrowser(url)
				case <-mLogs.ClickedCh:
					if app.manager != nil && app.manager.Paths != nil {
						if app.manager.Log != nil {
							app.manager.Log.Write("Tray: Open Logs Folder")
						}
						_ = openPath(app.manager.Paths.LogsDir())
					}
				case <-mStopAll.ClickedCh:
					if !confirmStopAll {
						confirmStopAll = true
						mStopAll.SetTitle("Confirm Stop All")
						if app.manager != nil && app.manager.Log != nil {
							app.manager.Log.Write("Tray: Stop All requested - awaiting confirmation")
						}
						go func() {
							time.Sleep(4 * time.Second)
							if confirmStopAll {
								confirmStopAll = false
								mStopAll.SetTitle("Stop All Servers")
							}
						}()
						continue
					}
					confirmStopAll = false
					mStopAll.SetTitle("Stop All Servers")
					if app.manager != nil {
						if app.manager.Log != nil {
							app.manager.Log.Write("Tray: Stopping all servers")
						}
						for _, srv := range app.manager.Servers {
							if srv != nil && srv.IsRunning() {
								srv.Stop()
							}
						}
					}
				case <-mRestart.ClickedCh:
					if !confirmRestart {
						confirmRestart = true
						mRestart.SetTitle("Confirm Restart")
						if app.manager != nil && app.manager.Log != nil {
							app.manager.Log.Write("Tray: Restart requested - awaiting confirmation")
						}
						go func() {
							time.Sleep(4 * time.Second)
							if confirmRestart {
								confirmRestart = false
								mRestart.SetTitle("Restart SDSM")
							}
						}()
						continue
					}
					confirmRestart = false
					mRestart.SetTitle("Restart SDSM")
					if app.manager != nil {
						if app.manager.Log != nil {
							app.manager.Log.Write("Tray: Restarting SDSM")
						}
						go app.manager.Restart()
					}
				case <-mQuit.ClickedCh:
					if app.manager != nil && app.manager.Log != nil {
						app.manager.Log.Write("Tray: Quit")
					}
					systray.Quit()
				}
			}
		}()
	}

	onExit := func() {
		close(done)
	}

	systray.Run(onReady, onExit)
}

// encodeICO wraps ico.Encode to allow future multi-size support if desired.
func encodeICO(buf *bytes.Buffer, img image.Image) error {
	return ico.Encode(buf, img)
}

func launchBrowser(url string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

func openPath(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}
