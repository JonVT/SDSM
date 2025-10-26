package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	fs "io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"sdsm/internal/models"
	"sdsm/internal/utils"
	"sdsm/steam"
)

type DeployType string

const (
	DeployTypeRelease   DeployType = "RELEASE"
	DeployTypeBeta      DeployType = "BETA"
	DeployTypeAll       DeployType = "ALL"
	DeployTypeBepInEx   DeployType = "BEPINEX"
	DeployTypeSteamCMD  DeployType = "STEAMCMD"
	DeployTypeServers   DeployType = "SERVERS"
	DeployTypeLaunchPad DeployType = "LAUNCHPAD"
)

type Manager struct {
	Active            bool             `json:"active"`
	ConfigFile        string           `json:"-"`
	Updating          bool             `json:"updating"`
	SetupInProgress   bool             `json:"setup_in_progress"`
	Log               *utils.Logger    `json:"-"`
	UpdateLog         *utils.Logger    `json:"-"`
	SteamID           string           `json:"steam_id"`
	SavedPath         string           `json:"saved_path"`
	Paths             *utils.Paths     `json:"paths"`
	Port              int              `json:"port"`
	Language          string           `json:"language"`
	Servers           []*models.Server `json:"servers"`
	UpdateTime        time.Time        `json:"update_time"`
	StartupUpdate     bool             `json:"startup_update"`
	MissingComponents []string         `json:"-"`
	NeedsUploadPrompt bool             `json:"-"`
	DeployErrors      []string         `json:"-"`
	deployMu          sync.Mutex       `json:"-"`
	steamCmdMu        sync.RWMutex     `json:"-"`
	steamCmdVersion   string           `json:"-"`
	steamCmdCheckedAt time.Time        `json:"-"`
	bepInExMu         sync.RWMutex     `json:"-"`
	bepInExVersion    string           `json:"-"`
	bepInExCheckedAt  time.Time        `json:"-"`
	bepInExLatestMu   sync.RWMutex     `json:"-"`
	bepInExLatest     string           `json:"-"`
	bepInExLatestAt   time.Time        `json:"-"`
	launchPadLatestMu sync.RWMutex     `json:"-"`
	launchPadLatest   string           `json:"-"`
	launchPadLatestAt time.Time        `json:"-"`
	launchPadMu       sync.RWMutex     `json:"-"`
	launchPadVersion  string           `json:"-"`
	launchPadChecked  time.Time        `json:"-"`
	releaseVersionMu  sync.RWMutex     `json:"-"`
	releaseVersion    string           `json:"-"`
	releaseCheckedAt  time.Time        `json:"-"`
	betaVersionMu     sync.RWMutex     `json:"-"`
	betaVersion       string           `json:"-"`
	betaCheckedAt     time.Time        `json:"-"`
	worldIndexMu      sync.RWMutex     `json:"-"`
	worldIndex        map[bool]*worldDefinitionCache
	progressMu        sync.RWMutex
	progressByType    map[DeployType]*UpdateProgress
	serverProgressMu  sync.RWMutex
	serverProgress    map[int]*ServerCopyProgress
}

type UpdateProgress struct {
	Key         string    `json:"key"`
	Component   string    `json:"component"`
	DisplayName string    `json:"display_name"`
	Stage       string    `json:"stage"`
	Percent     int       `json:"percent"`
	Downloaded  int64     `json:"downloaded"`
	Total       int64     `json:"total"`
	Running     bool      `json:"running"`
	Error       string    `json:"error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UpdateProgressSnapshot struct {
	Updating   bool             `json:"updating"`
	Components []UpdateProgress `json:"components"`
}

type ServerCopyProgress struct {
	ServerID  int       `json:"server_id"`
	Stage     string    `json:"stage"`
	Percent   int       `json:"percent"`
	Processed int64     `json:"processed"`
	Total     int64     `json:"total"`
	Running   bool      `json:"running"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

var deployDisplayNames = map[DeployType]string{
	DeployTypeRelease:   "rocketstation_DedicatedServer Release",
	DeployTypeBeta:      "rocketstation_DedicatedServer Beta",
	DeployTypeSteamCMD:  "steamcmd",
	DeployTypeBepInEx:   "BepInEx",
	DeployTypeLaunchPad: "Stationeers LaunchPad",
}

var progressOrder = []DeployType{
	DeployTypeRelease,
	DeployTypeBeta,
	DeployTypeSteamCMD,
	DeployTypeBepInEx,
	DeployTypeLaunchPad,
}

func (dt DeployType) key() string {
	return strings.ToLower(string(dt))
}

func (dt DeployType) displayName() string {
	if name, ok := deployDisplayNames[dt]; ok {
		return name
	}
	return string(dt)
}

func NewManager() *Manager {
	m := &Manager{
		SteamID:        "600760",
		SavedPath:      "",
		Port:           5000,
		Language:       "english",
		Servers:        []*models.Server{},
		UpdateTime:     time.Time{},
		StartupUpdate:  true,
		progressByType: make(map[DeployType]*UpdateProgress),
		serverProgress: make(map[int]*ServerCopyProgress),
	}

	for _, deployType := range progressOrder {
		m.progressByType[deployType] = &UpdateProgress{
			Key:         deployType.key(),
			Component:   string(deployType),
			DisplayName: deployType.displayName(),
			Stage:       "Idle",
			Percent:     0,
			Downloaded:  0,
			Total:       0,
			Running:     false,
			UpdatedAt:   time.Now(),
		}
	}

	// Initialize paths with default values
	m.Paths = utils.NewPaths("/tmp/sdsm") // Default fallback path

	// Prepare logging early so load() can report issues
	m.startLogs()

	var config string
	if len(os.Args) > 1 && fileExists(os.Args[1]) {
		config = os.Args[1]
	} else if env := os.Getenv("SDSM_CONFIG"); env != "" && fileExists(env) {
		config = env
	} else if fileExists("sdsm.config") {
		config = "sdsm.config"
	} else {
		// No config file found, start with default paths and logs
		m.safeLog("No configuration file found. Please specify a configuration file on the command line or set the SDSM_CONFIG environment variable.")
		return m
	}

	m.ConfigFile = config
	wasActive, err := m.load()
	if err != nil {
		m.safeLog(err.Error())
		return m
	}

	// Start logs after paths are properly loaded
	m.startLogs()
	m.initializeServers()

	m.safeLog("Configuration loaded successfully")

	// Check for missing components before attempting deploy
	m.CheckMissingComponents()

	if m.StartupUpdate && !wasActive && !m.NeedsUploadPrompt {
		if err := m.Deploy(DeployTypeAll); err != nil {
			m.safeLog(fmt.Sprintf("Initial deployment failed: %v", err))
		}
	}

	return m
}

func (m *Manager) progressEntry(dt DeployType) *UpdateProgress {
	if m.progressByType == nil {
		m.progressByType = make(map[DeployType]*UpdateProgress)
	}
	entry, ok := m.progressByType[dt]
	if !ok {
		entry = &UpdateProgress{
			Key:         dt.key(),
			Component:   string(dt),
			DisplayName: dt.displayName(),
			Stage:       "Idle",
			UpdatedAt:   time.Now(),
		}
		m.progressByType[dt] = entry
	}
	return entry
}

func normalizeStage(stage string) string {
	trimmed := strings.TrimSpace(stage)
	if trimmed == "" {
		return "Processing"
	}
	return trimmed
}

func (m *Manager) progressBegin(dt DeployType, stage string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	entry := m.progressEntry(dt)
	entry.Stage = normalizeStage(stage)
	entry.Percent = 0
	entry.Downloaded = 0
	entry.Total = 0
	entry.Running = true
	entry.Error = ""
	entry.UpdatedAt = time.Now()
}

func (m *Manager) progressUpdate(dt DeployType, stage string, downloaded, total int64) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	entry := m.progressEntry(dt)
	entry.Stage = normalizeStage(stage)
	entry.Downloaded = downloaded
	if total >= 0 {
		entry.Total = total
	}
	if entry.Total > 0 {
		percent := int((entry.Downloaded * 100) / entry.Total)
		if percent > 100 {
			percent = 100
		}
		if percent < 0 {
			percent = 0
		}
		entry.Percent = percent
	}
	entry.Running = true
	entry.Error = ""
	entry.UpdatedAt = time.Now()
}

func (m *Manager) progressComplete(dt DeployType, stage string, err error) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	entry := m.progressEntry(dt)
	if stage != "" {
		entry.Stage = normalizeStage(stage)
	}
	if err != nil {
		entry.Error = err.Error()
	} else {
		entry.Error = ""
		if entry.Total == 0 {
			entry.Percent = 100
		} else if entry.Percent < 100 {
			entry.Percent = 100
		}
	}
	entry.Running = false
	entry.UpdatedAt = time.Now()
}

func (m *Manager) progressReporter(dt DeployType) func(component, stage string, downloaded, total int64) {
	return func(_ string, stage string, downloaded, total int64) {
		if total < 0 {
			total = 0
		}
		m.progressUpdate(dt, stage, downloaded, total)
	}
}

func (m *Manager) ProgressSnapshot() UpdateProgressSnapshot {
	m.progressMu.RLock()
	defer m.progressMu.RUnlock()
	snapshot := UpdateProgressSnapshot{
		Updating: m.IsUpdating(),
	}
	for _, dt := range progressOrder {
		if entry, ok := m.progressByType[dt]; ok && entry != nil {
			copy := *entry
			snapshot.Components = append(snapshot.Components, copy)
		}
	}
	return snapshot
}

