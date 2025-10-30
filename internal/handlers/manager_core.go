// Package handlers exposes HTTP handlers for the SDSM web UI and API.
package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"sdsm/internal/manager"
	"sdsm/internal/models"

	"github.com/gin-gonic/gin"
)

const updateLogFallback = "No update activity recorded yet."

// ManagerHandlers wires HTTP handlers to a Manager instance.
type ManagerHandlers struct {
	manager *manager.Manager
}

// NewManagerHandlers constructs a handler set bound to the provided Manager.
func NewManagerHandlers(mgr *manager.Manager) *ManagerHandlers {
	return &ManagerHandlers{manager: mgr}
}

func (h *ManagerHandlers) buildWorldSelectionData() (map[string][]string, map[string]map[string]gin.H) {
	worldLists := map[string][]string{
		"release": h.manager.GetWorldsByVersion(false),
		"beta":    h.manager.GetWorldsByVersion(true),
	}

	worldData := map[string]map[string]gin.H{
		"release": {},
		"beta":    {},
	}

	for _, world := range worldLists["release"] {
		worldData["release"][world] = gin.H{
			"locations":  h.manager.GetStartLocationsForWorldVersion(world, false),
			"conditions": h.manager.GetStartConditionsForWorldVersion(world, false),
		}
	}

	for _, world := range worldLists["beta"] {
		worldData["beta"][world] = gin.H{
			"locations":  h.manager.GetStartLocationsForWorldVersion(world, true),
			"conditions": h.manager.GetStartConditionsForWorldVersion(world, true),
		}
	}

	return worldLists, worldData
}

func (h *ManagerHandlers) renderServerPage(c *gin.Context, status int, s *models.Server, username interface{}, errMsg string) {
	// Refresh runtime flags so the template does not show stale state after restarts.
	s.IsRunning()

	worldLists, worldData := h.buildWorldSelectionData()
	worldInfo := h.manager.GetWorldInfo(s.World, s.Beta)

	payload := gin.H{
		"server":     s,
		"manager":    h.manager,
		"username":   username,
		"worldInfo":  worldInfo,
		"worldLists": worldLists,
		"worldData":  worldData,
		// Per-channel difficulties for live switching on the status page
		"release_difficulties": h.manager.GetDifficultiesForVersion(false),
		"beta_difficulties":    h.manager.GetDifficultiesForVersion(true),
		"serverPath":           h.manager.Paths.ServerDir(s.ID),
		"banned":               s.BannedEntries(),
	}

	if errMsg != "" {
		payload["error"] = errMsg
	}

	c.HTML(status, "server_status.html", payload)
}

