// Package utils contains utility types for logging, process control, and
// filesystem path management used throughout SDSM.
package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Paths resolves and manages filesystem locations used by SDSM.
type Paths struct {
	RootPath string `json:"root_path"`
}

// NewPaths constructs Paths rooted at the specified directory.
func NewPaths(rootPath string) *Paths {
	return &Paths{RootPath: rootPath}
}

// SteamDir returns the path to the SteamCMD directory.
func (p *Paths) SteamDir() string {
	return filepath.Join(p.RootPath, "bin", "steamcmd")
}

// ReleaseDir returns the install path for the Stationeers release channel.
func (p *Paths) ReleaseDir() string {
	return filepath.Join(p.RootPath, "bin", "release")
}

// BetaDir returns the install path for the Stationeers beta channel.
func (p *Paths) BetaDir() string {
	return filepath.Join(p.RootPath, "bin", "beta")
}

// BepInExDir returns the root directory for BepInEx files.
func (p *Paths) BepInExDir() string {
	return filepath.Join(p.RootPath, "bin", "BepInEx")
}

// SCONDir returns the directory where SCON is installed.
func (p *Paths) SCONDir() string {
	return filepath.Join(p.RootPath, "bin", "SCON")
}

// LaunchPadDir returns the directory where Stationeers LaunchPad is installed.
func (p *Paths) LaunchPadDir() string {
	return filepath.Join(p.RootPath, "bin", "launchpad")
}

// LogsDir returns the global logs directory for SDSM.
func (p *Paths) LogsDir() string {
	return filepath.Join(p.RootPath, "logs")
}

// ConfigDir returns the application configuration directory.
func (p *Paths) ConfigDir() string {
	return filepath.Join(p.RootPath, "config")
}

// UsersFile returns the path to the user database file.
func (p *Paths) UsersFile() string {
	return filepath.Join(p.ConfigDir(), "users.json")
}

// LogFile returns the main SDSM log file path.
func (p *Paths) LogFile() string {
	return filepath.Join(p.LogsDir(), "sdsm.log")
}

// UpdateLogFile returns the path to the update log file.
func (p *Paths) UpdateLogFile() string {
	return filepath.Join(p.LogsDir(), "updates.log")
}

// CheckRoot verifies that core directories exist under the root path.
func (p *Paths) CheckRoot() bool {
	dirs := []string{p.RootPath, p.SteamDir(), p.ReleaseDir(), p.BetaDir(), p.BepInExDir(), p.SCONDir(), p.LaunchPadDir(), p.LogsDir()}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// DeployRoot creates the root directory structure (idempotent).
func (p *Paths) DeployRoot(logger *Logger) {
	// Helper to create a directory and log a standardized message
	mkdirLog := func(path, label string) {
		_ = os.MkdirAll(path, os.ModePerm)
		if logger != nil {
			logger.Write(fmt.Sprintf("Creating %s path: %s", label, path))
		}
	}

	mkdirLog(p.RootPath, "root")
	mkdirLog(p.SteamDir(), "steam")
	mkdirLog(p.ReleaseDir(), "release")
	mkdirLog(p.BetaDir(), "beta")
	mkdirLog(p.BepInExDir(), "BepInEx")
	mkdirLog(p.SCONDir(), "SCON")
	mkdirLog(p.LaunchPadDir(), "launchpad")
	mkdirLog(p.LogsDir(), "logs")
	mkdirLog(p.ConfigDir(), "config")
}

// ServerName returns the canonical name for a server id.
func (p *Paths) ServerName(id int) string {
	return fmt.Sprintf("Server%d", id)
}

// ServerDir returns the base directory for a server.
func (p *Paths) ServerDir(id int) string {
	return filepath.Join(p.RootPath, p.ServerName(id))
}

// ServerLogsDir returns the logs directory for a server.
func (p *Paths) ServerLogsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "logs")
}

// ServerSavesDir returns the saves directory for a server.
func (p *Paths) ServerSavesDir(id int) string {
	return filepath.Join(p.ServerDir(id), "saves")
}

// ServerSettingsDir returns the settings directory for a server.
func (p *Paths) ServerSettingsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "settings")
}

// ServerGameDir returns the game directory for a server deployment.
func (p *Paths) ServerGameDir(id int) string {
	return filepath.Join(p.ServerDir(id), "game")
}

// ServerModsDir returns the mods directory for a server.
func (p *Paths) ServerModsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "mods")
}

// ServerLogFile returns the admin log file path for a server.
func (p *Paths) ServerLogFile(id int) string {
	return filepath.Join(p.ServerLogsDir(id), fmt.Sprintf("%s_admin.log", p.ServerName(id)))
}

// ServerOutputFile returns the stdout/stderr capture file for a server.
func (p *Paths) ServerOutputFile(id int) string {
	return filepath.Join(p.ServerLogsDir(id), fmt.Sprintf("%s_output.log", p.ServerName(id)))
}

// ServerSettingsFile returns the settings file path for a server.
func (p *Paths) ServerSettingsFile(id int) string {
	return filepath.Join(p.ServerSettingsDir(id), "settings.xml")
}

// CheckServer verifies that a server's directory structure exists.
func (p *Paths) CheckServer(id int) bool {
	dirs := []string{
		p.ServerDir(id),
		p.ServerLogsDir(id),
		p.ServerSavesDir(id),
		p.ServerSettingsDir(id),
		p.ServerGameDir(id),
		p.ServerModsDir(id),
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// DeployServer creates a server's directory structure (idempotent).
func (p *Paths) DeployServer(id int, logger *Logger) {
	// Helper to create a directory and log a standardized message
	mkdirLog := func(path, label string) {
		_ = os.MkdirAll(path, os.ModePerm)
		if logger != nil {
			logger.Write(fmt.Sprintf("Creating %s path: %s", label, path))
		}
	}

	mkdirLog(p.ServerDir(id), "server")
	mkdirLog(p.ServerLogsDir(id), "logs")
	mkdirLog(p.ServerSavesDir(id), "saves")
	mkdirLog(p.ServerSettingsDir(id), "settings")
	mkdirLog(p.ServerGameDir(id), "game")
	mkdirLog(p.ServerModsDir(id), "mods")
}

// DeleteServerDirectory removes the entire server directory structure
// DeleteServerDirectory removes the entire server directory tree.
func (p *Paths) DeleteServerDirectory(id int, logger *Logger) error {
	serverDir := p.ServerDir(id)
	logger.Write(fmt.Sprintf("Deleting server directory: %s", serverDir))
	err := os.RemoveAll(serverDir)
	if err != nil {
		logger.Write(fmt.Sprintf("Error deleting server directory: %v", err))
		return err
	}
	logger.Write("Server directory successfully deleted")
	return nil
}

// RestartProcess restarts the application with the same arguments
// RestartProcess replaces or spawns the current process with the given
// executable and arguments depending on the platform. On Windows it spawns
// a new process; on Unix-like systems it uses syscall.Exec to replace.
func RestartProcess(executable string, args []string) error {
	// Get the environment
	env := os.Environ()

	if runtime.GOOS == "windows" {
		// On Windows, spawn a new process and allow the current one to exit normally.
		cmd := exec.Command(executable, args...)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		commandLine := strings.Join(append([]string{executable}, args...), " ")
		// Log restart execution to sdsm.log instead of stdout
		logger := NewLogger("")
		logger.Write("Executing command: " + commandLine)
		return cmd.Start()
	}

	// Use syscall.Exec to replace the current process on Unix-like systems.
	return syscall.Exec(executable, append([]string{executable}, args...), env)
}
