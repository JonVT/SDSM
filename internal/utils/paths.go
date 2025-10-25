package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Paths struct {
	RootPath string `json:"root_path"`
}

func NewPaths(rootPath string) *Paths {
	return &Paths{RootPath: rootPath}
}

func (p *Paths) SteamDir() string {
	return filepath.Join(p.RootPath, "bin", "steamcmd")
}

func (p *Paths) ReleaseDir() string {
	return filepath.Join(p.RootPath, "bin", "release")
}

func (p *Paths) BetaDir() string {
	return filepath.Join(p.RootPath, "bin", "beta")
}

func (p *Paths) BepInExDir() string {
	return filepath.Join(p.RootPath, "bin", "BepInEx")
}

func (p *Paths) LaunchPadDir() string {
	return filepath.Join(p.RootPath, "bin", "launchpad")
}

func (p *Paths) LogsDir() string {
	return filepath.Join(p.RootPath, "logs")
}

func (p *Paths) LogFile() string {
	return filepath.Join(p.LogsDir(), "sdsm.log")
}

func (p *Paths) UpdateLogFile() string {
	return filepath.Join(p.LogsDir(), "updates.log")
}

func (p *Paths) CheckRoot() bool {
	dirs := []string{p.RootPath, p.SteamDir(), p.ReleaseDir(), p.BetaDir(), p.BepInExDir(), p.LaunchPadDir(), p.LogsDir()}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func (p *Paths) DeployRoot(logger *Logger) {
	os.MkdirAll(p.RootPath, os.ModePerm)
	logger.Write(fmt.Sprintf("Creating root path: %s", p.RootPath))
	os.MkdirAll(p.SteamDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating steam path: %s", p.SteamDir()))
	os.MkdirAll(p.ReleaseDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating release path: %s", p.ReleaseDir()))
	os.MkdirAll(p.BetaDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating beta path: %s", p.BetaDir()))
	os.MkdirAll(p.BepInExDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating BepInEx path: %s", p.BepInExDir()))
	os.MkdirAll(p.LaunchPadDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating launchpad path: %s", p.LaunchPadDir()))
	os.MkdirAll(p.LogsDir(), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating logs path: %s", p.LogsDir()))
}

func (p *Paths) ServerName(id int) string {
	return fmt.Sprintf("Server%d", id)
}

func (p *Paths) ServerDir(id int) string {
	return filepath.Join(p.RootPath, p.ServerName(id))
}

func (p *Paths) ServerLogsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "logs")
}

func (p *Paths) ServerSavesDir(id int) string {
	return filepath.Join(p.ServerDir(id), "saves")
}

func (p *Paths) ServerSettingsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "settings")
}

func (p *Paths) ServerGameDir(id int) string {
	return filepath.Join(p.ServerDir(id), "game")
}

func (p *Paths) ServerModsDir(id int) string {
	return filepath.Join(p.ServerDir(id), "mods")
}

func (p *Paths) ServerLogFile(id int) string {
	return filepath.Join(p.ServerLogsDir(id), fmt.Sprintf("%s_admin.log", p.ServerName(id)))
}

func (p *Paths) ServerOutputFile(id int) string {
	return filepath.Join(p.ServerLogsDir(id), fmt.Sprintf("%s_output.log", p.ServerName(id)))
}

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

func (p *Paths) DeployServer(id int, logger *Logger) {
	os.MkdirAll(p.ServerDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating server path: %s", p.ServerDir(id)))
	os.MkdirAll(p.ServerLogsDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating logs path: %s", p.ServerLogsDir(id)))
	os.MkdirAll(p.ServerSavesDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating saves path: %s", p.ServerSavesDir(id)))
	os.MkdirAll(p.ServerSettingsDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating settings path: %s", p.ServerSettingsDir(id)))
	os.MkdirAll(p.ServerGameDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating game path: %s", p.ServerGameDir(id)))
	os.MkdirAll(p.ServerModsDir(id), os.ModePerm)
	logger.Write(fmt.Sprintf("Creating mods path: %s", p.ServerModsDir(id)))
}

// DeleteServerDirectory removes the entire server directory structure
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
func RestartProcess(executable string, args []string) error {
	// Get the environment
	env := os.Environ()

	// Use syscall.Exec to replace the current process (on Unix-like systems)
	return syscall.Exec(executable, append([]string{executable}, args...), env)
}
