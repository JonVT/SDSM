// Package manager orchestrates game-data scanning, caching, deployments,
// and server lifecycle for the Stationeers Dedicated Server Manager (SDSM).
package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	fs "io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"sdsm/internal/models"
	"sdsm/internal/utils"
	"sdsm/steam"
)

const (
	sconVersionCacheTTL = 5 * time.Minute
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
	DeployTypeSCON      DeployType = "SCON"
)

type Manager struct {
	Active            bool             `json:"-"`
	ConfigFile        string           `json:"-"`
	Updating          bool             `json:"-"`
	SetupInProgress   bool             `json:"-"`
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
	// SCON latest/deployed caches
	sconLatestMu     sync.RWMutex `json:"-"`
	sconLatest       string       `json:"-"`
	sconLatestAt     time.Time    `json:"-"`
	sconMu           sync.RWMutex `json:"-"`
	sconVersion      string       `json:"-"`
	sconChecked      time.Time    `json:"-"`
	releaseVersionMu sync.RWMutex `json:"-"`
	releaseVersion   string       `json:"-"`
	releaseCheckedAt time.Time    `json:"-"`
	betaVersionMu    sync.RWMutex `json:"-"`
	betaVersion      string       `json:"-"`
	betaCheckedAt    time.Time    `json:"-"`
	// Latest available (Steam) build IDs cache
	releaseLatestMu sync.RWMutex `json:"-"`
	releaseLatest   string       `json:"-"`
	releaseLatestAt time.Time    `json:"-"`
	betaLatestMu    sync.RWMutex `json:"-"`
	betaLatest      string       `json:"-"`
	betaLatestAt    time.Time    `json:"-"`
	worldIndexMu    sync.RWMutex `json:"-"`
	worldIndex      map[bool]*worldDefinitionCache
	// Per-language caches to avoid rebuilding world/difficulty data on every request
	worldIndexByLangMu sync.RWMutex `json:"-"`
	worldIndexByLang   map[bool]map[string]*worldDefinitionCache
	diffByLangMu       sync.RWMutex `json:"-"`
	diffByLang         map[bool]map[string]struct {
		list        []string
		generatedAt time.Time
	}
	// Languages cache per channel (beta=false/true) to avoid disk scan every ManagerGET.
	languagesCacheMu sync.RWMutex `json:"-"`
	languagesCache   map[bool]struct {
		list     []string
		cachedAt time.Time
	}
	progressMu       sync.RWMutex
	progressByType   map[DeployType]*UpdateProgress
	serverProgressMu sync.RWMutex
	serverProgress   map[int]*ServerCopyProgress
	// DetachedServers determines whether managed game server processes should remain
	// running if SDSM exits. When true, user-initiated shutdown can optionally leave
	// servers running. Processes are started in their own process group to avoid
	// receiving parent signals.
	DetachedServers bool `json:"detached_servers"`
	// TrayEnabled controls whether the Windows tray icon is started (Windows only).
	TrayEnabled bool `json:"tray_enabled"`
	// TLS settings for serving HTTPS. Effective at process start.
	TLSEnabled  bool   `json:"tls_enabled"`
	TLSCertPath string `json:"tls_cert"`
	TLSKeyPath  string `json:"tls_key"`
	// Web and logging behavior
	VerboseHTTP   bool `json:"verbose_http"`
	VerboseUpdate bool `json:"verbose_update"`
	// Security and auth
	JWTSecret string `json:"jwt_secret"`
	// Cookie settings
	CookieForceSecure bool   `json:"cookie_force_secure"`
	CookieSameSite    string `json:"cookie_samesite"`
	// UI embedding policy
	AllowIFrame bool `json:"allow_iframe"`
	// AutoPortForwardManager enables automatic router port forwarding (TCP) for the
	// manager's own HTTP(S) port via UPnP/NAT-PMP when available. Default is false.
	AutoPortForwardManager bool `json:"auto_port_forward_manager"`
	// Windows process discovery behavior (Windows-only); when true, WMI may be used.
	WindowsDiscoveryWMIEnabled bool `json:"windows_discovery_wmi_enabled"`
	// SCON overrides (optional)
	SCONRepoOverride       string `json:"scon_repo_override"`
	SCONURLLinuxOverride   string `json:"scon_url_linux_override"`
	SCONURLWindowsOverride string `json:"scon_url_windows_override"`
	// Discord integration
	// DiscordDefaultWebhook is the per-manager default webhook for notifications (servers, updates)
	DiscordDefaultWebhook string `json:"discord_default_webhook"`
	// Notification preferences (manager defaults)
	// NotifyEnableDeploy controls whether deployment/update notifications are sent to Discord.
	NotifyEnableDeploy bool `json:"notify_enable_deploy"`
	// NotifyEnableServer controls whether server lifecycle/update notifications are sent by default.
	NotifyEnableServer bool `json:"notify_enable_server"`
	// Per-event defaults for server notifications. Servers can override these individually.
	NotifyOnStart           bool `json:"notify_on_start"`
	NotifyOnStopping        bool `json:"notify_on_stopping"`
	NotifyOnStopped         bool `json:"notify_on_stopped"`
	NotifyOnRestart         bool `json:"notify_on_restart"`
	NotifyOnUpdateStarted   bool `json:"notify_on_update_started"`
	NotifyOnUpdateCompleted bool `json:"notify_on_update_completed"`
	NotifyOnUpdateFailed    bool `json:"notify_on_update_failed"`
	// Per-event message templates (tokens: {{server_name}}, {{event}}, {{detail}}, {{timestamp}})
	NotifyMsgStart           string `json:"notify_msg_start"`
	NotifyMsgStopping        string `json:"notify_msg_stopping"`
	NotifyMsgStopped         string `json:"notify_msg_stopped"`
	NotifyMsgRestart         string `json:"notify_msg_restart"`
	NotifyMsgUpdateStarted   string `json:"notify_msg_update_started"`
	NotifyMsgUpdateCompleted string `json:"notify_msg_update_completed"`
	NotifyMsgUpdateFailed    string `json:"notify_msg_update_failed"`
	// Per-event colors as hex strings (#RRGGBB); parsed at use-time. Fallback to defaults if invalid.
	NotifyColorStart           string `json:"notify_color_start"`
	NotifyColorStopping        string `json:"notify_color_stopping"`
	NotifyColorStopped         string `json:"notify_color_stopped"`
	NotifyColorRestart         string `json:"notify_color_restart"`
	NotifyColorUpdateStarted   string `json:"notify_color_update_started"`
	NotifyColorUpdateCompleted string `json:"notify_color_update_completed"`
	NotifyColorUpdateFailed    string `json:"notify_color_update_failed"`
	// Deploy notification templates & colors (manager-wide)
	// Tokens: {{component}}, {{duration}}, {{errors}}, {{status}}, {{timestamp}}
	NotifyMsgDeployStarted        string `json:"notify_msg_deploy_started"`
	NotifyMsgDeployCompleted      string `json:"notify_msg_deploy_completed"`
	NotifyMsgDeployCompletedError string `json:"notify_msg_deploy_completed_error"`
	NotifyColorDeployStarted        string `json:"notify_color_deploy_started"`
	NotifyColorDeployCompleted      string `json:"notify_color_deploy_completed"`
	NotifyColorDeployCompletedError string `json:"notify_color_deploy_completed_error"`
	// Transient NAT/port forward status for manager port (not persisted)
	ManagerPortForwardActive       bool          `json:"-"`
	ManagerPortForwardExternalPort int           `json:"-"`
	ManagerPortForwardLastError    string        `json:"-"`
	pfStop                         chan struct{} `json:"-"`
	// OnServerAttached is an optional callback invoked whenever the manager
	// attaches to an already running detached server process during initialization
	// or through recovery flows. Handlers can set this to trigger realtime UI
	// broadcasts so dashboards update immediately after attach.
	OnServerAttached func(*models.Server) `json:"-"`
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
	DeployTypeSCON:      "SCON",
}

var progressOrder = []DeployType{
	// Display SteamCMD first so setup progress shows it at the top
	DeployTypeSteamCMD,
	DeployTypeRelease,
	DeployTypeBeta,
	DeployTypeBepInEx,
	DeployTypeLaunchPad,
	DeployTypeSCON,
}

// (Legacy removed) Previously: SDSMCommunityBugReportWebhook constant and related
// backward compatibility code for a persistent bug report webhook field. The
// bug report webhook is now a fixed value used only at submission time.

func (dt DeployType) key() string {
	return strings.ToLower(string(dt))
}

func (dt DeployType) displayName() string {
	if name, ok := deployDisplayNames[dt]; ok {
		return name
	}
	return string(dt)
}

func NewManager() *Manager { return NewManagerWithConfig("") }

