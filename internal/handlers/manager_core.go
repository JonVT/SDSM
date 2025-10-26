package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"sdsm/internal/manager"
	"sdsm/internal/models"

	"github.com/gin-gonic/gin"
)

const updateLogFallback = "No update activity recorded yet."

type ManagerHandlers struct {
	manager *manager.Manager
}

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
		"serverPath": h.manager.Paths.ServerDir(s.ID),
	}

	if errMsg != "" {
		payload["error"] = errMsg
	}

	c.HTML(status, "server_status.html", payload)
}

func (h *ManagerHandlers) renderNewServerForm(c *gin.Context, status int, username interface{}, overrides gin.H) {
	worldLists, worldData := h.buildWorldSelectionData()

	const defaultServerPort = 27016
	suggestedName := "My Stationeers Server"
	if count := h.manager.ServerCount(); count > 0 {
		suggestedName = fmt.Sprintf("My Stationeers Server %d", count+1)
	}

	formDefaults := gin.H{
		"name":                  suggestedName,
		"world":                 "",
		"start_location":        "",
		"start_condition":       "",
		"difficulty":            "Normal",
		"port":                  fmt.Sprintf("%d", h.manager.GetNextAvailablePort(defaultServerPort)),
		"max_clients":           "10",
		"password":              "",
		"auth_secret":           "",
		"save_interval":         "300",
		"restart_delay_seconds": fmt.Sprintf("%d", models.DefaultRestartDelaySeconds),
		"beta":                  "false",
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

	payload := gin.H{
		"username":     username,
		"worldLists":   worldLists,
		"worldData":    worldData,
		"difficulties": h.manager.GetDifficulties(),
		"form":         formDefaults,
	}

	for k, v := range overrides {
		payload[k] = v
	}

	c.HTML(status, "server_new.html", payload)
}

func (h *ManagerHandlers) startDeployAsync(deployType manager.DeployType) error {
	if err := h.manager.StartDeployAsync(deployType); err != nil {
		return err
	}
	return nil
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

func (h *ManagerHandlers) Home(c *gin.Context) {
	c.Redirect(http.StatusFound, "/login")
}

func (h *ManagerHandlers) Frame(c *gin.Context) {
	username, _ := c.Get("username")

	data := gin.H{
		"active":   h.manager.IsActive(),
		"servers":  h.manager.Servers,
		"username": username,
	}
	c.HTML(http.StatusOK, "frame.html", data)
}

func (h *ManagerHandlers) Dashboard(c *gin.Context) {
	username, _ := c.Get("username")

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"servers":  h.manager.Servers,
		"username": username,
	})
}

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

	data := gin.H{
		"username":            username,
		"steam_id":            h.manager.SteamID,
		"root_path":           h.manager.Paths.RootPath,
		"port":                h.manager.Port,
		"language":            h.manager.Language,
		"languages":           h.manager.GetLanguages(),
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
	}
	c.HTML(http.StatusOK, "manager.html", data)
}

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

	data, readErr := h.manager.GetWorldImage(srv.WorldID, srv.Beta)
	if readErr != nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}