func (m *Manager) serverProgressEntryLocked(serverID int) *ServerCopyProgress {
	if m.serverProgress == nil {
		m.serverProgress = make(map[int]*ServerCopyProgress)
	}
	entry, ok := m.serverProgress[serverID]
	if !ok {
		entry = &ServerCopyProgress{
			ServerID:  serverID,
			Stage:     "Idle",
			Percent:   0,
			Processed: 0,
			Total:     0,
			Running:   false,
			UpdatedAt: time.Now(),
		}
		m.serverProgress[serverID] = entry
	}
	return entry
}

func (m *Manager) ServerProgressBegin(serverID int, stage string) {
	m.serverProgressMu.Lock()
	defer m.serverProgressMu.Unlock()
	entry := m.serverProgressEntryLocked(serverID)
	if strings.TrimSpace(stage) != "" {
		entry.Stage = stage
	} else {
		entry.Stage = "Queued"
	}
	entry.Percent = 0
	entry.Processed = 0
	entry.Total = 0
	entry.Running = true
	entry.Error = ""
	entry.UpdatedAt = time.Now()
}

func (m *Manager) ServerProgressUpdate(serverID int, stage string, processed, total int64) {
	m.serverProgressMu.Lock()
	defer m.serverProgressMu.Unlock()
	entry := m.serverProgressEntryLocked(serverID)
	if strings.TrimSpace(stage) != "" {
		entry.Stage = stage
	}
	if total > 0 {
		entry.Total = total
	}
	if processed >= 0 {
		entry.Processed = processed
	}
	entry.Running = true
	entry.Error = ""
	entry.Percent = calculatePercent(entry.Processed, entry.Total)
	entry.UpdatedAt = time.Now()
}

func (m *Manager) ServerProgressComplete(serverID int, stage string, err error) {
	m.serverProgressMu.Lock()
	defer m.serverProgressMu.Unlock()
	entry := m.serverProgressEntryLocked(serverID)
	if strings.TrimSpace(stage) != "" {
		entry.Stage = stage
	} else if entry.Stage == "" {
		entry.Stage = "Completed"
	}
	if entry.Total > 0 && entry.Processed < entry.Total {
		entry.Processed = entry.Total
	}
	if entry.Total == 0 && entry.Processed == 0 {
		entry.Processed = 0
	}
	entry.Percent = calculatePercent(entry.Processed, entry.Total)
	if entry.Percent < 100 {
		entry.Percent = 100
	}
	entry.Running = false
	if err != nil {
		entry.Error = err.Error()
		if entry.Stage == "" || strings.EqualFold(entry.Stage, "Completed") {
			entry.Stage = "Failed"
		}
	} else {
		entry.Error = ""
		if entry.Stage == "" {
			entry.Stage = "Completed"
		}
	}
	entry.UpdatedAt = time.Now()
}

func (m *Manager) ServerProgressSnapshot(serverID int) ServerCopyProgress {
	m.serverProgressMu.RLock()
	defer m.serverProgressMu.RUnlock()
	entry, ok := m.serverProgress[serverID]
	if !ok || entry == nil {
		return ServerCopyProgress{
			ServerID:  serverID,
			Stage:     "Idle",
			Percent:   0,
			Processed: 0,
			Total:     0,
			Running:   false,
			Error:     "",
			UpdatedAt: time.Now(),
		}
	}
	copy := *entry
	return copy
}

func (m *Manager) IsServerUpdateRunning(serverID int) bool {
	m.serverProgressMu.RLock()
	defer m.serverProgressMu.RUnlock()
	entry, ok := m.serverProgress[serverID]
	return ok && entry != nil && entry.Running
}

func calculatePercent(processed, total int64) int {
	if total <= 0 {
		if processed > 0 {
			return 100
		}
		return 0
	}
	percent := int((processed * 100) / total)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent
}

func (m *Manager) IsActive() bool {
	return m.Active
}

func (m *Manager) safeLog(message string) {
	if m.Log != nil {
		m.Log.Write(message)
		return
	}
	fmt.Println(message)
}

func (m *Manager) startLogs() {
	if m.Paths == nil {
		// Initialize with default paths if not set
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}
	if err := os.MkdirAll(m.Paths.LogsDir(), 0o755); err != nil {
		fmt.Printf("Unable to create logs directory %s: %v\n", m.Paths.LogsDir(), err)
	}
	if m.Log != nil {
		m.Log.Close()
	}
	if m.UpdateLog != nil {
		m.UpdateLog.Close()
	}
	m.Log = utils.NewLogger(m.Paths.LogFile())
	m.UpdateLog = utils.NewLogger(m.Paths.UpdateLogFile())
}

func (m *Manager) LastUpdateLogLine() string {
	if m.Paths == nil {
		return ""
	}

	logPath := m.Paths.UpdateLogFile()
	file, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lastLine string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lastLine = line
		}
	}

	if err := scanner.Err(); err != nil {
		return ""
	}

	return lastLine
}

func (m *Manager) initializeServers() {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}
	for _, srv := range m.Servers {
		if srv == nil {
			continue
		}
		srv.Paths = m.Paths
		if err := os.MkdirAll(m.Paths.ServerLogsDir(srv.ID), 0o755); err != nil {
			m.safeLog(fmt.Sprintf("Failed to ensure logs directory for server %d: %v", srv.ID, err))
		}
		srv.EnsureLogger(m.Paths)
	}
}

// load reads configuration from disk and rebuilds in-memory state.
func (m *Manager) load() (bool, error) {
	data, err := os.ReadFile(m.ConfigFile)
	if err != nil {
		return false, fmt.Errorf("configuration file not found: %w", err)
	}

	// Create a temporary struct to unmarshal into, preserving existing Paths
	temp := &Manager{}
	err = json.Unmarshal(data, temp)
	if err != nil {
		return false, fmt.Errorf("error parsing configuration: %w", err)
	}

	// Copy fields from loaded config
	m.SteamID = temp.SteamID
	m.SavedPath = temp.SavedPath
	m.Port = temp.Port
	m.Servers = temp.Servers
	m.UpdateTime = temp.UpdateTime
	m.StartupUpdate = temp.StartupUpdate

	// Update fields from temp, but preserve existing Paths if temp.Paths is nil
	if temp.Paths != nil && temp.Paths.RootPath != "" {
		m.Paths = temp.Paths
	}

	wasActive := temp.Active
	m.Updating = false
	m.Active = true

	return wasActive, nil
}

func (m *Manager) Save() {
	if m.ConfigFile == "" {
		m.Log.Write("No configuration file found. Please specify a configuration file on the command line or set the SDSM_CONFIG environment variable.")
		return
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		m.Log.Write(fmt.Sprintf("Error marshaling configuration: %v", err))
		return
	}

	err = os.WriteFile(m.ConfigFile, data, 0644)
	if err != nil {
		m.Log.Write(fmt.Sprintf("Error saving configuration: %v", err))
		m.Active = false
		return
	}

	m.Log.Write("Configuration saved successfully")
	m.Active = true
}

func (m *Manager) UpdateConfig(steamID, rootPath string, port int, updateTime time.Time, startupUpdate bool) {
	redeploy := false
	restart := false

	if m.ConfigFile == "" {
		m.Log.Write(fmt.Sprintf("Configuration file not found. Saving to %s", m.Paths.RootPath))
		m.ConfigFile = filepath.Join(m.Paths.RootPath, "sdsm.config")
	}

	m.UpdateTime = updateTime
	m.StartupUpdate = startupUpdate

	if m.Paths.RootPath != rootPath {
		m.Log.Write(fmt.Sprintf("Root path changed from %s to %s. Redeploying...", m.Paths.RootPath, rootPath))
		m.Paths.RootPath = rootPath
		if !m.Paths.CheckRoot() {
			m.Paths.DeployRoot(m.Log)
		}
		m.startLogs()
		m.initializeServers()
		redeploy = true
	}

	if m.SteamID != steamID {
		m.Log.Write(fmt.Sprintf("Steam ID changed from %s to %s. Redeploying...", m.SteamID, steamID))
		m.SteamID = steamID
		redeploy = true
	}

	if m.Port != port {
		m.Log.Write(fmt.Sprintf("Port changed from %d to %d. Restarting servers...", m.Port, port))
		m.Port = port
		restart = true
	}

	m.Log.Write(fmt.Sprintf("Configuration updated: Steam ID: %s, Root Path: %s, Port: %d, Update Time: %v, Startup Update: %v", m.SteamID, m.Paths.RootPath, m.Port, m.UpdateTime, m.StartupUpdate))
	m.Save()

	if redeploy {
		if err := m.Deploy(DeployTypeAll); err != nil {
			m.Log.Write(fmt.Sprintf("Redeploy failed: %v", err))
		}
	}
	if restart {
		m.Shutdown()
	}
}

func (m *Manager) beginDeploy() error {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	if m.Updating {
		return fmt.Errorf("deployment already in progress")
	}
	m.Updating = true
	return nil
}

func (m *Manager) finishDeploy() {
	m.deployMu.Lock()
	m.Updating = false
	m.deployMu.Unlock()
}

func (m *Manager) StartDeployAsync(deployType DeployType) error {
	if err := m.beginDeploy(); err != nil {
		return err
	}

	go func() {
		defer m.finishDeploy()
		if err := m.runDeploy(deployType); err != nil {
			// Errors are already logged inside runDeploy; nothing additional to do here.
		}
	}()

	return nil
}