// NewManagerWithConfig creates a Manager loading configuration from the provided path.
// When configPath is empty, it defaults to ./sdsm.config in the current working directory.
// No environment variables are consulted.
func NewManagerWithConfig(configPath string) *Manager {
	m := &Manager{
		SteamID:                    "600760",
		SavedPath:                  "",
		Port:                       5000,
		Language:                   "english",
		Servers:                    []*models.Server{},
		UpdateTime:                 time.Time{},
		StartupUpdate:              true,
		progressByType:             make(map[DeployType]*UpdateProgress),
		serverProgress:             make(map[int]*ServerCopyProgress),
		TrayEnabled:                runtime.GOOS == "windows", // default true on Windows, false elsewhere
		TLSEnabled:                 false,
		TLSCertPath:                "",
		TLSKeyPath:                 "",
		AutoPortForwardManager:     false,
		VerboseHTTP:                false,
		VerboseUpdate:              false,
		JWTSecret:                  "your-secret-key-change-in-production",
		CookieForceSecure:          false,
		CookieSameSite:             "none",
		AllowIFrame:                false,
		WindowsDiscoveryWMIEnabled: true,
		// Default notification preferences
		NotifyEnableDeploy:        true,
		NotifyEnableServer:        true,
		NotifyOnStart:             true,
		NotifyOnStopping:          true,
		NotifyOnStopped:           true,
		NotifyOnRestart:           true,
		NotifyOnUpdateStarted:     true,
		NotifyOnUpdateCompleted:   true,
		NotifyOnUpdateFailed:      true,
		// Default templates
		NotifyMsgStart:           "Server {{server_name}} started.",
		NotifyMsgStopping:        "Server {{server_name}} stopping.",
		NotifyMsgStopped:         "Server {{server_name}} stopped.",
		NotifyMsgRestart:         "Server {{server_name}} {{event}}.",
		NotifyMsgUpdateStarted:   "Server {{server_name}} update started.",
		NotifyMsgUpdateCompleted: "Server {{server_name}} update completed successfully.",
		NotifyMsgUpdateFailed:    "Server {{server_name}} update failed.",
		// Default colors (hex)
		NotifyColorStart:           "#16A34A",
		NotifyColorStopping:        "#F59E0B",
		NotifyColorStopped:         "#DC2626",
		NotifyColorRestart:         "#F59E0B",
		NotifyColorUpdateStarted:   "#2563EB",
		NotifyColorUpdateCompleted: "#16A34A",
		NotifyColorUpdateFailed:    "#DC2626",
		// Deploy templates/colors defaults
		NotifyMsgDeployStarted:        "Deployment started: {{component}}.",
		NotifyMsgDeployCompleted:      "Deployment completed: {{component}} in {{duration}}.",
		NotifyMsgDeployCompletedError: "Deployment completed with errors: {{component}} in {{duration}} ({{errors}}).",
		NotifyColorDeployStarted:        "#2563EB",
		NotifyColorDeployCompleted:      "#16A34A",
		NotifyColorDeployCompletedError: "#DC2626",
	}

	// (Legacy removed) Bug report webhook field initialization removed.

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

	// Initialize paths based on the executable directory until the config is loaded
	// This avoids creating logs in the current working directory.
	if exe, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
			exe = resolved
		}
		execDir := filepath.Dir(exe)
		m.Paths = utils.NewPaths(execDir)
	} else {
		// Fallback to a safe temp location if executable path cannot be determined
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	// Prepare logging early so load() can report issues
	m.startLogs()

	// Resolve configuration file path: prefer provided path; otherwise ./sdsm.config
	var config string
	if strings.TrimSpace(configPath) != "" {
		config = strings.TrimSpace(configPath)
	} else {
		config = "sdsm.config"
	}
	// Bootstrap default config if missing
	if !fileExists(config) {
		// Use current working directory as root for default config
		cwd, err := os.Getwd()
		if err != nil {
			m.safeLog(fmt.Sprintf("Unable to determine current working directory for default config: %v", err))
			return m
		}
		if err := m.bootstrapDefaultConfig(config, cwd); err != nil {
			m.safeLog(fmt.Sprintf("Unable to create default configuration at %s: %v", config, err))
			return m
		}
		m.safeLog(fmt.Sprintf("Created default configuration at %s", config))
	}

	m.ConfigFile = config
	_, err := m.load()
	if err != nil {
		m.safeLog(err.Error())
		return m
	}

	// Start logs after paths are properly loaded
	// Initialize logging once; avoid duplicate log start if already initialized.
	if m.Log == nil || m.UpdateLog == nil {
		m.startLogs()
	}
	m.initializeServers()

	m.safeLog("Configuration loaded")

	// Check for missing components before attempting deploy
	m.CheckMissingComponents()

	// Present setup prompt only when required components are missing (handled above in CheckMissingComponents).
	// Do not block startup updates merely because updates are available on first start.
	// This ensures StartupUpdate works consistently across runs.

	// Perform selective startup updates on every process start (not just first activation)
	// when enabled and no interactive prompt is needed. Previously gated by !wasActive;
	// removed to ensure beta/release channels update each launch when behind.
	if m.StartupUpdate && !m.NeedsUploadPrompt {
		// Ensure we evaluate against fresh values by clearing short-lived caches first.
		m.invalidateRocketStationVersionCache(false)
		m.invalidateRocketStationVersionCache(true)
		// Also refresh latest build IDs cache prior to evaluation.
		_ = m.ReleaseLatest()
		_ = m.BetaLatest()
		// Prime deployed values as well.
		_ = m.ReleaseDeployed()
		_ = m.BetaDeployed()

		// Diagnostic matrix of deployed vs latest at startup to help trace why updates may be skipped.
		m.safeLog(fmt.Sprintf("Startup update diagnostic: Release deployed=%s latest=%s | Beta deployed=%s latest=%s | BepInEx deployed=%s latest=%s | LaunchPad deployed=%s latest=%s | SCON deployed=%s latest=%s",
			strings.TrimSpace(m.ReleaseDeployed()), strings.TrimSpace(m.ReleaseLatest()),
			strings.TrimSpace(m.BetaDeployed()), strings.TrimSpace(m.BetaLatest()),
			strings.TrimSpace(m.BepInExDeployed()), strings.TrimSpace(m.BepInExLatest()),
			strings.TrimSpace(m.LaunchPadDeployed()), strings.TrimSpace(m.LaunchPadLatest()),
			strings.TrimSpace(m.SCONDeployed()), strings.TrimSpace(m.SCONLatest()),
		))
		types := m.ComponentsNeedingUpdate()
		if len(types) == 0 {
			m.safeLog("Startup update: all components are up-to-date; skipping deployment")
		} else {
			m.safeLog(fmt.Sprintf("Startup update: updating out-of-sync components: %v", types))
			if err := m.DeployTypes(types); err != nil {
				m.safeLog(fmt.Sprintf("Startup selective deployment failed: %v", err))
			}
		}
	}

	return m
}

// UpdatesAvailable returns true if any managed component appears out-of-date
// compared to its latest known version/build.
func (m *Manager) UpdatesAvailable() bool {
	// Check core game channels by build ID
	if deployed := strings.TrimSpace(m.ReleaseDeployed()); deployed != "Missing" && deployed != "Unknown" && deployed != "Error" {
		latest := strings.TrimSpace(m.ReleaseLatest())
		if latest != "Unknown" && latest != "" && latest != deployed {
			return true
		}
	}
	if deployed := strings.TrimSpace(m.BetaDeployed()); deployed != "Missing" && deployed != "Unknown" && deployed != "Error" {
		latest := strings.TrimSpace(m.BetaLatest())
		if latest != "Unknown" && latest != "" && latest != deployed {
			return true
		}
	}

	// BepInEx: normalize by comparing prefix (latest is usually 3-part, deployed 4-part)
	if dep := strings.TrimSpace(m.BepInExDeployed()); dep != "Missing" && dep != "Error" && dep != "Installed" && dep != "Unknown" && dep != "" {
		latest := strings.TrimSpace(m.BepInExLatest())
		if latest != "Unknown" && latest != "" {
			if !strings.HasPrefix(dep, latest) {
				return true
			}
		}
	}

	// LaunchPad: exact or prefix match, deployed may report "Installed"
	if dep := strings.TrimSpace(m.LaunchPadDeployed()); dep != "Missing" && dep != "Error" && dep != "" && dep != "Installed" && dep != "Unknown" {
		latest := strings.TrimSpace(m.LaunchPadLatest())
		if latest != "Unknown" && latest != "" {
			if !strings.EqualFold(dep, latest) && !strings.HasPrefix(strings.ToLower(dep), strings.ToLower(latest)) {
				return true
			}
		}
	}

	// SCON: tags often include leading 'v' in latest; deployed may store same tag
	if dep := strings.TrimSpace(m.SCONDeployed()); dep != "Missing" && dep != "Error" && dep != "" && dep != "Installed" && dep != "Unknown" {
		latest := strings.TrimSpace(m.SCONLatest())
		if latest != "Unknown" && latest != "" {
			dl := strings.ToLower(dep)
			ll := strings.ToLower(latest)
			if dl != ll {
				return true
			}
		}
	}

	// SteamCMD has no stable external latest; skip
	return false
}