func (h *ManagerHandlers) renderNewServerForm(c *gin.Context, status int, username interface{}, overrides gin.H) {
	worldLists, worldData := h.buildWorldSelectionData()

	defaultVersionKey := ""
	defaultWorld := ""
	defaultStartLocation := ""
	defaultStartCondition := ""
	defaultBetaValue := "false"

	if releaseWorlds := worldLists["release"]; len(releaseWorlds) > 0 {
		defaultWorld = releaseWorlds[0]
		defaultVersionKey = "release"
	} else if betaWorlds := worldLists["beta"]; len(betaWorlds) > 0 {
		defaultWorld = betaWorlds[0]
		defaultVersionKey = "beta"
		defaultBetaValue = "true"
	}

	if defaultVersionKey != "" && defaultWorld != "" {
		if versionedWorlds, ok := worldData[defaultVersionKey]; ok {
			if worldEntry, ok := versionedWorlds[defaultWorld]; ok {
				if locationsRaw, ok := worldEntry["locations"].([]manager.LocationInfo); ok && len(locationsRaw) > 0 {
					defaultStartLocation = locationsRaw[0].ID
				}
				if conditionsRaw, ok := worldEntry["conditions"].([]manager.ConditionInfo); ok && len(conditionsRaw) > 0 {
					defaultStartCondition = conditionsRaw[0].ID
				}
			}
		}
	}

	const defaultServerPort = 27016
	suggestedName := "My Stationeers Server"
	if count := h.manager.ServerCount(); count > 0 {
		suggestedName = fmt.Sprintf("My Stationeers Server %d", count+1)
	}

	// Determine default difficulty from release channel if available
	defaultDifficulty := ""
	if relDiffs := h.manager.GetDifficultiesForVersion(false); len(relDiffs) > 0 {
		defaultDifficulty = relDiffs[0]
	}

	formDefaults := gin.H{
		"name":                  suggestedName,
		"world":                 defaultWorld,
		"start_location":        defaultStartLocation,
		"start_condition":       defaultStartCondition,
		"difficulty":            defaultDifficulty,
		"port":                  fmt.Sprintf("%d", h.manager.GetNextAvailablePort(defaultServerPort)),
		"max_clients":           "10",
		"password":              "",
		"auth_secret":           "",
		"save_interval":         "300",
		"restart_delay_seconds": fmt.Sprintf("%d", models.DefaultRestartDelaySeconds),
		"beta":                  defaultBetaValue,
		"auto_start":            false,
		"auto_update":           false,
		"auto_save":             true,
		"auto_pause":            true,
		"server_visible":        true,
	}

	if overrides != nil {
		if existingForm, ok := overrides["form"].(gin.H); ok {
			for key, val := range existingForm {
				formDefaults[key] = val
			}
			delete(overrides, "form")
		}
	}

	if worldValue, ok := formDefaults["world"].(string); ok && worldValue == "" {
		formDefaults["world"] = defaultWorld
	}

	if locValue, ok := formDefaults["start_location"].(string); ok && locValue == "" {
		formDefaults["start_location"] = defaultStartLocation
	}

	if condValue, ok := formDefaults["start_condition"].(string); ok && condValue == "" {
		formDefaults["start_condition"] = defaultStartCondition
	}

	if betaValue, ok := formDefaults["beta"].(string); ok && betaValue == "" {
		formDefaults["beta"] = defaultBetaValue
	}

	payload := gin.H{
		"username":   username,
		"worldLists": worldLists,
		"worldData":  worldData,
		// Keep default difficulties for initial render (release by default)
		"difficulties":         h.manager.GetDifficulties(),
		"release_difficulties": h.manager.GetDifficultiesForVersion(false),
		"beta_difficulties":    h.manager.GetDifficultiesForVersion(true),
		"form":                 formDefaults,
	}

	for k, v := range overrides {
		payload[k] = v
	}

	c.HTML(status, "server_new.html", payload)
}

func (h *ManagerHandlers) startDeployAsync(deployType manager.DeployType) error {
	return h.manager.StartDeployAsync(deployType)
}

func (h *ManagerHandlers) startServerUpdateAsync(s *models.Server) {
	h.manager.ServerProgressBegin(s.ID, "Queued")
	s.SetProgressReporter(func(stage string, processed, total int64) {
		h.manager.ServerProgressUpdate(s.ID, stage, processed, total)
	})

	go func() {
		defer s.SetProgressReporter(nil)
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("server update panic: %v", r)
				if s.Logger != nil {
					s.Logger.Write(err.Error())
				}
				h.manager.ServerProgressComplete(s.ID, "Failed", err)
			}
		}()

		if err := s.Deploy(); err != nil {
			if s.Logger != nil {
				s.Logger.Write(fmt.Sprintf("Server update failed: %v", err))
			}
			h.manager.ServerProgressComplete(s.ID, "Failed", err)
			return
		}

		h.manager.ServerProgressComplete(s.ID, "Completed", nil)
	}()
}

// Home redirects to the login page (root entry point).
func (h *ManagerHandlers) Home(c *gin.Context) {
	c.Redirect(http.StatusFound, "/login")
}

// Frame renders the outer frame shell with server list and active status.
func (h *ManagerHandlers) Frame(c *gin.Context) {
	username, _ := c.Get("username")

	data := gin.H{
		"active":   h.manager.IsActive(),
		"servers":  h.manager.Servers,
		"username": username,
	}
	c.HTML(http.StatusOK, "frame.html", data)
}

// Dashboard renders the main dashboard with server cards for quick status.
func (h *ManagerHandlers) Dashboard(c *gin.Context) {
	username, _ := c.Get("username")

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"servers":  h.manager.Servers,
		"username": username,
	})
}