func (m *Manager) Deploy(deployType DeployType) error {
	if err := m.beginDeploy(); err != nil {
		m.Log.Write(err.Error())
		return err
	}
	defer m.finishDeploy()
	return m.runDeploy(deployType)
}

func (m *Manager) runDeploy(deployType DeployType) error {
	startTime := time.Now()
	m.Log.Write(fmt.Sprintf("Deployment (%s) started", deployType))
	if m.UpdateLog != nil {
		m.UpdateLog.Write(fmt.Sprintf("Deployment (%s) started", deployType))
	}

	m.Paths.DeployRoot(m.Log)

	s := steam.NewSteam(m.SteamID, m.UpdateLog, m.Paths)

	var errs []string

	if deployType == DeployTypeSteamCMD || deployType == DeployTypeAll {
		m.Log.Write("Beginning SteamCMD deployment")
		m.progressBegin(DeployTypeSteamCMD, "Queued")
		s.SetProgressReporter(string(DeployTypeSteamCMD), m.progressReporter(DeployTypeSteamCMD))
		if err := s.UpdateSteamCMD(); err != nil {
			msg := fmt.Sprintf("SteamCMD deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeSteamCMD, "Failed", err)
		} else {
			m.Log.Write("SteamCMD deployment completed successfully")
			m.progressComplete(DeployTypeSteamCMD, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateSteamCmdVersionCache()
	}

	if deployType == DeployTypeRelease || deployType == DeployTypeAll {
		m.Log.Write("Beginning Release server deployment")
		m.progressBegin(DeployTypeRelease, "Queued")
		s.SetProgressReporter(string(DeployTypeRelease), m.progressReporter(DeployTypeRelease))
		if err := s.UpdateGame(false); err != nil {
			msg := fmt.Sprintf("Release deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeRelease, "Failed", err)
		} else {
			m.Log.Write("Release server deployment completed successfully")
			m.progressComplete(DeployTypeRelease, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateRocketStationVersionCache(false)
	}

	if deployType == DeployTypeBeta || deployType == DeployTypeAll {
		m.Log.Write("Beginning Beta server deployment")
		m.progressBegin(DeployTypeBeta, "Queued")
		s.SetProgressReporter(string(DeployTypeBeta), m.progressReporter(DeployTypeBeta))
		if err := s.UpdateGame(true); err != nil {
			msg := fmt.Sprintf("Beta deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeBeta, "Failed", err)
		} else {
			m.Log.Write("Beta server deployment completed successfully")
			m.progressComplete(DeployTypeBeta, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateRocketStationVersionCache(true)
	}

	if deployType == DeployTypeBepInEx || deployType == DeployTypeAll {
		m.Log.Write("Beginning BepInEx deployment")
		m.progressBegin(DeployTypeBepInEx, "Queued")
		s.SetProgressReporter(string(DeployTypeBepInEx), m.progressReporter(DeployTypeBepInEx))
		if err := s.UpdateBepInEx(); err != nil {
			msg := fmt.Sprintf("BepInEx deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeBepInEx, "Failed", err)
		} else {
			m.Log.Write("BepInEx deployment completed successfully")
			m.progressComplete(DeployTypeBepInEx, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateBepInExVersionCache()
	}

	if deployType == DeployTypeLaunchPad || deployType == DeployTypeAll {
		m.Log.Write("Beginning LaunchPad deployment")
		m.progressBegin(DeployTypeLaunchPad, "Queued")
		s.SetProgressReporter(string(DeployTypeLaunchPad), m.progressReporter(DeployTypeLaunchPad))
		if err := s.UpdateLaunchPad(); err != nil {
			msg := fmt.Sprintf("Stationeers LaunchPad deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeLaunchPad, "Failed", err)
		} else {
			m.Log.Write("LaunchPad deployment completed successfully")
			m.progressComplete(DeployTypeLaunchPad, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateLaunchPadVersionCache()
	}

	if deployType == DeployTypeServers || deployType == DeployTypeAll {
		serversFailed := false
		for _, srv := range m.Servers {
			m.Log.Write(fmt.Sprintf("Deploying server: %s", srv.Name))
			if err := srv.Deploy(); err != nil {
				serversFailed = true
				msg := fmt.Sprintf("Server %s deploy failed: %v", srv.Name, err)
				errs = append(errs, msg)
				m.Log.Write(msg)
				if m.UpdateLog != nil {
					m.UpdateLog.Write(msg)
				}
			}
		}
		if !serversFailed {
			m.Log.Write("All servers deployed successfully.")
		}
	}

	m.CheckMissingComponents()

	m.deployMu.Lock()
	if len(errs) > 0 {
		m.DeployErrors = append([]string(nil), errs...)
	} else {
		m.DeployErrors = nil
	}
	m.deployMu.Unlock()

	duration := time.Since(startTime)
	if len(errs) > 0 {
		combined := errors.New(strings.Join(errs, "; "))
		m.Log.Write(fmt.Sprintf("Deployment (%s) completed with errors in %s", deployType, duration))
		if m.UpdateLog != nil {
			m.UpdateLog.Write(fmt.Sprintf("Deployment (%s) completed with errors in %s", deployType, duration))
		}
		return combined
	}

	m.Log.Write(fmt.Sprintf("Deployment (%s) completed successfully in %s", deployType, duration))
	if m.UpdateLog != nil {
		m.UpdateLog.Write(fmt.Sprintf("Deployment (%s) completed successfully in %s", deployType, duration))
	}
	m.Active = true
	return nil
}

func (m *Manager) GetDeployErrors() []string {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	if len(m.DeployErrors) == 0 {
		return nil
	}
	errs := make([]string, len(m.DeployErrors))
	copy(errs, m.DeployErrors)
	return errs
}

func (m *Manager) IsUpdating() bool {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	return m.Updating
}

func (m *Manager) Shutdown() {
	// Allow HTTP response to complete before shutting down
	time.Sleep(1000 * time.Millisecond)

	m.Log.Write("Shutting down all servers.")
	for _, srv := range m.Servers {
		m.Log.Write(fmt.Sprintf("Stopping server: %s", srv.Name))
		srv.Stop()
	}
	m.Log.Write("All servers stopped successfully.")
	m.Save()
	m.Active = false
	m.Log.Write("SDSM is shutting down.")

	// Exit the application
	time.Sleep(1000 * time.Millisecond) // Give logs time to flush
	os.Exit(0)
}

func (m *Manager) Restart() {
	// Allow HTTP response to complete before restarting
	time.Sleep(1000 * time.Millisecond)

	m.Log.Write("Restarting SDSM...")
	m.Log.Write("Shutting down all servers.")
	for _, srv := range m.Servers {
		m.Log.Write(fmt.Sprintf("Stopping server: %s", srv.Name))
		srv.Stop()
	}
	m.Log.Write("All servers stopped successfully.")
	m.Save()
	m.Log.Write("SDSM will now restart.")

	truncateLogFile := func(path string) {
		if path == "" {
			return
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if err != nil {
			fmt.Printf("Unable to truncate log file %s: %v\n", path, err)
			return
		}
		file.Close()
	}

	truncateLogFile(m.Paths.LogFile())
	truncateLogFile(m.Paths.UpdateLogFile())
	// Restart the application
	time.Sleep(1000 * time.Millisecond) // Give logs time to flush

	// Get the executable path and arguments
	executable, err := os.Executable()
	if err != nil {
		m.Log.Write(fmt.Sprintf("Failed to get executable path: %v", err))
		os.Exit(1)
	}

	// Start new process
	args := os.Args[1:] // Get original arguments
	if err := utils.RestartProcess(executable, args); err != nil {
		m.Log.Write(fmt.Sprintf("Failed to restart: %v", err))
		os.Exit(1)
	}

	// Exit current process
	os.Exit(0)
}

func (m *Manager) GetWorlds() []string {
	return m.GetWorldsByVersion(false)
}

func (m *Manager) GetWorldsByVersion(beta bool) []string {
	if worlds := m.getWorldsFromXML(beta); len(worlds) > 0 {
		return worlds
	}
	return []string{"Error: No worlds found"}
}

// ResolveWorldID returns the technical world identifier (directory name) for a given world display name.
func (m *Manager) ResolveWorldID(world string, beta bool) string {
	if strings.TrimSpace(world) == "" {
		return ""
	}
	return m.resolveWorldTechnicalID(world, beta)
}

// getGameDataPath returns the path to the game data directory
// It checks ReleaseDir, BetaDir, and common Steam installation paths
func (m *Manager) getGameDataPath() string {
	return m.getGameDataPathForVersion(false)
}

func (m *Manager) getGameDataPathForVersion(beta bool) string {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	releasePath := filepath.Join(m.Paths.ReleaseDir(), "rocketstation_DedicatedServer_Data")
	betaPath := filepath.Join(m.Paths.BetaDir(), "rocketstation_DedicatedServer_Data")
	rootPath := filepath.Join(m.Paths.RootPath, "rocketstation_DedicatedServer_Data")
	steamPath := filepath.Join(os.Getenv("HOME"), ".steam", "steam", "steamapps", "common", "Stationeers Dedicated Server", "rocketstation_DedicatedServer_Data")

	var candidates []string
	if beta {
		candidates = append(candidates, betaPath, releasePath)
	} else {
		candidates = append(candidates, releasePath, betaPath)
	}
	candidates = append(candidates, rootPath, steamPath)

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	for _, path := range candidates {
		if path != "" {
			return path
		}
	}

	return releasePath
}

func (m *Manager) GetDifficulties() []string {
	if difficulties := m.getDifficultiesFromXML(false); len(difficulties) > 0 {
		return difficulties
	}
	return []string{"Creative", "Easy", "Normal", "Stationeer"}
}

func (m *Manager) GetStartConditions() []string {
	if conditions := m.getStartConditionsFromXML(false); len(conditions) > 0 {
		return conditions
	}
	return []string{}
}

func (m *Manager) GetLanguages() []string {
	// Pull language options directly from the Stationeers language directory.
	return m.getLanguagesFromFolder()
}

// XML parsing structures
type GameData struct {
	WorldSettings   WorldSettings    `xml:"WorldSettings"`
	StartConditions []StartCondition `xml:"StartCondition"`
}

type WorldSettings struct {
	World World `xml:"World"`
}

type World struct {
	ID       string `xml:"Id,attr"`
	Priority string `xml:"Priority,attr"`
	Hidden   string `xml:"Hidden,attr"`
}

type StartCondition struct {
	ID   string `xml:"Id,attr"`
	Name string `xml:"Name"`
}

type boolFlagElement struct {
	Value string `xml:"Value,attr"`
	Text  string `xml:",chardata"`
}

func attrValueIsTrue(attrs []xml.Attr) bool {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, "Value") && strings.EqualFold(attr.Value, "true") {
			return true
		}
	}
	return false
}

func (m *Manager) getWorldsFromXML(beta bool) []string {
	cache := m.worldDefinitionsCache(beta)
	if cache == nil || len(cache.definitions) == 0 {
		return nil
	}
	worlds := make([]string, 0, len(cache.definitions))
	for _, def := range cache.definitions {
		worlds = append(worlds, def.DisplayName)
	}
	return worlds
}

func extractWorldLocalizationKeys(worldXML []byte) (string, string, string, string) {
	decoder := xml.NewDecoder(bytes.NewReader(worldXML))
	var current string
	var nameKey, nameText, descKey, descText string

	for {
		tok, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nameKey, nameText, descKey, descText
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "Name":
				current = "Name"
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Key" && nameKey == "" {
						nameKey = attr.Value
						break
					}
				}
			case "Description":
				current = "Description"
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Key" && descKey == "" {
						descKey = attr.Value
						break
					}
				}
			default:
				current = ""
			}
		case xml.CharData:
			text := strings.TrimSpace(string(elem))
			if text == "" {
				continue
			}
			if current == "Name" && nameText == "" {
				nameText = text
			}
			if current == "Description" && descText == "" {
				descText = text
			}
		case xml.EndElement:
			if elem.Name.Local == "Name" || elem.Name.Local == "Description" {
				current = ""
			}
		}
	}

	return nameKey, nameText, descKey, descText
}

func (m *Manager) lookupLanguageValue(beta bool, key string) string {
	if key == "" {
		return ""
	}

	language := m.Language
	if language == "" {
		language = "english"
	}

	langPath := filepath.Join(m.getGameDataPathForVersion(beta), "StreamingAssets", "Language", language+".xml")
	data, err := os.ReadFile(langPath)
	if err != nil {
		return ""
	}

	content := string(data)
	searchKey := "<Key>" + key + "</Key>"
	idx := strings.Index(content, searchKey)
	if idx == -1 {
		return ""
	}

	section := content[idx+len(searchKey):]
	valueStart := strings.Index(section, "<Value>")
	if valueStart == -1 {
		return ""
	}
	valueStart += len("<Value>")
	valueEnd := strings.Index(section[valueStart:], "</Value>")
	if valueEnd == -1 {
		return ""
	}

	return section[valueStart : valueStart+valueEnd]
}

func (m *Manager) getStartConditionsFromXML(beta bool) []string {
	xmlPath := filepath.Join(m.getGameDataPathForVersion(beta), "StreamingAssets", "Data", "startconditions.xml")

	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return nil
	}

	var gameData GameData
	if err := xml.Unmarshal(data, &gameData); err != nil {
		return nil
	}

	var conditions []string
	for _, sc := range gameData.StartConditions {
		if sc.ID != "" {
			conditions = append(conditions, sc.ID)
		}
	}

	return conditions
}

func (m *Manager) getLanguagesFromFolder() []string {
	langPath := filepath.Join(m.getGameDataPath(), "StreamingAssets", "Language")

	entries, err := os.ReadDir(langPath)
	if err != nil {
		return nil
	}

	languagesMap := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip non-language files
		if !strings.HasSuffix(name, ".xml") || strings.Contains(name, "_") {
			continue
		}

		// Remove .xml extension
		langName := strings.TrimSuffix(name, ".xml")

		// Skip override files
		if strings.Contains(langName, "Override") {
			continue
		}

		languagesMap[langName] = true
	}

	var result []string
	for lang := range languagesMap {
		result = append(result, lang)
	}

	return result
}

// WorldInfo contains localized world name and description
type WorldInfo struct {
	ID          string
	Name        string
	Description string
}

// LocationInfo contains localized start location information
type LocationInfo struct {
	ID          string
	Name        string
	Description string
}

// ConditionInfo contains localized start condition information
type ConditionInfo struct {
	ID          string
	Name        string
	Description string
}

var steamCmdVersionPattern = regexp.MustCompile(`Steam Console Client \(c\) Valve Corporation - version (\d+)`)
var bepinexVersionPattern = regexp.MustCompile(`\b\d+\.\d+\.\d+\.\d+\b`)
var launchPadVersionPattern = regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)
var appManifestBuildIDPattern = regexp.MustCompile(`"buildid"\s*"(\d+)"`)

var errLaunchPadVersionNotFound = errors.New("launchpad version not found")
var errLaunchPadDLLFound = errors.New("launchpad dll located")
var errRocketStationVersionNotFound = errors.New("rocketstation version not found")

const steamCmdVersionCacheTTL = time.Minute
const bepinexVersionCacheTTL = time.Minute
const bepinexLatestCacheTTL = 30 * time.Minute
const worldIndexCacheTTL = time.Minute
const bepinexLatestURL = "https://api.github.com/repos/BepInEx/BepInEx/releases/latest"
const launchPadLatestCacheTTL = 30 * time.Minute
const launchPadLatestURL = "https://api.github.com/repos/StationeersLaunchPad/StationeersLaunchPad/releases/latest"
const launchPadVersionCacheTTL = time.Minute
const rocketStationVersionCacheTTL = time.Minute
const bepInExVersionFile = "bepinex.version"

type worldDefinition struct {
	Directory           string
	ID                  string
	DisplayName         string
	Priority            int
	Root                string
	NameKey             string
	NameFallback        string
	DescriptionKey      string
	DescriptionFallback string
}

func (wd worldDefinition) effectivePriority() int {
	if wd.Priority > 0 {
		return wd.Priority
	}
	return 1 << 30
}

func (wd worldDefinition) preferredOver(other worldDefinition) bool {
	if wd.effectivePriority() != other.effectivePriority() {
		return wd.effectivePriority() < other.effectivePriority()
	}
	return strings.ToLower(wd.DisplayName) < strings.ToLower(other.DisplayName)
}

type worldDefinitionCache struct {
	definitions []worldDefinition
	byCanonical map[string]worldDefinition
	generatedAt time.Time
}

type worldMetadata struct {
	ID                  string
	Priority            int
	Hidden              bool
	AllowDedicated      bool
	AllowDedicatedKnown bool
	ShouldSkip          bool
}

func canonicalWorldIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func isTrueString(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func parseBooleanElement(decoder *xml.Decoder, start xml.StartElement) bool {
	if attrValueIsTrue(start.Attr) {
		return true
	}
	var flag boolFlagElement
	if err := decoder.DecodeElement(&flag, &start); err != nil {
		return false
	}
	if isTrueString(flag.Value) {
		return true
	}
	return isTrueString(flag.Text)
}

func extractWorldMetadata(data []byte) (worldMetadata, error) {
	meta := worldMetadata{
		Priority:       1 << 30,
		AllowDedicated: true,
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return meta, err
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "World":
				for _, attr := range elem.Attr {
					switch attr.Name.Local {
					case "Id":
						if meta.ID == "" {
							meta.ID = strings.TrimSpace(attr.Value)
						}
					case "Priority":
						if p, err := strconv.Atoi(strings.TrimSpace(attr.Value)); err == nil {
							meta.Priority = p
						}
					case "Hidden":
						if isTrueString(attr.Value) {
							meta.Hidden = true
						}
					case "Deprecated":
						if isTrueString(attr.Value) {
							meta.ShouldSkip = true
						}
					case "AllowDedicated":
						meta.AllowDedicatedKnown = true
						meta.AllowDedicated = isTrueString(attr.Value)
						if !meta.AllowDedicated {
							meta.ShouldSkip = true
						}
					}
				}
			case "AllowDedicated":
				meta.AllowDedicatedKnown = true
				if !parseBooleanElement(decoder, elem) {
					meta.AllowDedicated = false
					meta.ShouldSkip = true
				}
			case "IsTutorial", "IsDepreciated":
				if parseBooleanElement(decoder, elem) {
					meta.ShouldSkip = true
				}
			}
		}
	}

	if meta.Hidden {
		meta.ShouldSkip = true
	}

	return meta, nil
}

func (m *Manager) worldDefinitionsCache(beta bool) *worldDefinitionCache {
	m.worldIndexMu.RLock()
	cache := m.worldIndex[beta]
	if cache != nil && time.Since(cache.generatedAt) < worldIndexCacheTTL {
		defer m.worldIndexMu.RUnlock()
		return cache
	}
	m.worldIndexMu.RUnlock()

	cache = m.buildWorldDefinitionCache(beta)
	m.worldIndexMu.Lock()
	if m.worldIndex == nil {
		m.worldIndex = make(map[bool]*worldDefinitionCache)
	}
	m.worldIndex[beta] = cache
	m.worldIndexMu.Unlock()
	return cache
}

func (m *Manager) buildWorldDefinitionCache(beta bool) *worldDefinitionCache {
	cache := &worldDefinitionCache{
		byCanonical: make(map[string]worldDefinition),
	}

	paths := []string{m.getGameDataPathForVersion(beta)}
	if beta {
		paths = append(paths, m.getGameDataPathForVersion(false))
	}

	uniquePaths := uniqueStrings(paths)
	byDisplay := make(map[string]worldDefinition)

	for _, root := range uniquePaths {
		if root == "" {
			continue
		}
		worldsPath := filepath.Join(root, "StreamingAssets", "Worlds")
		entries, err := os.ReadDir(worldsPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirName := entry.Name()
			xmlFiles, err := filepath.Glob(filepath.Join(worldsPath, dirName, "*.xml"))
			if err != nil || len(xmlFiles) == 0 {
				continue
			}

			for _, xmlPath := range xmlFiles {
				data, err := os.ReadFile(xmlPath)
				if err != nil {
					continue
				}

				meta, err := extractWorldMetadata(data)
				if err != nil {
					continue
				}
				if meta.ShouldSkip {
					continue
				}

				def := worldDefinition{
					Directory: dirName,
					ID:        meta.ID,
					Priority:  meta.Priority,
					Root:      root,
				}
				if def.ID == "" {
					def.ID = dirName
				}

				nameKey, nameFallback, descKey, descFallback := extractWorldLocalizationKeys(data)
				def.NameKey = nameKey
				def.NameFallback = nameFallback
				def.DescriptionKey = descKey
				def.DescriptionFallback = descFallback

				displayName := def.ID
				if def.NameKey != "" {
					if value := strings.TrimSpace(m.lookupLanguageValue(beta, def.NameKey)); value != "" {
						displayName = value
					} else if def.NameFallback != "" {
						displayName = def.NameFallback
					}
				} else if def.NameFallback != "" {
					displayName = def.NameFallback
				}
				def.DisplayName = strings.TrimSpace(displayName)
				if def.DisplayName == "" {
					def.DisplayName = def.ID
				}

				canonicalKeys := []string{def.DisplayName, def.ID, def.Directory}
				for _, key := range canonicalKeys {
					canonical := canonicalWorldIdentifier(key)
					if canonical == "" {
						continue
					}
					if existing, ok := cache.byCanonical[canonical]; !ok || def.preferredOver(existing) {
						cache.byCanonical[canonical] = def
					}
				}

				displayKey := canonicalWorldIdentifier(def.DisplayName)
				if existing, ok := byDisplay[displayKey]; !ok || def.preferredOver(existing) {
					byDisplay[displayKey] = def
				}

				// Prefer the first successful XML for this directory
				break
			}
		}
	}

	for _, def := range byDisplay {
		cache.definitions = append(cache.definitions, def)
	}

	sort.Slice(cache.definitions, func(i, j int) bool {
		da := cache.definitions[i]
		db := cache.definitions[j]
		if da.effectivePriority() != db.effectivePriority() {
			return da.effectivePriority() < db.effectivePriority()
		}
		return strings.ToLower(da.DisplayName) < strings.ToLower(db.DisplayName)
	})

	cache.generatedAt = time.Now()
	return cache
}

func (m *Manager) worldLocalizationFromDirectory(root, dir string) (string, string, string, string, error) {
	if root == "" || dir == "" {
		return "", "", "", "", os.ErrNotExist
	}
	base := filepath.Join(root, "StreamingAssets", "Worlds", dir)
	primary := filepath.Join(base, dir+".xml")
	data, err := os.ReadFile(primary)
	if err != nil {
		xmlFiles, globErr := filepath.Glob(filepath.Join(base, "*.xml"))
		if globErr != nil || len(xmlFiles) == 0 {
			return "", "", "", "", err
		}
		readErr := err
		for _, candidate := range xmlFiles {
			if candidate == primary {
				continue
			}
			data, readErr = os.ReadFile(candidate)
			if readErr == nil {
				break
			}
		}
		if readErr != nil {
			return "", "", "", "", readErr
		}
	}

	nameKey, nameFallback, descKey, descFallback := extractWorldLocalizationKeys(data)
	return nameKey, nameFallback, descKey, descFallback, nil
}

func extractLocalizationForElement(data []byte, elementName, targetID string) (string, string, string, string) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var capturing bool
	current := ""
	var nameKey, nameFallback, descKey, descFallback string

	for {
		tok, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", "", "", ""
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			if elem.Name.Local == elementName {
				match := false
				for _, attr := range elem.Attr {
					if strings.EqualFold(attr.Name.Local, "Id") && strings.EqualFold(strings.TrimSpace(attr.Value), targetID) {
						match = true
						break
					}
				}
				capturing = match
				current = ""
				continue
			}
			if !capturing {
				continue
			}
			switch elem.Name.Local {
			case "Name":
				current = "Name"
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Key" && nameKey == "" {
						nameKey = attr.Value
					}
				}
			case "Description":
				current = "Description"
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Key" && descKey == "" {
						descKey = attr.Value
					}
				}
			default:
				current = ""
			}
		case xml.CharData:
			if !capturing || current == "" {
				continue
			}
			text := strings.TrimSpace(string(elem))
			if text == "" {
				continue
			}
			if current == "Name" && nameFallback == "" {
				nameFallback = text
			}
			if current == "Description" && descFallback == "" {
				descFallback = text
			}
		case xml.EndElement:
			if !capturing {
				continue
			}
			if elem.Name.Local == elementName {
				return nameKey, nameFallback, descKey, descFallback
			}
			if elem.Name.Local == "Name" || elem.Name.Local == "Description" {
				current = ""
			}
		}
	}

	return nameKey, nameFallback, descKey, descFallback
}