// ComponentsNeedingUpdate returns the list of DeployType components that appear
// out-of-date relative to their latest known versions. SteamCMD is excluded
// (no stable external latest). Servers are included when any underlying
// component requires an update so that copied plugin / data state remains
// consistent.
func (m *Manager) ComponentsNeedingUpdate() []DeployType {
	var out []DeployType

	// Optional verbose evaluation controlled by configuration. When VerboseUpdate is true
	// each component's decision path is logged (deployed/latest values, sentinel classification, mismatch).
	verbose := m.VerboseUpdate

	// Helper predicates replicating UpdateAvailable logic while surfacing per-component types
	isSentinel := func(v string) bool {
		if v == "" {
			return true
		}
		v = strings.TrimSpace(v)
		switch v {
		case "Missing", "Unknown", "Error":
			return true
		}
		return false
	}

	logVerbose := func(format string, args ...interface{}) {
		if verbose {
			m.safeLog("UpdateEval: " + fmt.Sprintf(format, args...))
		}
	}

	// Treat "Missing" for Release/Beta channels as an actionable update (install) instead of skipping.
	// This allows startup updates to bootstrap absent installations automatically.
	shouldInstallIfMissing := func(v string) bool {
		return strings.TrimSpace(v) == "Missing"
	}

	// Release
	if dep := strings.TrimSpace(m.ReleaseDeployed()); shouldInstallIfMissing(dep) {
		logVerbose("Release channel missing; scheduling install")
		out = append(out, DeployTypeRelease)
	} else if !isSentinel(dep) {
		latest := strings.TrimSpace(m.ReleaseLatest())
		if latest != "" && latest != "Unknown" && latest != dep {
			logVerbose("Release mismatch: deployed=%s latest=%s", dep, latest)
			out = append(out, DeployTypeRelease)
		} else {
			logVerbose("Release up-to-date: deployed=%s latest=%s", dep, latest)
		}
	} else {
		logVerbose("Release sentinel value '%s' detected; skipping (not actionable)", dep)
	}
	// Beta
	if dep := strings.TrimSpace(m.BetaDeployed()); shouldInstallIfMissing(dep) {
		logVerbose("Beta channel missing; scheduling install")
		out = append(out, DeployTypeBeta)
	} else if !isSentinel(dep) {
		latest := strings.TrimSpace(m.BetaLatest())
		if latest != "" && latest != "Unknown" && latest != dep {
			logVerbose("Beta mismatch: deployed=%s latest=%s", dep, latest)
			out = append(out, DeployTypeBeta)
		} else {
			logVerbose("Beta up-to-date: deployed=%s latest=%s", dep, latest)
		}
	} else {
		logVerbose("Beta sentinel value '%s' detected; skipping (not actionable)", dep)
	}
	// BepInEx: compare prefix to handle deployed having 4-part version while latest often 3-part
	if dep := strings.TrimSpace(m.BepInExDeployed()); dep != "" && dep != "Missing" && dep != "Error" && dep != "Installed" && dep != "Unknown" {
		latest := strings.TrimSpace(m.BepInExLatest())
		if latest != "" && latest != "Unknown" && !strings.HasPrefix(dep, latest) {
			logVerbose("BepInEx mismatch: deployed=%s latest=%s", dep, latest)
			out = append(out, DeployTypeBepInEx)
		} else {
			logVerbose("BepInEx up-to-date or sentinel: deployed=%s latest=%s", dep, latest)
		}
	}
	// LaunchPad: skip when Installed/Missing/Error/Unknown; deployed may be 'Installed' when metadata missing
	if dep := strings.TrimSpace(m.LaunchPadDeployed()); dep != "" && dep != "Missing" && dep != "Error" && dep != "Installed" && dep != "Unknown" {
		latest := strings.TrimSpace(m.LaunchPadLatest())
		if latest != "" && latest != "Unknown" {
			dl := strings.ToLower(dep)
			ll := strings.ToLower(latest)
			if dl != ll && !strings.HasPrefix(dl, ll) {
				logVerbose("LaunchPad mismatch: deployed=%s latest=%s", dep, latest)
				out = append(out, DeployTypeLaunchPad)
			} else {
				logVerbose("LaunchPad up-to-date: deployed=%s latest=%s", dep, latest)
			}
		} else {
			logVerbose("LaunchPad latest sentinel '%s' for deployed=%s", latest, dep)
		}
	} else if verbose {
		logVerbose("LaunchPad sentinel or empty deployed value '%s'", strings.TrimSpace(m.LaunchPadDeployed()))
	}
	// SCON: deployed may be tag (with/without leading v) or Installed/Missing/Error
	if dep := strings.TrimSpace(m.SCONDeployed()); dep != "" && dep != "Missing" && dep != "Error" && dep != "Installed" && dep != "Unknown" {
		latest := strings.TrimSpace(m.SCONLatest())
		if latest != "" && latest != "Unknown" {
			dl := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(dep, "v"), "V"))
			ll := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(latest, "v"), "V"))
			if dl != ll {
				logVerbose("SCON mismatch: deployed=%s latest=%s (normalized deployed=%s latest=%s)", dep, latest, dl, ll)
				out = append(out, DeployTypeSCON)
			} else {
				logVerbose("SCON up-to-date: deployed=%s latest=%s", dep, latest)
			}
		} else {
			logVerbose("SCON latest sentinel '%s' for deployed=%s", latest, dep)
		}
	} else if verbose {
		logVerbose("SCON sentinel or empty deployed value '%s'", strings.TrimSpace(m.SCONDeployed()))
	}

	// Include Servers deployment when any component needs update so server copies reflect updated binaries/plugins.
	if len(out) > 0 {
		out = append(out, DeployTypeServers)
		logVerbose("Servers added due to component updates: %v", out)
	}
	return out
}