// ManagerGET renders the manager settings/status page with version info,
// missing component warnings, deploy state, and game-data scanner warnings.
func (h *ManagerHandlers) ManagerGET(c *gin.Context) {
	username, _ := c.Get("username")

	if !h.manager.IsUpdating() {
		h.manager.CheckMissingComponents()
	}

	missingComponents := h.manager.GetMissingComponents()
	deployErrors := h.manager.GetDeployErrors()
	needsSetup := h.manager.NeedsUploadPrompt
	setupInProgress := h.manager.SetupInProgress
	lastLogLine := h.manager.LastUpdateLogLine()

	// Build lightweight game-data warnings if scanners return empty sets
	relLangs := h.manager.GetLanguagesForVersion(false)
	betaLangs := h.manager.GetLanguagesForVersion(true)
	relWorlds := h.manager.GetWorldsByVersion(false)
	betaWorlds := h.manager.GetWorldsByVersion(true)
	relDiffs := h.manager.GetDifficultiesForVersion(false)
	betaDiffs := h.manager.GetDifficultiesForVersion(true)

	warnings := []string{}
	if len(relWorlds) == 0 {
		warnings = append(warnings, "No worlds found for Release channel.")
	}
	if len(betaWorlds) == 0 {
		warnings = append(warnings, "No worlds found for Beta channel.")
	}
	if len(relLangs) == 0 {
		warnings = append(warnings, "No languages found for Release channel.")
	}
	if len(betaLangs) == 0 {
		warnings = append(warnings, "No languages found for Beta channel.")
	}
	if len(relDiffs) == 0 {
		warnings = append(warnings, "No difficulties found for Release channel.")
	}
	if len(betaDiffs) == 0 {
		warnings = append(warnings, "No difficulties found for Beta channel.")
	}

	data := gin.H{
		"username":  username,
		"steam_id":  h.manager.SteamID,
		"root_path": h.manager.Paths.RootPath,
		"port":      h.manager.Port,
		"language":  h.manager.Language,
		// Provide both release and beta languages for live switching in the UI
		"languages":           relLangs,
		"release_languages":   relLangs,
		"beta_languages":      betaLangs,
		"auto_update":         h.manager.UpdateTime.Format("15:04:05"),
		"start_update":        h.manager.StartupUpdate,
		"release_latest":      h.manager.ReleaseLatest(),
		"beta_latest":         h.manager.BetaLatest(),
		"steamcmd_latest":     h.manager.SteamCmdLatest(),
		"bepinex_latest":      h.manager.BepInExLatest(),
		"launchpad_latest":    h.manager.LaunchPadLatest(),
		"release_deployed":    h.manager.ReleaseDeployed(),
		"beta_deployed":       h.manager.BetaDeployed(),
		"steamcmd_deployed":   h.manager.SteamCmdDeployed(),
		"bepinex_deployed":    h.manager.BepInExDeployed(),
		"launchpad_deployed":  h.manager.LaunchPadDeployed(),
		"server_count":        h.manager.ServerCount(),
		"server_count_active": h.manager.ServerCountActive(),
		"updating":            h.manager.IsUpdating(),
		"needs_setup":         needsSetup,
		"setup_in_progress":   setupInProgress,
		"missing_components":  missingComponents,
		"deploy_errors":       deployErrors,
		"setup_last_log_line": lastLogLine,
		"setup_log_fallback":  updateLogFallback,
		"game_data_warnings":  warnings,
	}
	c.HTML(http.StatusOK, "manager.html", data)
}

// ServerWorldImage streams the PNG planet image for the server's configured world.
func (h *ManagerHandlers) ServerWorldImage(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	var srv *models.Server
	for _, s := range h.manager.Servers {
		if s.ID == serverID {
			srv = s
			break
		}
	}

	if srv == nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Allow optional preview overrides via query parameters so the UI can
	// show a live world image when the user changes selections.
	worldID := srv.WorldID
	if w := strings.TrimSpace(c.Query("world")); w != "" {
		worldID = w
	}
	beta := srv.Beta
	if qb := strings.TrimSpace(c.Query("beta")); qb != "" {
		if parsed, perr := strconv.ParseBool(qb); perr == nil {
			beta = parsed
		}
	}

	data, readErr := h.manager.GetWorldImage(worldID, beta)
	if readErr != nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}