func (m *Manager) getConditionLocalizationFromXML(conditionID string, beta bool) (string, string, string, string) {
	roots := []string{m.getGameDataPathForVersion(beta)}
	if beta {
		roots = append(roots, m.getGameDataPathForVersion(false))
	}

	for _, root := range uniqueStrings(roots) {
		if root == "" {
			continue
		}
		path := filepath.Join(root, "StreamingAssets", "Data", "startconditions.xml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		nameKey, nameFallback, descKey, descFallback := extractLocalizationForElement(data, "StartCondition", conditionID)
		if nameKey != "" || nameFallback != "" || descKey != "" || descFallback != "" {
			return nameKey, nameFallback, descKey, descFallback
		}
	}

	return "", "", "", ""
}

func (m *Manager) getDifficultiesFromXML(beta bool) []string {
	roots := []string{m.getGameDataPathForVersion(beta)}
	if beta {
		roots = append(roots, m.getGameDataPathForVersion(false))
	}

	seen := make(map[string]struct{})

	for _, root := range uniqueStrings(roots) {
		if root == "" {
			continue
		}
		dataDir := filepath.Join(root, "StreamingAssets", "Data")
		candidates := []string{
			filepath.Join(dataDir, "difficultysettings.xml"),
			filepath.Join(dataDir, "DifficultySettings.xml"),
			filepath.Join(dataDir, "difficulties.xml"),
		}

		for _, path := range candidates {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			decoder := xml.NewDecoder(bytes.NewReader(data))
			for {
				tok, err := decoder.Token()
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					break
				}
				start, ok := tok.(xml.StartElement)
				if !ok {
					continue
				}
				switch strings.ToLower(start.Name.Local) {
				case "difficultysetting", "difficulty":
					for _, attr := range start.Attr {
						if strings.EqualFold(attr.Name.Local, "Id") {
							id := strings.TrimSpace(attr.Value)
							if id != "" {
								seen[id] = struct{}{}
							}
						}
					}
				}
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// GetWorldImage returns the PNG bytes for the planet image that matches the normalized world name.
func (m *Manager) GetWorldImage(worldId string, beta bool) ([]byte, error) {
	fileName := ""

	switch worldId[0:2] {
	case "Ma":
		fileName = "StatMars.png"
	case "Eu":
		fileName = "StatEuropa.png"
	case "Mi":
		fileName = "StatMimas.png"
	case "Lu":
		fileName = "StatMoon.png"
	case "Ve":
		fileName = "StatVenus.png"
	case "Vu":
		fileName = "StatVulkan.png"
	}
	imagePath := filepath.Join(m.Paths.RootPath, "bin", "release", "rocketstation_DedicatedServer_Data", "StreamingAssets", "Images", "SpaceMapImages", "Planets", fileName)
	data, err := os.ReadFile(imagePath)
	if err == nil {
		return data, nil
	}

	return nil, fmt.Errorf("world image not found for %s", worldId)
}

func (m *Manager) worldLocalizationKeys(worldID string, beta bool) (string, string, string, string) {
	canonical := canonicalWorldIdentifier(worldID)
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			if def.NameKey != "" || def.NameFallback != "" || def.DescriptionKey != "" || def.DescriptionFallback != "" {
				return def.NameKey, def.NameFallback, def.DescriptionKey, def.DescriptionFallback
			}
			if nameKey, nameFallback, descKey, descFallback, err := m.worldLocalizationFromDirectory(def.Root, def.Directory); err == nil {
				return nameKey, nameFallback, descKey, descFallback
			}
		}
	}

	technicalID := m.resolveWorldTechnicalID(worldID, beta)
	roots := []string{m.getGameDataPathForVersion(beta)}
	if beta {
		roots = append(roots, m.getGameDataPathForVersion(false))
	}

	for _, root := range uniqueStrings(roots) {
		nameKey, nameFallback, descKey, descFallback, err := m.worldLocalizationFromDirectory(root, technicalID)
		if err != nil {
			continue
		}
		if nameKey != "" || nameFallback != "" || descKey != "" || descFallback != "" {
			return nameKey, nameFallback, descKey, descFallback
		}
	}

	return "", "", "", ""
}

func (m *Manager) resolveWorldTechnicalID(worldID string, beta bool) string {
	canonical := canonicalWorldIdentifier(worldID)
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok && def.Directory != "" {
			return def.Directory
		}
	}
	return worldID
}

func (m *Manager) GetWorldInfo(worldID string, beta bool) WorldInfo {
	nameKey, nameFallback, descKey, descFallback := m.worldLocalizationKeys(worldID, beta)

	name := worldID
	description := "No description available"

	if nameKey != "" {
		if value := strings.TrimSpace(m.lookupLanguageValue(beta, nameKey)); value != "" {
			name = value
		} else if nameFallback != "" {
			name = nameFallback
		}
	} else if nameFallback != "" {
		name = nameFallback
	}

	if descKey != "" {
		if value := strings.TrimSpace(m.lookupLanguageValue(beta, descKey)); value != "" {
			description = value
		} else if descFallback != "" {
			description = descFallback
		}
	} else if descFallback != "" {
		description = descFallback
	}

	return WorldInfo{
		ID:          worldID,
		Name:        name,
		Description: description,
	}
}

// GetStartLocationsForWorld returns all start locations available for a specific world
func (m *Manager) GetStartLocationsForWorld(worldID string) []LocationInfo {
	return m.GetStartLocationsForWorldVersion(worldID, false)
}

func (m *Manager) GetStartLocationsForWorldVersion(worldID string, beta bool) []LocationInfo {
	technicalID := m.resolveWorldTechnicalID(worldID, beta)
	if technicalID == "" {
		return []LocationInfo{}
	}

	worldXMLPath := filepath.Join(m.getGameDataPathForVersion(beta), "StreamingAssets", "Worlds", technicalID, technicalID+".xml")

	data, err := os.ReadFile(worldXMLPath)
	if err != nil {
		return []LocationInfo{}
	}

	content := string(data)
	var locations []LocationInfo
	seen := make(map[string]bool)

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "<StartLocation Id=") {
			continue
		}

		start := strings.Index(line, "Id=\"")
		if start == -1 {
			continue
		}
		start += 4

		end := strings.Index(line[start:], "\"")
		if end <= 0 {
			continue
		}

		locationID := line[start : start+end]
		if locationID == "" || seen[locationID] {
			continue
		}

		seen[locationID] = true
		locationInfo := m.GetLocationInfo(locationID, beta)
		locations = append(locations, locationInfo)
	}

	return locations
}