// DeployTypes performs sequential deployment of the specified component types under
// a single deployment lock, aggregating errors. Progress reporting for each
// component is preserved via the existing runDeploy logic.
func (m *Manager) DeployTypes(types []DeployType) error {
	if len(types) == 0 {
		return nil
	}
	if err := m.beginDeploy(); err != nil {
		m.Log.Write(err.Error())
		return err
	}
	defer m.finishDeploy()

	start := time.Now()
	m.Log.Write(fmt.Sprintf("Selective deployment started (%v)", types))

	var aggErrs []string
	for _, dt := range types {
		if err := m.runDeploy(dt); err != nil {
			// runDeploy already logs component-specific errors; collect summary
			aggErrs = append(aggErrs, fmt.Sprintf("%s: %v", dt, err))
		}
	}

	// Aggregate errors for GetDeployErrors consumers.
	m.deployMu.Lock()
	if len(aggErrs) > 0 {
		m.DeployErrors = append([]string(nil), aggErrs...)
	} else {
		m.DeployErrors = nil
	}
	m.deployMu.Unlock()

	dur := time.Since(start)
	if len(aggErrs) > 0 {
		combined := errors.New(strings.Join(aggErrs, "; "))
		m.Log.Write(fmt.Sprintf("Selective deployment completed with errors in %s", dur))
		return combined
	}
	m.Log.Write(fmt.Sprintf("Selective deployment completed successfully in %s", dur))
	return nil
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

func (m *Manager) BuildTime() string {
	return "dev"
}

func (m *Manager) safeLog(message string) {
	if m.Log != nil {
		m.Log.Write(message)
	}
}

func (m *Manager) startLogs() {
	if err := os.MkdirAll(m.Paths.LogsDir(), 0o755); err != nil {
		m.safeLog(fmt.Sprintf("Unable to create logs directory %s: %v", m.Paths.LogsDir(), err))
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
	// Perform best-effort process discovery before per-server initialization when detached mode is enabled.
	var discovered map[int]int
	if m.DetachedServers {
		discovered = discoverRunningServerPIDs(m.Paths, m.WindowsDiscoveryWMIEnabled, func(msg string) { m.safeLog(msg) })
		if len(discovered) > 0 {
			m.safeLog(fmt.Sprintf("Process discovery found running detached servers: %v", discovered))
		} else {
			m.safeLog("Process discovery found no running detached server processes")
		}
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

		// Apply legacy-safe defaults for notification prefs: if fields are all zero-values,
		// prefer manager defaults and enable notifications.
		if !srv.NotifyUseManagerDefaults && strings.TrimSpace(srv.DiscordWebhook) == "" && !srv.NotifyEnable && !srv.NotifyOnStart && !srv.NotifyOnStopping && !srv.NotifyOnStopped && !srv.NotifyOnRestart && !srv.NotifyOnUpdateStarted && !srv.NotifyOnUpdateCompleted && !srv.NotifyOnUpdateFailed {
			srv.NotifyUseManagerDefaults = true
			srv.NotifyEnable = true
		}
		// No explicit templates/colors -> leave empty to inherit.

		// Attach via process discovery if detached mode enabled
		if m.DetachedServers {
			if pid, ok := discovered[srv.ID]; ok && pid > 0 && models.IsPidAlive(pid) {
				srv.AttachToRunning(pid)
				m.safeLog(fmt.Sprintf("Attached detached server %s (ID:%d) PID %d", srv.Name, srv.ID, pid))
				if m.OnServerAttached != nil {
					m.OnServerAttached(srv)
				}
			}
		}
	}
}

func (m *Manager) bootstrapDefaultConfig(configPath, rootPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("config path cannot be empty")
	}
	if strings.TrimSpace(rootPath) == "" {
		return fmt.Errorf("root path cannot be empty")
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to ensure config directory: %w", err)
	}

	m.ConfigFile = configPath
	m.Paths = utils.NewPaths(rootPath)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal default configuration: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write default configuration: %w", err)
	}

	return nil
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// load reads configuration from disk and rebuilds in-memory state.
func (m *Manager) load() (bool, error) {
	data, err := os.ReadFile(m.ConfigFile)
	if err != nil {
		return false, fmt.Errorf("configuration file not found: %w", err)
	}

	// Preserve previously persisted active state for backward compatibility.
	var wasActive bool
	if len(data) > 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err == nil {
			if activeRaw, ok := raw["active"]; ok {
				_ = json.Unmarshal(activeRaw, &wasActive)
			}
		}
	}

	// Create a temporary struct to unmarshal into, preserving existing Paths.
	temp := &Manager{}
	if err := json.Unmarshal(data, temp); err != nil {
		return false, fmt.Errorf("error parsing configuration: %w", err)
	}

	// Copy fields from loaded config
	m.SteamID = temp.SteamID
	m.SavedPath = temp.SavedPath
	m.Port = temp.Port
	m.Language = strings.TrimSpace(temp.Language)
	m.Servers = temp.Servers
	m.UpdateTime = temp.UpdateTime
	m.StartupUpdate = temp.StartupUpdate
	m.DetachedServers = temp.DetachedServers
	m.DetachedServers = temp.DetachedServers
	m.TLSEnabled = temp.TLSEnabled
	m.TLSCertPath = strings.TrimSpace(temp.TLSCertPath)
	m.TLSKeyPath = strings.TrimSpace(temp.TLSKeyPath)
	m.TrayEnabled = temp.TrayEnabled
	m.AutoPortForwardManager = temp.AutoPortForwardManager
	// Web, security, and logging behavior
	m.VerboseHTTP = temp.VerboseHTTP
	m.VerboseUpdate = temp.VerboseUpdate
	m.JWTSecret = strings.TrimSpace(temp.JWTSecret)
	m.CookieForceSecure = temp.CookieForceSecure
	m.CookieSameSite = strings.TrimSpace(temp.CookieSameSite)
	m.AllowIFrame = temp.AllowIFrame
	m.WindowsDiscoveryWMIEnabled = temp.WindowsDiscoveryWMIEnabled
	// SCON overrides
	m.SCONRepoOverride = strings.TrimSpace(temp.SCONRepoOverride)
	m.SCONURLLinuxOverride = strings.TrimSpace(temp.SCONURLLinuxOverride)
	m.SCONURLWindowsOverride = strings.TrimSpace(temp.SCONURLWindowsOverride)
	// Discord integration fields
	m.DiscordDefaultWebhook = strings.TrimSpace(temp.DiscordDefaultWebhook)
	// Notification preferences (manager defaults)
	m.NotifyEnableDeploy = temp.NotifyEnableDeploy
	m.NotifyEnableServer = temp.NotifyEnableServer
	m.NotifyOnStart = temp.NotifyOnStart
	m.NotifyOnStopping = temp.NotifyOnStopping
	m.NotifyOnStopped = temp.NotifyOnStopped
	m.NotifyOnRestart = temp.NotifyOnRestart
	m.NotifyOnUpdateStarted = temp.NotifyOnUpdateStarted
	m.NotifyOnUpdateCompleted = temp.NotifyOnUpdateCompleted
	m.NotifyOnUpdateFailed = temp.NotifyOnUpdateFailed
	// Message templates (empty -> keep existing defaults)
	if strings.TrimSpace(temp.NotifyMsgStart) != "" { m.NotifyMsgStart = strings.TrimSpace(temp.NotifyMsgStart) }
	if strings.TrimSpace(temp.NotifyMsgStopping) != "" { m.NotifyMsgStopping = strings.TrimSpace(temp.NotifyMsgStopping) }
	if strings.TrimSpace(temp.NotifyMsgStopped) != "" { m.NotifyMsgStopped = strings.TrimSpace(temp.NotifyMsgStopped) }
	if strings.TrimSpace(temp.NotifyMsgRestart) != "" { m.NotifyMsgRestart = strings.TrimSpace(temp.NotifyMsgRestart) }
	if strings.TrimSpace(temp.NotifyMsgUpdateStarted) != "" { m.NotifyMsgUpdateStarted = strings.TrimSpace(temp.NotifyMsgUpdateStarted) }
	if strings.TrimSpace(temp.NotifyMsgUpdateCompleted) != "" { m.NotifyMsgUpdateCompleted = strings.TrimSpace(temp.NotifyMsgUpdateCompleted) }
	if strings.TrimSpace(temp.NotifyMsgUpdateFailed) != "" { m.NotifyMsgUpdateFailed = strings.TrimSpace(temp.NotifyMsgUpdateFailed) }
	// Colors (validate basic format #RRGGBB)
	validColor := func(v string) bool { v = strings.TrimSpace(v); return len(v) == 7 && strings.HasPrefix(v, "#") }
	if validColor(temp.NotifyColorStart) { m.NotifyColorStart = strings.TrimSpace(temp.NotifyColorStart) }
	if validColor(temp.NotifyColorStopping) { m.NotifyColorStopping = strings.TrimSpace(temp.NotifyColorStopping) }
	if validColor(temp.NotifyColorStopped) { m.NotifyColorStopped = strings.TrimSpace(temp.NotifyColorStopped) }
	if validColor(temp.NotifyColorRestart) { m.NotifyColorRestart = strings.TrimSpace(temp.NotifyColorRestart) }
	if validColor(temp.NotifyColorUpdateStarted) { m.NotifyColorUpdateStarted = strings.TrimSpace(temp.NotifyColorUpdateStarted) }
	if validColor(temp.NotifyColorUpdateCompleted) { m.NotifyColorUpdateCompleted = strings.TrimSpace(temp.NotifyColorUpdateCompleted) }
	if validColor(temp.NotifyColorUpdateFailed) { m.NotifyColorUpdateFailed = strings.TrimSpace(temp.NotifyColorUpdateFailed) }
	// Deploy templates/colors (only override when provided & valid)
	if strings.TrimSpace(temp.NotifyMsgDeployStarted) != "" { m.NotifyMsgDeployStarted = strings.TrimSpace(temp.NotifyMsgDeployStarted) }
	if strings.TrimSpace(temp.NotifyMsgDeployCompleted) != "" { m.NotifyMsgDeployCompleted = strings.TrimSpace(temp.NotifyMsgDeployCompleted) }
	if strings.TrimSpace(temp.NotifyMsgDeployCompletedError) != "" { m.NotifyMsgDeployCompletedError = strings.TrimSpace(temp.NotifyMsgDeployCompletedError) }
	if validColor(temp.NotifyColorDeployStarted) { m.NotifyColorDeployStarted = strings.TrimSpace(temp.NotifyColorDeployStarted) }
	if validColor(temp.NotifyColorDeployCompleted) { m.NotifyColorDeployCompleted = strings.TrimSpace(temp.NotifyColorDeployCompleted) }
	if validColor(temp.NotifyColorDeployCompletedError) { m.NotifyColorDeployCompletedError = strings.TrimSpace(temp.NotifyColorDeployCompletedError) }
	// (Legacy removed) Bug report webhook field load-time override removed.
	// Default false when missing (zero value already false). Propagate to servers.
	for _, srv := range m.Servers {
		if srv != nil {
			srv.Detached = m.DetachedServers
		}
	}

	// Update fields from temp, but preserve existing Paths if temp.Paths is nil
	if temp.Paths != nil && temp.Paths.RootPath != "" {
		m.Paths = temp.Paths
	}

	m.Updating = false
	m.Active = true

	return wasActive, nil
}

// --- Manager Port Forwarding (TCP) ---
// managePortForwarding creates and periodically refreshes a TCP port mapping for the
// manager's HTTP(S) port while the process is running and the feature is enabled.
// It exits when pfStop is closed or the feature is disabled.
func (m *Manager) managePortForwarding() {
	if m == nil || m.Port <= 0 {
		return
	}
	// Initial attempt
	m.refreshManagerPortMapping()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Periodic refresh
			if !m.AutoPortForwardManager {
				return
			}
			m.refreshManagerPortMapping()
		case <-m.pfStop:
			return
		default:
			time.Sleep(2 * time.Second)
		}
	}
}

// refreshManagerPortMapping attempts to (re)create the TCP mapping and updates transient status fields.
func (m *Manager) refreshManagerPortMapping() {
	ctx := context.Background()
	externalPort, err := utils.AddOrRefreshMapping(ctx, "tcp", m.Port, "SDSM Manager", 10*time.Minute)
	if err != nil {
		if m.Log != nil {
			m.Log.Write("Manager port forward attempt failed: " + err.Error())
		}
		m.ManagerPortForwardActive = false
		m.ManagerPortForwardLastError = err.Error()
		return
	}
	m.ManagerPortForwardActive = true
	m.ManagerPortForwardExternalPort = externalPort
	m.ManagerPortForwardLastError = ""
	if m.Log != nil {
		m.Log.Write(fmt.Sprintf("Manager port forward active: internal TCP %d -> external TCP %d", m.Port, externalPort))
	}
}

// deleteManagerPortMapping removes the TCP mapping (best-effort); errors are logged but ignored.
func (m *Manager) deleteManagerPortMapping() {
	ctx := context.Background()
	if err := utils.DeleteMapping(ctx, "tcp", m.Port); err != nil {
		if m.Log != nil {
			m.Log.Write("Manager port forward removal failed: " + err.Error())
		}
		return
	}
	if m.Log != nil {
		m.Log.Write("Manager port forward mapping removed")
	}
	m.ManagerPortForwardActive = false
}

// StartManagerPortForwarding begins the background TCP mapping refresh loop when enabled.
func (m *Manager) StartManagerPortForwarding() {
	if m == nil || !m.AutoPortForwardManager || m.Port <= 0 {
		return
	}
	if m.pfStop != nil {
		// Already running
		return
	}
	m.pfStop = make(chan struct{})
	go m.managePortForwarding()
}

// StopManagerPortForwarding stops the background loop (if running) and deletes the mapping.
func (m *Manager) StopManagerPortForwarding() {
	if m == nil {
		return
	}
	if m.pfStop != nil {
		close(m.pfStop)
		m.pfStop = nil
	}
	// Best-effort cleanup
	m.deleteManagerPortMapping()
}