// GetStartConditionsForWorld returns all start conditions available for a specific world
func (m *Manager) GetStartConditionsForWorld(worldID string) []ConditionInfo {
	return m.GetStartConditionsForWorldVersion(worldID, false)
}

func (m *Manager) GetStartConditionsForWorldVersion(worldID string, beta bool) []ConditionInfo {
	technicalID := m.resolveWorldTechnicalID(worldID, beta)

	worldXMLPath := filepath.Join(m.getGameDataPathForVersion(beta), "StreamingAssets", "Worlds", technicalID, technicalID+".xml")

	data, err := os.ReadFile(worldXMLPath)
	if err != nil {
		return []ConditionInfo{}
	}

	content := string(data)
	var conditions []ConditionInfo
	seen := make(map[string]bool)

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "<!--") {
			continue
		}
		if !strings.Contains(line, "<StartCondition Id=") {
			continue
		}

		start := strings.Index(line, "Id=\"")
		if start == -1 {
			continue
		}
		start += 4

		end := strings.Index(line[start:], "\"")
		if end <= 0 {
			continue
		}

		conditionID := line[start : start+end]
		if conditionID == "" || seen[conditionID] {
			continue
		}

		seen[conditionID] = true
		conditionInfo := m.GetConditionInfo(conditionID, worldID, beta)
		conditions = append(conditions, conditionInfo)
	}

	return conditions
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}

	return result
}

// GetLocationInfo returns localized information for a start location
func (m *Manager) GetLocationInfo(locationID string, beta bool) LocationInfo {
	langPath := filepath.Join(m.getGameDataPathForVersion(beta), "StreamingAssets", "Language", m.Language+".xml")

	data, err := os.ReadFile(langPath)
	if err != nil {
		return LocationInfo{
			ID:          locationID,
			Name:        locationID,
			Description: "",
		}
	}

	content := string(data)

	// Location keys follow pattern: {LocationID}Name and {LocationID}Description
	nameKey := locationID + "Name"
	descKey := locationID + "Description"

	name := locationID
	description := ""

	// Extract name
	if idx := strings.Index(content, "<Key>"+nameKey+"</Key>"); idx != -1 {
		remaining := content[idx:]
		if valueStart := strings.Index(remaining, "<Value>"); valueStart != -1 {
			valueStart += 7
			if valueEnd := strings.Index(remaining[valueStart:], "</Value>"); valueEnd != -1 {
				name = remaining[valueStart : valueStart+valueEnd]
			}
		}
	}

	// Extract description
	if idx := strings.Index(content, "<Key>"+descKey+"</Key>"); idx != -1 {
		remaining := content[idx:]
		if valueStart := strings.Index(remaining, "<Value>"); valueStart != -1 {
			valueStart += 7
			if valueEnd := strings.Index(remaining[valueStart:], "</Value>"); valueEnd != -1 {
				description = remaining[valueStart : valueStart+valueEnd]
			}
		}
	}

	return LocationInfo{
		ID:          locationID,
		Name:        name,
		Description: description,
	}
}

// GetConditionInfo returns localized information for a start condition
func (m *Manager) GetConditionInfo(conditionID string, worldID string, beta bool) ConditionInfo {
	nameKey, nameFallback, descKey, descFallback := m.getConditionLocalizationFromXML(conditionID, beta)

	name := conditionID
	if nameKey != "" {
		if value := strings.TrimSpace(m.lookupLanguageValue(beta, nameKey)); value != "" {
			name = value
		} else if nameFallback != "" {
			name = nameFallback
		}
	} else if nameFallback != "" {
		name = nameFallback
	}

	description := ""
	if descKey != "" {
		if value := strings.TrimSpace(m.lookupLanguageValue(beta, descKey)); value != "" {
			description = value
		} else if descFallback != "" {
			description = descFallback
		}
	} else if descFallback != "" {
		description = descFallback
	}

	return ConditionInfo{
		ID:          conditionID,
		Name:        name,
		Description: description,
	}
}

func (m *Manager) NextID() int {
	maxID := 0
	for _, s := range m.Servers {
		if s.ID > maxID {
			maxID = s.ID
		}
	}
	return maxID + 1
}

func (m *Manager) AddServer(cfg *models.ServerConfig) (*models.Server, error) {
	if cfg == nil {
		return nil, errors.New("server config cannot be nil")
	}

	id := m.NextID()
	srv := models.NewServerFromConfig(id, m.Paths, cfg)

	if srv.Paths != nil {
		if err := os.MkdirAll(srv.Paths.ServerLogsDir(srv.ID), 0o755); err != nil {
			m.safeLog(fmt.Sprintf("Unable to create server logs directory for %s (ID: %d): %v", srv.Name, srv.ID, err))
		}
		srv.EnsureLogger(srv.Paths)
	}

	m.Servers = append(m.Servers, srv)
	m.Log.Write(fmt.Sprintf("Server %s added successfully with ID %d.", srv.Name, srv.ID))
	m.Save()
	return srv, nil
}

func (m *Manager) ReleaseLatest() string {
	return "1.0"
}

func (m *Manager) BetaLatest() string {
	return "2.0"
}

func (m *Manager) invalidateSteamCmdVersionCache() {
	m.steamCmdMu.Lock()
	m.steamCmdVersion = ""
	m.steamCmdCheckedAt = time.Time{}
	m.steamCmdMu.Unlock()
}

func (m *Manager) invalidateBepInExVersionCache() {
	m.bepInExMu.Lock()
	m.bepInExVersion = ""
	m.bepInExCheckedAt = time.Time{}
	m.bepInExMu.Unlock()
}

func (m *Manager) invalidateLaunchPadVersionCache() {
	m.launchPadMu.Lock()
	m.launchPadVersion = ""
	m.launchPadChecked = time.Time{}
	m.launchPadMu.Unlock()
}

func (m *Manager) invalidateRocketStationVersionCache(beta bool) {
	if beta {
		m.betaVersionMu.Lock()
		m.betaVersion = ""
		m.betaCheckedAt = time.Time{}
		m.betaVersionMu.Unlock()
		return
	}
	m.releaseVersionMu.Lock()
	m.releaseVersion = ""
	m.releaseCheckedAt = time.Time{}
	m.releaseVersionMu.Unlock()
}