func (m *Manager) Save() {
	if m.ConfigFile == "" {
		m.Log.Write("No configuration file found. Please specify a configuration file path with --config.")
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

// InstallTLSFromSources copies provided certificate/key sources into a
// managed relative directory under the SDSM root ("certs/") and returns the
// relative paths to be persisted in configuration. Empty inputs are ignored
// and return empty outputs respectively. On error, no best-effort partial
// state is persisted by this method; callers should decide how to proceed.
func (m *Manager) InstallTLSFromSources(certSrc, keySrc string) (string, string, error) {
	// Validate provided sources before copying; collect errors but attempt both so user sees aggregated issues.
	validateCert := func(path string) error {
		if strings.TrimSpace(path) == "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read certificate: %w", err)
		}
		block, _ := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" {
			return fmt.Errorf("invalid PEM certificate (expected CERTIFICATE block)")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}
		// Basic freshness checks
		now := time.Now()
		if now.Before(cert.NotBefore) {
			return fmt.Errorf("certificate not yet valid (starts %s)", cert.NotBefore.Format(time.RFC3339))
		}
		if now.After(cert.NotAfter) {
			return fmt.Errorf("certificate expired %s", cert.NotAfter.Format(time.RFC3339))
		}
		return nil
	}
	validateKey := func(path string) (any, error) {
		if strings.TrimSpace(path) == "" {
			return nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read key: %w", err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("invalid PEM key (no block found)")
		}
		var key any
		switch block.Type {
		case "RSA PRIVATE KEY":
			k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse RSA key: %w", err)
			}
			if k.N.BitLen() < 2048 {
				return nil, fmt.Errorf("RSA key too small (<2048 bits)")
			}
			key = k
		case "EC PRIVATE KEY":
			k, err := x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse EC key: %w", err)
			}
			key = k
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse PKCS8 key: %w", err)
			}
			key = k
		default:
			return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
		}
		return key, nil
	}

	var preErrs []string
	if err := validateCert(certSrc); err != nil {
		preErrs = append(preErrs, err.Error())
	}
	privKey, keyErr := validateKey(keySrc)
	if keyErr != nil {
		preErrs = append(preErrs, keyErr.Error())
	}
	// If both parsed, perform public key match check (best-effort)
	if privKey != nil && certSrc != "" && keySrc != "" && len(preErrs) == 0 {
		// Re-read cert to obtain parsed object (already parsed earlier)
		// We need the cert's public key to compare types; size already verified for RSA.
		certData, _ := os.ReadFile(certSrc)
		block, _ := pem.Decode(certData)
		if block != nil && block.Type == "CERTIFICATE" {
			cert, _ := x509.ParseCertificate(block.Bytes)
			if cert != nil {
				pub := cert.PublicKey
				match := false
				switch pk := privKey.(type) {
				case *rsa.PrivateKey:
					if rsaPub, ok := pub.(*rsa.PublicKey); ok && rsaPub.N.Cmp(pk.N) == 0 {
						match = true
					}
				case *ecdsa.PrivateKey:
					if ecPub, ok := pub.(*ecdsa.PublicKey); ok && pk.X.Cmp(ecPub.X) == 0 && pk.Y.Cmp(ecPub.Y) == 0 {
						match = true
					}
				default:
					// Unsupported key type for match; do not fail.
					match = true
				}
				if !match {
					preErrs = append(preErrs, "certificate public key does not match private key")
				}
			}
		}
	}
	if len(preErrs) > 0 {
		return "", "", errors.New(strings.Join(preErrs, "; "))
	}
	// Normalize inputs
	certSrc = strings.TrimSpace(certSrc)
	keySrc = strings.TrimSpace(keySrc)

	// Ensure destination directory
	certDir := filepath.Join(m.Paths.RootPath, "certs")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return "", "", fmt.Errorf("failed to ensure certs directory: %w", err)
	}

	copyOne := func(src string, base string) (string, error) {
		if src == "" {
			return "", nil
		}
		// Validate source exists and is a regular file
		info, err := os.Stat(src)
		if err != nil {
			return "", fmt.Errorf("source not accessible: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("source is a directory: %s", src)
		}
		// Destination path under root/certs
		destAbs := filepath.Join(certDir, base)
		// If already at destination, return relative path directly
		if samePath(src, destAbs) {
			return filepath.ToSlash(filepath.Join("certs", base)), nil
		}
		// Copy file contents
		in, err := os.Open(src)
		if err != nil {
			return "", fmt.Errorf("failed to open source: %w", err)
		}
		defer in.Close()

		// Write to temp then atomically replace to avoid partial writes
		tmp, err := os.CreateTemp(certDir, base+".*.tmp")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		_, cpErr := io.Copy(tmp, in)
		closeErr := tmp.Close()
		if cpErr != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("copy failed: %w", cpErr)
		}
		if closeErr != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("failed to finalize temp file: %w", closeErr)
		}
		// Set conservative permissions: certificate world-readable, key restricted (non-Windows)
		perm := 0o644
		if base == "sdsm.key" && runtime.GOOS != "windows" {
			perm = 0o600
		}
		_ = os.Chmod(tmpPath, fs.FileMode(perm))
		if err := os.Rename(tmpPath, destAbs); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("failed to move into place: %w", err)
		}
		// Ensure final permissions as well
		_ = os.Chmod(destAbs, fs.FileMode(perm))
		return filepath.ToSlash(filepath.Join("certs", base)), nil
	}

	var certRel, keyRel string
	var errs []string
	if rel, err := copyOne(certSrc, "sdsm.crt"); err != nil {
		errs = append(errs, fmt.Sprintf("cert: %v", err))
	} else {
		certRel = rel
	}
	if rel, err := copyOne(keySrc, "sdsm.key"); err != nil {
		errs = append(errs, fmt.Sprintf("key: %v", err))
	} else {
		keyRel = rel
	}
	if len(errs) > 0 {
		return certRel, keyRel, errors.New(strings.Join(errs, "; "))
	}
	return certRel, keyRel, nil
}

// samePath returns true if two paths refer to the same location after evaluation.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ar, _ := filepath.EvalSymlinks(a)
	br, _ := filepath.EvalSymlinks(b)
	if ar == "" {
		ar = a
	}
	if br == "" {
		br = b
	}
	// Normalize case on Windows
	if runtime.GOOS == "windows" {
		ar = strings.ToLower(ar)
		br = strings.ToLower(br)
	}
	return ar == br
}

func (m *Manager) fetchSCONLatestVersion() (string, error) {
	s := steam.NewSteam(m.SteamID, m.UpdateLog, m.Paths)
	s.SetSCONOverrides(m.SCONRepoOverride, m.SCONURLLinuxOverride, m.SCONURLWindowsOverride)
	return s.GetSCONLatestTag()
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
	m.notifyDeployStart(deployType)

	m.Paths.DeployRoot(m.Log)

	s := steam.NewSteam(m.SteamID, m.UpdateLog, m.Paths)
	s.SetSCONOverrides(m.SCONRepoOverride, m.SCONURLLinuxOverride, m.SCONURLWindowsOverride)

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
		m.invalidateGameDataCaches(false)
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
		m.invalidateGameDataCaches(true)
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

	if deployType == DeployTypeSCON || deployType == DeployTypeAll {
		m.Log.Write("Beginning SCON deployment")
		m.progressBegin(DeployTypeSCON, "Queued")
		s.SetProgressReporter(string(DeployTypeSCON), m.progressReporter(DeployTypeSCON))
		if err := s.UpdateSCON(); err != nil {
			msg := fmt.Sprintf("SCON deployment failed: %v", err)
			errs = append(errs, msg)
			m.Log.Write(msg)
			if m.UpdateLog != nil {
				m.UpdateLog.Write(msg)
			}
			m.progressComplete(DeployTypeSCON, "Failed", err)
		} else {
			m.Log.Write("SCON deployment completed successfully")
			m.progressComplete(DeployTypeSCON, "Completed", nil)
		}
		s.SetProgressReporter("", nil)
		m.invalidateSCONVersionCache()
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
		m.notifyDeployComplete(deployType, duration, errs)
		return combined
	}

	m.Log.Write(fmt.Sprintf("Deployment (%s) completed successfully in %s", deployType, duration))
	if m.UpdateLog != nil {
		m.UpdateLog.Write(fmt.Sprintf("Deployment (%s) completed successfully in %s", deployType, duration))
	}
	m.notifyDeployComplete(deployType, duration, nil)
	m.Active = true
	return nil
}