func (m *Manager) SteamCmdVersion() string {
	m.steamCmdMu.RLock()
	cached := m.steamCmdVersion
	cachedAt := m.steamCmdCheckedAt
	m.steamCmdMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < steamCmdVersionCacheTTL {
		return cached
	}

	m.safeLog("Checking SteamCMD version...")
	version, err := m.fetchSteamCmdVersion()
	if err != nil {
		result := "Error"
		switch {
		case errors.Is(err, os.ErrNotExist):
			m.safeLog(fmt.Sprintf("SteamCMD version check failed: %v", err))
			result = "Missing"
		case errors.Is(err, context.DeadlineExceeded):
			m.safeLog("SteamCMD version check timed out after 10s")
			result = "Timeout"
		default:
			m.safeLog(fmt.Sprintf("Failed to read SteamCMD version: %v", err))
		}

		m.steamCmdMu.Lock()
		m.steamCmdVersion = result
		m.steamCmdCheckedAt = time.Now()
		m.steamCmdMu.Unlock()
		return result
	}

	m.safeLog(fmt.Sprintf("SteamCMD version reported: %s", version))
	m.steamCmdMu.Lock()
	m.steamCmdVersion = version
	m.steamCmdCheckedAt = time.Now()
	m.steamCmdMu.Unlock()
	return version
}

func (m *Manager) SteamCmdLatest() string {
	return "0.0"
}

func (m *Manager) BepInExLatest() string {
	m.bepInExLatestMu.RLock()
	cached := m.bepInExLatest
	cachedAt := m.bepInExLatestAt
	m.bepInExLatestMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < bepinexLatestCacheTTL {
		return cached
	}

	version, err := m.fetchBepInExLatestVersion()
	if err != nil {
		m.safeLog(fmt.Sprintf("Failed to fetch latest BepInEx version: %v", err))
		if cached != "" {
			return cached
		}
		version = "Unknown"
	}

	m.bepInExLatestMu.Lock()
	m.bepInExLatest = version
	m.bepInExLatestAt = time.Now()
	m.bepInExLatestMu.Unlock()

	return version
}

func (m *Manager) ReleaseDeployed() string {
	return m.rocketStationVersion(false)
}

func (m *Manager) BetaDeployed() string {
	return m.rocketStationVersion(true)
}

func (m *Manager) rocketStationVersion(beta bool) string {
	var (
		mu       *sync.RWMutex
		cached   string
		cachedAt time.Time
		label    string
	)

	if beta {
		mu = &m.betaVersionMu
		mu.RLock()
		cached = m.betaVersion
		cachedAt = m.betaCheckedAt
		mu.RUnlock()
		label = "beta"
	} else {
		mu = &m.releaseVersionMu
		mu.RLock()
		cached = m.releaseVersion
		cachedAt = m.releaseCheckedAt
		mu.RUnlock()
		label = "release"
	}

	if cached != "" && time.Since(cachedAt) < rocketStationVersionCacheTTL {
		return cached
	}

	version, err := m.fetchRocketStationBuildID(beta)
	if err != nil {
		result := "Error"
		switch {
		case errors.Is(err, os.ErrNotExist):
			result = "Missing"
		case errors.Is(err, errRocketStationVersionNotFound):
			result = "Unknown"
			m.safeLog(fmt.Sprintf("Stationeers %s installation found but build ID missing", label))
		default:
			m.safeLog(fmt.Sprintf("Failed to determine Stationeers %s build ID: %v", label, err))
		}

		mu.Lock()
		if beta {
			m.betaVersion = result
			m.betaCheckedAt = time.Now()
		} else {
			m.releaseVersion = result
			m.releaseCheckedAt = time.Now()
		}
		mu.Unlock()
		return result
	}

	m.safeLog(fmt.Sprintf("Stationeers %s build ID: %s", label, version))

	mu.Lock()
	if beta {
		m.betaVersion = version
		m.betaCheckedAt = time.Now()
	} else {
		m.releaseVersion = version
		m.releaseCheckedAt = time.Now()
	}
	mu.Unlock()

	return version
}

func (m *Manager) SteamCmdDeployed() string {
	return m.SteamCmdVersion()
}

func (m *Manager) BepInExDeployed() string {
	m.bepInExMu.RLock()
	cached := m.bepInExVersion
	cachedAt := m.bepInExCheckedAt
	m.bepInExMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < bepinexVersionCacheTTL {
		return cached
	}

	version, err := m.fetchBepInExVersion()
	if err != nil {
		result := "Error"
		switch {
		case errors.Is(err, os.ErrNotExist):
			m.safeLog("BepInEx version check failed: installation not found")
			result = "Missing"
		default:
			m.safeLog(fmt.Sprintf("Failed to determine BepInEx version: %v", err))
		}
		m.bepInExMu.Lock()
		m.bepInExVersion = result
		m.bepInExCheckedAt = time.Now()
		m.bepInExMu.Unlock()
		return result
	}

	m.safeLog(fmt.Sprintf("BepInEx version reported: %s", version))
	m.bepInExMu.Lock()
	m.bepInExVersion = version
	m.bepInExCheckedAt = time.Now()
	m.bepInExMu.Unlock()
	return version
}

func (m *Manager) fetchBepInExLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, bepinexLatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sdsm-manager/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github responded with %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	version := strings.TrimSpace(payload.TagName)
	if version == "" {
		return "", fmt.Errorf("github response missing tag_name")
	}
	if len(version) > 0 {
		switch version[0] {
		case 'v', 'V':
			version = version[1:]
		}
	}
	return version, nil
}

func (m *Manager) LaunchPadLatest() string {
	m.launchPadLatestMu.RLock()
	cached := m.launchPadLatest
	cachedAt := m.launchPadLatestAt
	m.launchPadLatestMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < launchPadLatestCacheTTL {
		return cached
	}

	version, err := m.fetchLaunchPadLatestVersion()
	if err != nil {
		m.safeLog(fmt.Sprintf("Failed to fetch latest Stationeers LaunchPad version: %v", err))
		if cached != "" {
			return cached
		}
		version = "Unknown"
	} else {
		m.safeLog(fmt.Sprintf("Stationeers LaunchPad latest version reported: %s", version))
	}

	m.launchPadLatestMu.Lock()
	m.launchPadLatest = version
	m.launchPadLatestAt = time.Now()
	m.launchPadLatestMu.Unlock()

	return version
}

func (m *Manager) fetchLaunchPadLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, launchPadLatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sdsm-manager/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github responded with %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	version := strings.TrimSpace(payload.TagName)
	if version == "" {
		return "", fmt.Errorf("github response missing tag_name")
	}
	if len(version) > 0 {
		switch version[0] {
		case 'v', 'V':
			version = version[1:]
		}
	}

	return version, nil
}

func (m *Manager) LaunchPadDeployed() string {
	m.launchPadMu.RLock()
	cached := m.launchPadVersion
	cachedAt := m.launchPadChecked
	m.launchPadMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < launchPadVersionCacheTTL {
		return cached
	}

	version, err := m.fetchLaunchPadDeployedVersion()
	if err != nil {
		result := "Error"
		switch {
		case errors.Is(err, os.ErrNotExist):
			result = "Missing"
		case errors.Is(err, errLaunchPadVersionNotFound):
			result = "Installed"
			m.safeLog("Stationeers LaunchPad installation found but version metadata missing")
		default:
			m.safeLog(fmt.Sprintf("Failed to determine Stationeers LaunchPad deployed version: %v", err))
		}

		m.launchPadMu.Lock()
		m.launchPadVersion = result
		m.launchPadChecked = time.Now()
		m.launchPadMu.Unlock()
		return result
	}

	m.safeLog(fmt.Sprintf("Stationeers LaunchPad deployed version: %s", version))
	m.launchPadMu.Lock()
	m.launchPadVersion = version
	m.launchPadChecked = time.Now()
	m.launchPadMu.Unlock()

	return version
}

func (m *Manager) fetchLaunchPadDeployedVersion() (string, error) {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	root := m.Paths.LaunchPadDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	if len(entries) == 0 {
		return "", errLaunchPadVersionNotFound
	}

	var dllPath string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "StationeersLaunchPad.dll") {
			dllPath = path
			return errLaunchPadDLLFound
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, errLaunchPadDLLFound) {
		return "", walkErr
	}

	if dllPath == "" {
		return "", errLaunchPadVersionNotFound
	}

	version := extractLaunchPadVersionFromDLL(dllPath)
	if version == "" {
		return "", errLaunchPadVersionNotFound
	}

	return version, nil
}

func (m *Manager) fetchRocketStationBuildID(beta bool) (string, error) {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	installDir := m.Paths.ReleaseDir()
	if beta {
		installDir = m.Paths.BetaDir()
	}

	if installDir == "" {
		return "", os.ErrNotExist
	}

	if _, err := os.Stat(installDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	steamID := strings.TrimSpace(m.SteamID)
	if steamID == "" {
		steamID = "600760"
	}

	manifestPath := filepath.Join(installDir, "steamapps", fmt.Sprintf("appmanifest_%s.acf", steamID))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Treat missing manifest as missing install when directory is mostly empty
			entries, dirErr := os.ReadDir(installDir)
			if dirErr == nil && len(entries) == 0 {
				return "", os.ErrNotExist
			}
			return "", errRocketStationVersionNotFound
		}
		return "", err
	}

	if matches := appManifestBuildIDPattern.FindSubmatch(data); len(matches) == 2 {
		return string(matches[1]), nil
	}

	// Fallback to buildid embedded in plain text files
	if v := m.findRocketStationVersionFallbacks(installDir); v != "" {
		return v, nil
	}

	return "", errRocketStationVersionNotFound
}

func (m *Manager) findRocketStationVersionFallbacks(root string) string {
	candidates := []string{
		filepath.Join(root, "buildid.txt"),
		filepath.Join(root, "BuildID.txt"),
		filepath.Join(root, "BUILDID"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(data))
		if value == "" {
			continue
		}
		if isDigitsOnly(value) {
			return value
		}
	}

	return ""
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func extractLaunchPadVersionFromDLL(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.Size() > 32*1024*1024 {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	anchors := [][]byte{
		[]byte("stationeers.launchpad"),
		[]byte("StationeersLaunchPad"),
		[]byte("pluginVersion"),
	}
	for _, anchor := range anchors {
		if version := findVersionNearAnchor(data, anchor); version != "" {
			return version
		}
	}

	if match := launchPadVersionPattern.Find(data); match != nil {
		return string(match)
	}

	return ""
}

func findVersionNearAnchor(data, anchor []byte) string {
	search := 0
	for search < len(data) {
		idx := bytes.Index(data[search:], anchor)
		if idx == -1 {
			break
		}
		idx += search
		start := idx - 64
		if start < 0 {
			start = 0
		}
		end := idx + len(anchor) + 256
		if end > len(data) {
			end = len(data)
		}

		if match := launchPadVersionPattern.Find(data[start:end]); match != nil {
			return string(match)
		}

		search = idx + len(anchor)
	}
	return ""
}

func (m *Manager) fetchSteamCmdVersion() (string, error) {
	executable := "steamcmd.sh"
	if runtime.GOOS == "windows" {
		executable = "steamcmd.exe"
	}

	steamCmdPath := filepath.Join(m.Paths.SteamDir(), executable)
	if _, err := os.Stat(steamCmdPath); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, steamCmdPath, "+quit")
	cmd.Dir = filepath.Dir(steamCmdPath)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("steamcmd execution failed: %w", err)
	}

	matches := steamCmdVersionPattern.FindStringSubmatch(string(output))
	if len(matches) != 2 {
		return "", fmt.Errorf("unable to parse SteamCMD version")
	}

	return matches[1], nil
}

func (m *Manager) fetchBepInExVersion() (string, error) {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	if version := m.readPersistedBepInExVersion(); version != "" {
		return version, nil
	}

	paths := m.bepInExCandidateFiles()
	if len(paths) == 0 {
		return "", os.ErrNotExist
	}

	missing := 0
	total := 0

	for _, path := range paths {
		if path == "" {
			continue
		}
		total++
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				missing++
				continue
			}
			return "", err
		}

		if version := extractBepInExVersion(data); version != "" {
			return version, nil
		}
	}

	if total == 0 || missing == total {
		return "", os.ErrNotExist
	}

	return "", fmt.Errorf("unable to locate BepInEx version marker")
}

func extractBepInExVersion(data []byte) string {
	matches := bepinexVersionPattern.FindAll(data, -1)
	best := ""
	bestScore := -1
	for _, match := range matches {
		candidate := string(match)
		if !isLikelyBepInExVersion(candidate) {
			continue
		}
		score := bepInExVersionConfidence(candidate)
		if score > bestScore || (score == bestScore && compareVersionStrings(candidate, best) > 0) {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func isLikelyBepInExVersion(version string) bool {
	if strings.Count(version, ".") != 3 {
		return false
	}
	major, ok := bepInExMajor(version)
	if !ok || major <= 0 {
		return false
	}
	return true
}

func bepInExVersionConfidence(version string) int {
	major, ok := bepInExMajor(version)
	if !ok {
		return 0
	}
	switch {
	case major == 5 || major == 6:
		return 3
	case major > 0 && major <= 9:
		return 2
	case major < 100:
		return 1
	default:
		return 0
	}
}

func bepInExMajor(version string) (int, bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 4 {
		return 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return major, true
}

func (m *Manager) readPersistedBepInExVersion() string {
	if m.Paths == nil {
		return ""
	}
	versionFile := filepath.Join(m.Paths.BepInExDir(), bepInExVersionFile)
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return ""
	}
	if version := sanitizeBepInExVersionString(string(data)); version != "" {
		return version
	}
	return ""
}

func sanitizeBepInExVersionString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	for i, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			value = value[:i]
			break
		}
	}
	if !isLikelyBepInExVersion(value) {
		return ""
	}
	return value
}

func compareVersionStrings(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for len(partsA) < maxLen {
		partsA = append(partsA, "0")
	}
	for len(partsB) < maxLen {
		partsB = append(partsB, "0")
	}

	for i := 0; i < maxLen; i++ {
		aVal, _ := strconv.Atoi(partsA[i])
		bVal, _ := strconv.Atoi(partsB[i])
		if aVal > bVal {
			return 1
		}
		if aVal < bVal {
			return -1
		}
	}

	return 0
}

// CheckMissingComponents checks for missing SteamCMD, game files, and BepInEx
func (m *Manager) CheckMissingComponents() {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()

	previous := append([]string(nil), m.MissingComponents...)
	m.MissingComponents = []string{}
	var missingDetails []string

	// Check for SteamCMD
	steamCmdPath := filepath.Join(m.Paths.SteamDir(), "steamcmd.sh")
	if _, err := os.Stat(steamCmdPath); os.IsNotExist(err) {
		m.MissingComponents = append(m.MissingComponents, "SteamCMD")
		missingDetails = append(missingDetails, fmt.Sprintf("SteamCMD expected at %s", steamCmdPath))
	}

	// Check for Stationeers game files (Release)
	releasePath := filepath.Join(m.Paths.ReleaseDir(), "rocketstation_DedicatedServer.x86_64")
	if _, err := os.Stat(releasePath); os.IsNotExist(err) {
		m.MissingComponents = append(m.MissingComponents, "Stationeers Release")
		missingDetails = append(missingDetails, fmt.Sprintf("Stationeers release binary expected at %s", releasePath))
	}

	// Check for BepInEx
	if !m.hasBepInExInstall() {
		m.MissingComponents = append(m.MissingComponents, "BepInEx")
		missingDetails = append(missingDetails, fmt.Sprintf("BepInEx files expected under %s", m.Paths.BepInExDir()))
	}

	// Check for StationeersLaunchPad (ensure directory contains files)
	launchPadDir := m.Paths.LaunchPadDir()
	if entries, err := os.ReadDir(launchPadDir); err != nil || len(entries) == 0 {
		m.MissingComponents = append(m.MissingComponents, "Stationeers LaunchPad")
		missingDetails = append(missingDetails, fmt.Sprintf("Stationeers LaunchPad expected at %s", launchPadDir))
	}

	changed := !reflect.DeepEqual(previous, m.MissingComponents)

	if len(m.MissingComponents) > 0 {
		m.NeedsUploadPrompt = true
		if changed {
			m.Log.Write(fmt.Sprintf("Missing components detected: %v", m.MissingComponents))
			for _, detail := range missingDetails {
				m.Log.Write("- " + detail)
			}
		}
	} else {
		m.NeedsUploadPrompt = false
		if changed {
			m.Log.Write("All required components detected")
		}
	}
}

func (m *Manager) GetMissingComponents() []string {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	if len(m.MissingComponents) == 0 {
		return nil
	}
	comps := make([]string, len(m.MissingComponents))
	copy(comps, m.MissingComponents)
	return comps
}

func (m *Manager) hasBepInExInstall() bool {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	for _, candidate := range m.bepInExCandidateFiles() {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}

	return false
}

func (m *Manager) bepInExCandidateFiles() []string {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	var candidates []string
	root := m.Paths.BepInExDir()
	if root != "" {
		candidates = append(candidates,
			filepath.Join(root, "BepInEx", "core", "BepInEx.dll"),
			filepath.Join(root, "BepInEx", "core", "BepInEx.Preloader.dll"),
			filepath.Join(root, "doorstop_config.ini"),
		)
	}

	return uniqueStrings(candidates)
}

func (m *Manager) ServerCount() int {
	return len(m.Servers)
}

func (m *Manager) ServerCountActive() int {
	count := 0
	for _, s := range m.Servers {
		if s.IsRunning() {
			count++
		}
	}
	return count
}

func (m *Manager) ServerByID(id int) *models.Server {
	for _, srv := range m.Servers {
		if srv != nil && srv.ID == id {
			return srv
		}
	}
	return nil
}

func (m *Manager) GetTotalPlayers() int {
	total := 0
	for _, server := range m.Servers {
		total += server.ClientCount()
	}
	return total
}

// IsPortAvailable checks if a port is available (not used by any server and at least 3 ports away)
func (m *Manager) IsPortAvailable(port int, excludeServerID int) bool {
	for _, srv := range m.Servers {
		if srv.ID == excludeServerID {
			continue
		}
		// Check if port conflicts or is within 3 ports of existing server
		if abs(srv.Port-port) < 3 {
			return false
		}
	}
	return true
}

// IsServerNameAvailable checks if a server name is unique
func (m *Manager) IsServerNameAvailable(name string, excludeServerID int) bool {
	for _, srv := range m.Servers {
		if srv.ID == excludeServerID {
			continue
		}
		if srv.Name == name {
			return false
		}
	}
	return true
}

// GetNextAvailablePort returns the next available port starting from the suggested port
func (m *Manager) GetNextAvailablePort(suggestedPort int) int {
	port := suggestedPort
	for !m.IsPortAvailable(port, -1) {
		port += 3 // Jump by 3 to ensure spacing
	}
	return port
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