func (m *Manager) GetDeployErrors() []string {
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	if len(m.DeployErrors) == 0 {
		return []string{}
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

func (m *Manager) ServerByID(id int) *models.Server {
	for _, s := range m.Servers {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *Manager) ServerCount() int {
	return len(m.Servers)
}

func (m *Manager) GetNextAvailablePort(start int) int {
	if start <= 0 {
		start = 27016
	}
	port := start
	for {
		available := true
		for _, s := range m.Servers {
			if s.Port == port {
				available = false
				break
			}
		}
		if available {
			return port
		}
		port++
	}
}

func (m *Manager) ServerCountActive() int {
	return m.ActiveServerCount()
}

func (m *Manager) GetTotalPlayers() int {
	total := 0
	for _, s := range m.Servers {
		total += s.ClientCount()
	}
	return total
}

func (m *Manager) GetMissingComponents() []string {
	return m.MissingComponents
}

func (m *Manager) IsServerNameAvailable(name string, excludeID int) bool {
	for _, s := range m.Servers {
		if s.ID == excludeID {
			continue
		}
		if strings.EqualFold(s.Name, name) {
			return false
		}
	}
	return true
}

func (m *Manager) IsPortAvailable(port int, excludeID int) bool {
	for _, s := range m.Servers {
		if s.ID == excludeID {
			continue
		}
		if s.Port == port {
			return false
		}
	}
	return true
}

func (m *Manager) ActiveServerCount() int {
	count := 0
	for _, s := range m.Servers {
		if s.IsRunning() {
			count++
		}
	}
	return count
}

func (m *Manager) Shutdown() {
	// Allow HTTP response to complete before shutting down
	time.Sleep(1000 * time.Millisecond)

	m.Log.Write("Shutting down all servers.")
	if m.DetachedServers {
		m.Log.Write("Shutdown requested (detached mode disabled for this operation). Stopping all servers.")
	}
	for _, srv := range m.Servers {
		if srv.IsRunning() {
			m.Log.Write(fmt.Sprintf("Stopping server: %s", srv.Name))
			srv.Stop()
		}
	}
	m.Log.Write("All servers stop sequence completed.")
	m.Save()
	m.Active = false
	m.Log.Write("SDSM is shutting down.")
	// Exit the application
	time.Sleep(1000 * time.Millisecond) // Give logs time to flush
	os.Exit(0)
}

// If stopServers is false, running servers are left alive; they were started in their own
// process group and will continue independently.
func (m *Manager) ExitDetached(stopServers bool) {
	// Allow HTTP response to complete before exiting
	time.Sleep(1000 * time.Millisecond)
	if stopServers {
		m.Log.Write("Detached shutdown: stopping all servers before exit.")
		for _, srv := range m.Servers {
			if srv.IsRunning() {
				m.Log.Write(fmt.Sprintf("Stopping server: %s", srv.Name))
				srv.Stop()
			}
		}
		m.Log.Write("All servers stopped.")
	} else {
		m.Log.Write("Detached shutdown: leaving servers running.")
	}
	m.Save()
	m.Active = false
	m.Log.Write("SDSM exiting now.")
	time.Sleep(1000 * time.Millisecond)
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
			m.safeLog(fmt.Sprintf("Unable to truncate log file %s: %v", path, err))
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

// GetWorlds returns the list of world display names for the release channel.
func (m *Manager) GetWorlds() []string {
	return m.GetWorldsByVersion(false)
}

// GetWorldsByVersion returns world display names for either release or beta.
func (m *Manager) GetWorldsByVersion(beta bool) []string {
	cache := m.worldDefinitionsCache(beta)
	if cache == nil || len(cache.definitions) == 0 {
		return []string{}
	}
	worlds := make([]string, 0, len(cache.definitions))
	for _, def := range cache.definitions {
		worlds = append(worlds, def.DisplayName)
	}
	return worlds
}

// GetWorldIDsByVersion returns the stable technical world IDs for either release or beta.
// These IDs are language-independent and safe for configuration and lookups.
func (m *Manager) GetWorldIDsByVersion(beta bool) []string {
	cache := m.worldDefinitionsCache(beta)
	if cache == nil || len(cache.definitions) == 0 {
		return []string{}
	}
	ids := make([]string, 0, len(cache.definitions))
	for _, def := range cache.definitions {
		if def.ID != "" {
			ids = append(ids, def.ID)
		}
	}
	sort.Strings(ids)
	return ids
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
// Deprecated helper no longer used; callers should prefer getGameDataPathForVersion.
// func (m *Manager) getGameDataPath() string { return m.getGameDataPathForVersion(false) }

// getGameDataPathForVersion resolves the best game data path for the selected channel.
func (m *Manager) getGameDataPathForVersion(beta bool) string {
	releasePath := filepath.Join(m.Paths.ReleaseDir(), "rocketstation_DedicatedServer_Data")
	betaPath := filepath.Join(m.Paths.BetaDir(), "rocketstation_DedicatedServer_Data")
	rootPath := filepath.Join(m.Paths.RootPath, "rocketstation_DedicatedServer_Data")

	var candidates []string
	if beta {
		candidates = append(candidates, betaPath, releasePath)
	} else {
		candidates = append(candidates, releasePath, betaPath)
	}
	candidates = append(candidates, rootPath)

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

// GetDifficulties returns difficulty IDs for the release channel.
func (m *Manager) GetDifficulties() []string {
	return m.GetDifficultiesForVersion(false)
}

// GetDifficultiesForVersion returns difficulty IDs for the specified channel.
func (m *Manager) GetDifficultiesForVersion(beta bool) []string {
	if difficulties := m.getDifficultiesFromXML(beta); len(difficulties) > 0 {
		return difficulties
	}
	return []string{}
}

// GetLanguages returns supported language names for the release channel.
func (m *Manager) GetLanguages() []string {
	// Use release channel languages via RocketStation scan
	return m.GetLanguagesForVersion(false)
}

// GetLanguagesForVersion returns available languages for release/beta
// independently using the RocketStation language scan.
func (m *Manager) GetLanguagesForVersion(beta bool) []string {
	// Cache TTL for languages: modest since language set rarely changes.
	const languagesCacheTTL = 10 * time.Minute
	m.languagesCacheMu.RLock()
	cache := m.languagesCache
	entry, has := cache[beta]
	m.languagesCacheMu.RUnlock()
	if has && len(entry.list) > 0 && time.Since(entry.cachedAt) < languagesCacheTTL {
		// Return a copy to avoid external mutation.
		copyList := make([]string, len(entry.list))
		copy(copyList, entry.list)
		return copyList
	}
	base := m.getGameDataPathForVersion(beta)
	langs, err := ScanLanguages(base)
	if err != nil || len(langs) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(langs))
	for _, l := range langs {
		name := strings.TrimSuffix(l.FileName, ".xml")
		if name != "" && !strings.Contains(name, "_") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	m.languagesCacheMu.Lock()
	if m.languagesCache == nil {
		m.languagesCache = make(map[bool]struct {
			list     []string
			cachedAt time.Time
		})
	}
	m.languagesCache[beta] = struct {
		list     []string
		cachedAt time.Time
	}{list: out, cachedAt: time.Now()}
	m.languagesCacheMu.Unlock()
	copyList := make([]string, len(out))
	copy(copyList, out)
	return copyList
}

// WorldInfo contains localized world name, description, and the relative
// image path for the world's map image (under StreamingAssets).
type WorldInfo struct {
	ID          string
	Name        string
	Description string
	// Image is the relative path (under StreamingAssets) to the planet image for this world,
	// as resolved by the RocketStation parser (e.g. Images/SpaceMapImages/Planets/StatMoon.png).
	// It is exposed for templates that wish to reference it directly.
	Image string
}

// LocationInfo contains localized start location information for a given world.
type LocationInfo struct {
	ID          string
	Name        string
	Description string
}

// ConditionInfo contains localized start condition information for a given world.
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
const rocketStationLatestCacheTTL = time.Minute
const bepInExVersionFile = "bepinex.version"
const sconVersionFile = "scon.version"
const sconLatestCacheTTL = 30 * time.Minute

type worldDefinition struct {
	Directory           string
	ID                  string
	DisplayName         string
	Priority            int
	Root                string
	NameFallback        string
	DescriptionFallback string
	StartConditions     []RSStartCondition
	StartLocations      []RSStartLocation
	Image               string
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

// start locations are now part of cached world definitions; no separate cache needed

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

// worldDefinitionsCacheFor returns a cached worldDefinitionCache for the given
// channel and language. It builds and stores a new cache when missing or stale.
func (m *Manager) worldDefinitionsCacheFor(beta bool, language string) *worldDefinitionCache {
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = strings.TrimSpace(m.Language)
	}
	if lang == "" {
		lang = "english"
	}
	key := strings.ToLower(lang)

	// Fast path: read lock lookup
	m.worldIndexByLangMu.RLock()
	byLang := m.worldIndexByLang[beta]
	var cached *worldDefinitionCache
	if byLang != nil {
		cached = byLang[key]
	}
	m.worldIndexByLangMu.RUnlock()

	if cached != nil {
		return cached
	}

	// Build fresh and cache
	built := m.buildWorldDefinitionCacheFor(beta, key)
	m.worldIndexByLangMu.Lock()
	if m.worldIndexByLang == nil {
		m.worldIndexByLang = make(map[bool]map[string]*worldDefinitionCache)
	}
	if m.worldIndexByLang[beta] == nil {
		m.worldIndexByLang[beta] = make(map[string]*worldDefinitionCache)
	}
	m.worldIndexByLang[beta][key] = built
	m.worldIndexByLangMu.Unlock()
	return built
}

// buildWorldDefinitionCacheFor builds an in-memory world definition cache for a specific
// game channel and language without mutating any global state. This is safe to call for
// per-request language overrides.
func (m *Manager) buildWorldDefinitionCacheFor(beta bool, language string) *worldDefinitionCache {
	cache := &worldDefinitionCache{
		byCanonical: make(map[string]worldDefinition),
	}

	// Only use a single install path based on the selected channel (server configuration)
	paths := []string{m.getGameDataPathForVersion(beta)}
	uniquePaths := uniqueStrings(paths)
	byDisplay := make(map[string]worldDefinition)

	// Determine language file to use for translations
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = strings.TrimSpace(m.Language)
	}
	if lang == "" {
		lang = "english"
	}
	langFile := lang + ".xml"

	for _, root := range uniquePaths {
		if root == "" {
			continue
		}
		worlds, err := ScanWorldDefinitions(root, langFile)
		if err != nil || len(worlds) == 0 {
			continue
		}
		for _, w := range worlds {
			// Skip hidden worlds to match previous behavior
			if w.Hidden {
				continue
			}
			display := w.Name
			if strings.TrimSpace(display) == "" {
				display = w.ID
			}
			def := worldDefinition{
				Directory:           w.Directory,
				ID:                  w.ID,
				DisplayName:         strings.TrimSpace(display),
				Priority:            w.Priority,
				Root:                root,
				NameFallback:        strings.TrimSpace(w.Name),
				DescriptionFallback: strings.TrimSpace(w.Description),
				StartConditions:     append([]RSStartCondition(nil), w.StartConditions...),
				Image:               strings.TrimSpace(w.Image),
			}
			// Copy start locations
			if len(w.StartLocations) > 0 {
				def.StartLocations = append([]RSStartLocation(nil), w.StartLocations...)
			}
			if def.ID == "" {
				def.ID = def.Directory
			}
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

// buildWorldDefinitionCache builds a cache based on the manager's current language setting
// and stores it under the beta/non-beta key for reuse across requests.
func (m *Manager) buildWorldDefinitionCache(beta bool) *worldDefinitionCache {
	return m.buildWorldDefinitionCacheFor(beta, m.Language)
}

func (m *Manager) getDifficultiesFromXML(beta bool) []string {
	base := m.getGameDataPathForVersion(beta)
	language := m.Language
	if strings.TrimSpace(language) == "" {
		language = "english"
	}
	if diffs, err := ScanDifficulties(base, language+".xml"); err == nil && len(diffs) > 0 {
		ids := make([]string, 0, len(diffs))
		for _, d := range diffs {
			if d.ID != "" {
				ids = append(ids, d.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	return []string{}
}

// GetWorldImage returns the PNG bytes for the planet image that matches the world ID.
func (m *Manager) GetWorldImage(worldId string, beta bool) ([]byte, error) {
	base := m.getGameDataPathForVersion(beta)
	return GetWorldImage(base, worldId)
}

func (m *Manager) resolveWorldTechnicalID(worldID string, beta bool) string {
	canonical := canonicalWorldIdentifier(worldID)
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			// Prefer stable world ID for linkage and server args
			if strings.TrimSpace(def.ID) != "" {
				return def.ID
			}
			if strings.TrimSpace(def.Directory) != "" {
				return def.Directory
			}
		}
	}
	return worldID
}

// GetWorldInfo returns the localized name/description and image path for a world.
func (m *Manager) GetWorldInfo(worldID string, beta bool) WorldInfo {
	canonical := canonicalWorldIdentifier(worldID)
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			name := strings.TrimSpace(def.DisplayName)
			if name == "" {
				name = def.NameFallback
			}
			if strings.TrimSpace(name) == "" {
				name = worldID
			}
			desc := strings.TrimSpace(def.DescriptionFallback)
			return WorldInfo{ID: worldID, Name: name, Description: desc, Image: def.Image}
		}
	}
	return WorldInfo{ID: worldID, Name: worldID, Description: "", Image: ""}
}

// GetStartLocationsForWorld returns all start locations available for a specific world
// GetStartLocationsForWorld returns all start locations for a world (release).
func (m *Manager) GetStartLocationsForWorld(worldID string) []LocationInfo {
	return m.GetStartLocationsForWorldVersion(worldID, false)
}

// GetStartLocationsForWorldVersion returns start locations for a world by channel.
func (m *Manager) GetStartLocationsForWorldVersion(worldID string, beta bool) []LocationInfo {
	canonical := canonicalWorldIdentifier(worldID)
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			out := make([]LocationInfo, 0, len(def.StartLocations))
			for _, l := range def.StartLocations {
				name := strings.TrimSpace(l.Name)
				if name == "" {
					name = l.ID
				}
				out = append(out, LocationInfo{ID: l.ID, Name: name, Description: l.Description})
			}
			return out
		}
	}
	return []LocationInfo{}
}

// GetStartConditionsForWorld returns all start conditions available for a specific world
// GetStartConditionsForWorld returns all start conditions for a world (release).
func (m *Manager) GetStartConditionsForWorld(worldID string) []ConditionInfo {
	return m.GetStartConditionsForWorldVersion(worldID, false)
}

// GetStartConditionsForWorldVersion returns start conditions for a world by channel.
func (m *Manager) GetStartConditionsForWorldVersion(worldID string, beta bool) []ConditionInfo {
	if cache := m.worldDefinitionsCache(beta); cache != nil {
		canonical := canonicalWorldIdentifier(worldID)
		if def, ok := cache.byCanonical[canonical]; ok {
			seen := make(map[string]bool)
			out := make([]ConditionInfo, 0, len(def.StartConditions))
			for _, sc := range def.StartConditions {
				if sc.ID == "" || seen[sc.ID] {
					continue
				}
				seen[sc.ID] = true
				name := strings.TrimSpace(sc.DisplayName)
				if name == "" {
					name = sc.ID
				}
				out = append(out, ConditionInfo{ID: sc.ID, Name: name, Description: sc.Description})
			}
			return out
		}
	}
	return []ConditionInfo{}
}

// Language-aware variants used for per-server localization
func (m *Manager) GetWorldsByVersionWithLanguage(beta bool, language string) []string {
	cache := m.worldDefinitionsCacheFor(beta, language)
	if cache == nil || len(cache.definitions) == 0 {
		return []string{}
	}
	worlds := make([]string, 0, len(cache.definitions))
	for _, def := range cache.definitions {
		worlds = append(worlds, def.DisplayName)
	}
	return worlds
}

func (m *Manager) GetWorldInfoWithLanguage(worldID string, beta bool, language string) WorldInfo {
	canonical := canonicalWorldIdentifier(worldID)
	cache := m.worldDefinitionsCacheFor(beta, language)
	if cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			name := strings.TrimSpace(def.DisplayName)
			if name == "" {
				name = def.NameFallback
			}
			if strings.TrimSpace(name) == "" {
				name = worldID
			}
			desc := strings.TrimSpace(def.DescriptionFallback)
			return WorldInfo{ID: worldID, Name: name, Description: desc, Image: def.Image}
		}
	}
	return WorldInfo{ID: worldID, Name: worldID, Description: "", Image: ""}
}

func (m *Manager) GetStartLocationsForWorldVersionWithLanguage(worldID string, beta bool, language string) []LocationInfo {
	canonical := canonicalWorldIdentifier(worldID)
	cache := m.worldDefinitionsCacheFor(beta, language)
	if cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			out := make([]LocationInfo, 0, len(def.StartLocations))
			for _, l := range def.StartLocations {
				name := strings.TrimSpace(l.Name)
				if name == "" {
					name = l.ID
				}
				out = append(out, LocationInfo{ID: l.ID, Name: name, Description: l.Description})
			}
			return out
		}
	}
	return []LocationInfo{}
}

func (m *Manager) GetStartConditionsForWorldVersionWithLanguage(worldID string, beta bool, language string) []ConditionInfo {
	canonical := canonicalWorldIdentifier(worldID)
	cache := m.worldDefinitionsCacheFor(beta, language)
	if cache != nil {
		if def, ok := cache.byCanonical[canonical]; ok {
			seen := make(map[string]bool)
			out := make([]ConditionInfo, 0, len(def.StartConditions))
			for _, sc := range def.StartConditions {
				if sc.ID == "" || seen[sc.ID] {
					continue
				}
				seen[sc.ID] = true
				name := strings.TrimSpace(sc.DisplayName)
				if name == "" {
					name = sc.ID
				}
				out = append(out, ConditionInfo{ID: sc.ID, Name: name, Description: sc.Description})
			}
			return out
		}
	}
	return []ConditionInfo{}
}

func (m *Manager) GetDifficultiesForVersionWithLanguage(beta bool, language string) []string {
	base := m.getGameDataPathForVersion(beta)
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = strings.TrimSpace(m.Language)
	}
	if lang == "" {
		lang = "english"
	}
	key := strings.ToLower(lang)

	// Cached lookup
	m.diffByLangMu.RLock()
	if m.diffByLang != nil && m.diffByLang[beta] != nil {
		if entry, ok := m.diffByLang[beta][key]; ok {
			if len(entry.list) > 0 {
				out := make([]string, len(entry.list))
				copy(out, entry.list)
				m.diffByLangMu.RUnlock()
				return out
			}
		}
	}
	m.diffByLangMu.RUnlock()

	diffs, err := ScanDifficulties(base, lang+".xml")
	if err != nil || len(diffs) == 0 {
		return []string{}
	}
	ids := make([]string, 0, len(diffs))
	for _, d := range diffs {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)

	// Store in cache
	m.diffByLangMu.Lock()
	if m.diffByLang == nil {
		m.diffByLang = make(map[bool]map[string]struct {
			list        []string
			generatedAt time.Time
		})
	}
	if m.diffByLang[beta] == nil {
		m.diffByLang[beta] = make(map[string]struct {
			list        []string
			generatedAt time.Time
		})
	}
	m.diffByLang[beta][key] = struct {
		list        []string
		generatedAt time.Time
	}{list: append([]string(nil), ids...), generatedAt: time.Now()}
	m.diffByLangMu.Unlock()

	return ids
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

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
	// Apply current manager-level detached behavior to new server instances.
	srv.Detached = m.DetachedServers

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
	m.releaseLatestMu.RLock()
	cached := m.releaseLatest
	cachedAt := m.releaseLatestAt
	m.releaseLatestMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < rocketStationLatestCacheTTL {
		return cached
	}

	rel, bet, err := m.fetchRocketStationLatestBuildIDs()
	if err != nil {
		if cached != "" {
			return cached
		}
		return "Unknown"
	}

	// Update both caches together
	m.releaseLatestMu.Lock()
	m.releaseLatest = rel
	m.releaseLatestAt = time.Now()
	m.releaseLatestMu.Unlock()

	m.betaLatestMu.Lock()
	m.betaLatest = bet
	m.betaLatestAt = time.Now()
	m.betaLatestMu.Unlock()

	return rel
}

func (m *Manager) BetaLatest() string {
	m.betaLatestMu.RLock()
	cached := m.betaLatest
	cachedAt := m.betaLatestAt
	m.betaLatestMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < rocketStationLatestCacheTTL {
		return cached
	}

	rel, bet, err := m.fetchRocketStationLatestBuildIDs()
	if err != nil {
		if cached != "" {
			return cached
		}
		return "Unknown"
	}

	// Update both caches together
	m.releaseLatestMu.Lock()
	m.releaseLatest = rel
	m.releaseLatestAt = time.Now()
	m.releaseLatestMu.Unlock()

	m.betaLatestMu.Lock()
	m.betaLatest = bet
	m.betaLatestAt = time.Now()
	m.betaLatestMu.Unlock()

	return bet
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

// invalidateSCONVersionCache clears cached SCON deployed/latest values
func (m *Manager) invalidateSCONVersionCache() {
	m.sconMu.Lock()
	m.sconVersion = ""
	m.sconChecked = time.Time{}
	m.sconMu.Unlock()

	m.sconLatestMu.Lock()
	m.sconLatest = ""
	m.sconLatestAt = time.Time{}
	m.sconLatestMu.Unlock()
}

// invalidateGameDataCaches clears any cached game data (worlds, languages, difficulties)
// so that subsequent requests recollect fresh data from disk after an update.
func (m *Manager) invalidateGameDataCaches(beta bool) {
	// Invalidate world definition cache for the specified channel
	m.worldIndexMu.Lock()
	if m.worldIndex != nil {
		delete(m.worldIndex, beta)
	}
	m.worldIndexMu.Unlock()

	// Invalidate per-language world caches for the specified channel
	m.worldIndexByLangMu.Lock()
	if m.worldIndexByLang != nil {
		delete(m.worldIndexByLang, beta)
	}
	m.worldIndexByLangMu.Unlock()

	// Invalidate per-language difficulties cache for the specified channel
	m.diffByLangMu.Lock()
	if m.diffByLang != nil {
		delete(m.diffByLang, beta)
	}
	m.diffByLangMu.Unlock()

	// Note: RS XML scanners (ScanWorldDefinitions/ScanDifficulties/ScanLanguages)
	// are currently invoked on-demand without an internal cache. If we add
	// explicit caching for them in the future, hook invalidation here as well.
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
	// There is no reliable public "latest" for SteamCMD version; use deployed as the effective latest
	return m.SteamCmdDeployed()
}

// fetchRocketStationLatestBuildIDs queries Steam for the latest public and beta build IDs
func (m *Manager) fetchRocketStationLatestBuildIDs() (string, string, error) {
	s := steam.NewSteam(m.SteamID, m.UpdateLog, m.Paths)
	s.SetSCONOverrides(m.SCONRepoOverride, m.SCONURLLinuxOverride, m.SCONURLWindowsOverride)
	versions, err := s.GetVersions()
	if err != nil {
		return "", "", err
	}
	if len(versions) < 2 {
		return "", "", fmt.Errorf("incomplete version data from Steam API")
	}
	rel := strings.TrimSpace(versions[0])
	bet := strings.TrimSpace(versions[1])
	if rel == "" && bet == "" {
		return "", "", fmt.Errorf("empty version data from Steam API")
	}
	return rel, bet, nil
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

// SCONLatest returns the latest tagged version for the SCON plugin from its GitHub repo.
// Repo can be overridden via configuration field SCONRepoOverride (format: owner/repo).
func (m *Manager) SCONLatest() string {
	m.sconLatestMu.RLock()
	cached := m.sconLatest
	cachedAt := m.sconLatestAt
	m.sconLatestMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < sconLatestCacheTTL {
		return cached
	}

	version, err := m.fetchSCONLatestVersion()
	if err != nil {
		m.safeLog(fmt.Sprintf("Failed to fetch latest SCON version: %v", err))
		if cached != "" {
			return cached
		}
		version = "Unknown"
	} else {
		m.safeLog(fmt.Sprintf("SCON latest version reported: %s", version))
	}

	m.sconLatestMu.Lock()
	m.sconLatest = version
	m.sconLatestAt = time.Now()
	m.sconLatestMu.Unlock()

	return version
}

func (m *Manager) SCONDeployed() string {
	m.sconMu.RLock()
	cached := m.sconVersion
	cachedAt := m.sconChecked
	m.sconMu.RUnlock()

	if cached != "" && time.Since(cachedAt) < sconVersionCacheTTL {
		return cached
	}

	version, err := m.fetchSCONDeployedVersion()
	if err != nil {
		result := "Error"
		if errors.Is(err, os.ErrNotExist) {
			result = "Missing"
		} else {
			m.safeLog(fmt.Sprintf("Failed to determine SCON deployed version: %v", err))
		}

		m.sconMu.Lock()
		m.sconVersion = result
		m.sconChecked = time.Now()
		m.sconMu.Unlock()
		return result
	}

	m.safeLog(fmt.Sprintf("SCON deployed version: %s", version))
	m.sconMu.Lock()
	m.sconVersion = version
	m.sconChecked = time.Now()
	m.sconMu.Unlock()

	return version
}

func (m *Manager) fetchSCONDeployedVersion() (string, error) {
	sconPath := m.Paths.SCONReleaseDir()
	if _, err := os.Stat(sconPath); err != nil {
		return "", err
	}

	versionFile := filepath.Join(sconPath, "version.txt")
	if _, err := os.Stat(versionFile); err != nil {
		return "", err
	}

	data, err := os.ReadFile(versionFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func (m *Manager) CheckMissingComponents() {
	m.MissingComponents = []string{}
	if m.SteamCmdDeployed() == "Missing" {
		m.MissingComponents = append(m.MissingComponents, "SteamCMD")
	}
	if m.ReleaseDeployed() == "Missing" && m.BetaDeployed() == "Missing" {
		m.MissingComponents = append(m.MissingComponents, "RocketStation Dedicated Server")
	}
	if len(m.MissingComponents) > 0 {
		m.NeedsUploadPrompt = true
	}
}

func (m *Manager) fetchSteamCmdVersion() (string, error) {
	if m.Paths == nil {
		m.Paths = utils.NewPaths("/tmp/sdsm")
	}

	steamCmdFile := "steamcmd.sh"
	if runtime.GOOS == "windows" {
		steamCmdFile = "steamcmd.exe"
	}

	steamCmdPath := filepath.Join(m.Paths.SteamDir(), steamCmdFile)
	if _, err := os.Stat(steamCmdPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	cmd := exec.Command(steamCmdPath, "+quit")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	matches := steamCmdVersionPattern.FindSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("unable to parse SteamCMD version")
	}

	return string(matches[1]), nil
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
	idx := bytes.Index(bytes.ToLower(data), bytes.ToLower(anchor))
	if idx < 0 {
		return ""
	}

	dataLen := len(data)
	searchStart := idx - 256
	if searchStart < 0 {
		searchStart = 0
	}
	if searchStart >= dataLen {
		searchStart = dataLen
	}
	
	searchEnd := idx + len(anchor) + 256
	if searchEnd > dataLen {
		searchEnd = dataLen
	}
	
	// Ensure valid slice bounds
	if searchStart >= searchEnd {
		return ""
	}

	region := data[searchStart:searchEnd]
	if match := launchPadVersionPattern.Find(region); match != nil {
		return string(match)
	}

	return ""
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
	if len(matches) == 0 {
		return ""
	}

	var best string
	bestConf := 0

	for _, m := range matches {
		version := string(m)
		if !isLikelyBepInExVersion(version) {
			continue
		}
		conf := bepInExVersionConfidence(version)
		if conf > bestConf {
			best = version
			bestConf = conf
		}
	}

	return best
}

func isLikelyBepInExVersion(version string) bool {
	if version == "" {
		return false
	}
	major, ok := bepInExMajor(version)
	if !ok {
		return false
	}
	return major >= 5 && major <= 7
}

func bepInExVersionConfidence(version string) int {
	score := 0
	major, ok := bepInExMajor(version)
	if !ok {
		return 0
	}

	if major == 5 {
		score += 10
	} else if major == 6 {
		score += 8
	}

	parts := strings.Split(version, ".")
	if len(parts) == 4 {
		score += 5
	}

	return score
}

func bepInExMajor(version string) (int, bool) {
	if version == "" {
		return 0, false
	}
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
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

func (m *Manager) bepInExCandidateFiles() []string {
	if m.Paths == nil {
		return nil
	}
	dir := m.Paths.BepInExDir()
	return []string{
		filepath.Join(dir, "core", "BepInEx.dll"),
		filepath.Join(dir, "BepInEx.dll"),
		filepath.Join(dir, "core", "BepInEx.Preloader.dll"),
		filepath.Join(dir, "BepInEx.Preloader.dll"),
	}
}

