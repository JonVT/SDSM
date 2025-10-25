package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"sdsm/manager"
	"sdsm/middleware"
	"sdsm/models"

	"github.com/gin-gonic/gin"
)

type AuthHandlers struct {
	authService *middleware.AuthService
	manager     *manager.Manager
}

type LoginRequest struct {
	Username string `json:"username" binding:"required" validate:"required,min=3,max=50"`
	Password string `json:"password" binding:"required" validate:"required,min=6"`
}

const updateLogFallback = "No update activity recorded yet."

// User credentials (in production, this should be from a database)
var userCredentials = map[string]string{
	"admin": "", // This will be set with hashed password
	"user1": "", // Add more users here
	"user2": "", // Add more users here
}

func NewAuthHandlers(authService *middleware.AuthService, mgr *manager.Manager) *AuthHandlers {
	h := &AuthHandlers{
		authService: authService,
		manager:     mgr,
	}

	// Initialize user passwords
	// Priority: Environment variables > Default passwords
	userPasswords := map[string]string{
		"admin": getEnvOrDefault("SDSM_ADMIN_PASSWORD", "admin123"),
		"user1": getEnvOrDefault("SDSM_USER1_PASSWORD", "password1"),
		"user2": getEnvOrDefault("SDSM_USER2_PASSWORD", "password2"),
	}

	for username, password := range userPasswords {
		hashedPassword, err := authService.HashPassword(password)
		if err != nil {
			// In production, handle this error properly
			panic(fmt.Sprintf("Failed to hash password for user %s: %v", username, err))
		}
		userCredentials[username] = hashedPassword
	}

	return h
}

// Helper function to get environment variable or default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func (h *AuthHandlers) LoginGET(c *gin.Context) {
	// Check if already authenticated
	if token, _ := c.Cookie(middleware.CookieName); token != "" {
		if _, err := h.authService.ValidateToken(token); err == nil {
			c.Redirect(http.StatusFound, "/manager")
			return
		}
	}

	redirect := c.Query("redirect")
	c.HTML(http.StatusOK, "login.html", gin.H{
		"redirect": redirect,
		"error":    c.Query("error"),
	})
}

func (h *AuthHandlers) LoginPOST(c *gin.Context) {
	username := middleware.SanitizeString(c.PostForm("username"))
	password := c.PostForm("password")
	redirect := c.PostForm("redirect")

	if username == "" || password == "" {
		c.HTML(http.StatusBadRequest, "login.html", gin.H{
			"error":    "Username and password are required",
			"redirect": redirect,
		})
		return
	}

	// Validate credentials
	hashedPassword, exists := userCredentials[username]
	if !exists || !h.authService.CheckPassword(password, hashedPassword) {
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{
			"error":    "Invalid username or password",
			"redirect": redirect,
		})
		return
	}

	// Generate JWT token
	token, err := h.authService.GenerateToken(username)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "login.html", gin.H{
			"error":    "Failed to generate authentication token",
			"redirect": redirect,
		})
		return
	}

	// Set secure cookie
	c.SetCookie(
		middleware.CookieName,
		token,
		int(middleware.TokenExpiry.Seconds()),
		"/",
		"",    // domain
		false, // secure (set to true in production with HTTPS)
		true,  // httpOnly
	)

	if redirect == "" || redirect == "/login" || redirect == "/setup" {
		redirect = "/manager"
	}

	// Redirect to requested page or manager dashboard
	c.Redirect(http.StatusFound, redirect)
}

func (h *AuthHandlers) Logout(c *gin.Context) {
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// API login endpoint
func (h *AuthHandlers) APILogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
		})
		return
	}

	username := middleware.SanitizeString(req.Username)
	password := req.Password

	// Validate credentials
	hashedPassword, exists := userCredentials[username]
	if !exists || !h.authService.CheckPassword(password, hashedPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid username or password",
		})
		return
	}

	// Generate JWT token
	token, err := h.authService.GenerateToken(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate authentication token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  username,
	})
}

// Manager handlers with updated authentication
type ManagerHandlers struct {
	manager *manager.Manager
}

func NewManagerHandlers(mgr *manager.Manager) *ManagerHandlers {
	return &ManagerHandlers{
		manager: mgr,
	}
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

	data, readErr := h.manager.GetWorldImage(srv.World, srv.Beta)
	if readErr != nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}

func (h *ManagerHandlers) renderNewServerForm(c *gin.Context, status int, username interface{}, overrides gin.H) {
	worldLists, worldData := h.buildWorldSelectionData()

	payload := gin.H{
		"username":     username,
		"worldLists":   worldLists,
		"worldData":    worldData,
		"difficulties": h.manager.GetDifficulties(),
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

func (h *ManagerHandlers) UpdatePOST(c *gin.Context) {
	if !middleware.ValidateFormData(c, []string{}) { // No required fields for this form
		return
	}

	isAsync := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json") ||
		strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")

	var deployType manager.DeployType
	var deployErr error
	actionHandled := false

	if c.PostForm("update_config") != "" {
		actionHandled = true
		steamID := middleware.SanitizeString(c.PostForm("steam_id"))
		rootPath := middleware.SanitizeFilename(c.PostForm("root_path"))
		portStr := c.PostForm("port")
		language := middleware.SanitizeString(c.PostForm("language"))

		port, err := middleware.ValidatePort(portStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "manager.html", gin.H{
				"error": "Invalid port number",
			})
			return
		}

		t, _ := time.Parse("15:04:05", c.PostForm("auto_update"))
		startupUpdate := c.PostForm("start_update") == "on"

		h.manager.UpdateConfig(steamID, rootPath, port, t, startupUpdate)
		if language != "" {
			h.manager.Language = language
			h.manager.Save()
		}
	} else if c.PostForm("update_release") != "" {
		actionHandled = true
		deployType = manager.DeployTypeRelease
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_beta") != "" {
		actionHandled = true
		deployType = manager.DeployTypeBeta
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_steamcmd") != "" {
		actionHandled = true
		deployType = manager.DeployTypeSteamCMD
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_bepinex") != "" {
		actionHandled = true
		deployType = manager.DeployTypeBepInEx
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_launchpad") != "" {
		actionHandled = true
		deployType = manager.DeployTypeLaunchPad
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_all") != "" {
		actionHandled = true
		deployType = manager.DeployTypeAll
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("shutdown") != "" {
		actionHandled = true
		go h.manager.Shutdown()
	} else if c.PostForm("restart") != "" {
		actionHandled = true
		go h.manager.Restart()
	}

	if isAsync {
		if deployType != "" {
			if deployErr != nil {
				c.JSON(http.StatusConflict, gin.H{
					"error": deployErr.Error(),
				})
				return
			}
			c.JSON(http.StatusAccepted, gin.H{
				"status":      "started",
				"deploy_type": deployType,
			})
			return
		}

		if actionHandled {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "no action specified"})
		return
	}

	c.Redirect(http.StatusFound, "/manager")
}

func (h *ManagerHandlers) UpdateProgressGET(c *gin.Context) {
	c.JSON(http.StatusOK, h.manager.ProgressSnapshot())
}

func (h *ManagerHandlers) ServerGET(c *gin.Context) {
	username, _ := c.Get("username")

	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Invalid server ID",
		})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Server not found",
		})
		return
	}

	h.renderServerPage(c, http.StatusOK, s, username, "")
}

func (h *ManagerHandlers) ServerPOST(c *gin.Context) {
	username, _ := c.Get("username")

	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Invalid server ID",
		})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Server not found",
		})
		return
	}

	// Handle different actions
	isAsync := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json") ||
		strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")

	if c.PostForm("start") != "" {
		s.Start()
	} else if c.PostForm("restart") != "" {
		go s.Restart()
	} else if c.PostForm("stop") != "" {
		s.Stop()
	} else if c.PostForm("deploy") != "" {
		if err := s.Deploy(); err != nil {
			errMsg := fmt.Sprintf("Deploy failed: %v", err)
			if isAsync {
				c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
			} else {
				h.renderServerPage(c, http.StatusInternalServerError, s, username, errMsg)
			}
			return
		}
	} else if c.PostForm("update_server") != "" {
		if s.Logger != nil {
			s.Logger.Write("Update Server requested via UI; redeploying server files.")
		}
		if isAsync {
			if h.manager.IsServerUpdateRunning(s.ID) {
				c.JSON(http.StatusOK, gin.H{"status": "running"})
				return
			}
			h.startServerUpdateAsync(s)
			c.JSON(http.StatusOK, gin.H{"status": "started"})
			return
		}
		if err := s.Deploy(); err != nil {
			errMsg := fmt.Sprintf("Update failed: %v", err)
			h.renderServerPage(c, http.StatusInternalServerError, s, username, errMsg)
			return
		}
	} else if c.PostForm("delete") != "" {
		// Stop the server if running
		if s.Running {
			s.Stop()
		}

		// Delete server directory structure
		if s.Paths != nil {
			err := s.Paths.DeleteServerDirectory(s.ID, s.Logger)
			if err != nil {
				s.Logger.Write(fmt.Sprintf("Failed to delete server directory: %v", err))
			}
		}

		// Remove server from manager
		for i, srv := range h.manager.Servers {
			if srv.ID == serverID {
				h.manager.Servers = append(h.manager.Servers[:i], h.manager.Servers[i+1:]...)
				break
			}
		}

		// Save configuration
		h.manager.Save()

		// Redirect to dashboard
		c.Redirect(http.StatusFound, "/dashboard")
		return
	} else if c.PostForm("update") != "" {
		// Track original beta flag to detect game version changes
		originalBeta := s.Beta

		// Update server configuration
		if name := middleware.SanitizeString(c.PostForm("name")); name != "" {
			// Check if new name is unique
			if !h.manager.IsServerNameAvailable(name, s.ID) {
				h.renderServerPage(c, http.StatusBadRequest, s, username, "Server name already exists. Please choose a unique name.")
				return
			}
			s.Name = name
		}

		if world := middleware.SanitizeString(c.PostForm("world")); world != "" {
			s.World = world
		}

		if startLocation := middleware.SanitizeString(c.PostForm("start_location")); startLocation != "" {
			s.StartLocation = startLocation
		}

		if startCondition := middleware.SanitizeString(c.PostForm("start_condition")); startCondition != "" {
			s.StartCondition = startCondition
		}

		if difficulty := middleware.SanitizeString(c.PostForm("difficulty")); difficulty != "" {
			s.Difficulty = difficulty
		}

		if portStr := c.PostForm("port"); portStr != "" {
			if port, err := middleware.ValidatePort(portStr); err == nil {
				// Check if port is available (excluding current server)
				if h.manager.IsPortAvailable(port, s.ID) {
					s.Port = port
				} else {
					suggestedPort := h.manager.GetNextAvailablePort(port)
					h.renderServerPage(c, http.StatusBadRequest, s, username, fmt.Sprintf("Port %d is not available. Try port %d.", port, suggestedPort))
					return
				}
			}
		}

		s.Password = c.PostForm("password")
		s.AuthSecret = c.PostForm("auth_secret")

		if maxClientsStr := c.PostForm("max_clients"); maxClientsStr != "" {
			if maxClients, err := strconv.Atoi(maxClientsStr); err == nil && maxClients >= 1 && maxClients <= 100 {
				s.MaxClients = maxClients
			}
		}

		if saveIntervalStr := c.PostForm("save_interval"); saveIntervalStr != "" {
			if saveInterval, err := strconv.Atoi(saveIntervalStr); err == nil && saveInterval >= 60 && saveInterval <= 3600 {
				s.SaveInterval = saveInterval
			}
		}

		if restartDelayStr := c.PostForm("restart_delay_seconds"); restartDelayStr != "" {
			if restartDelay, err := strconv.Atoi(restartDelayStr); err == nil && restartDelay >= 0 && restartDelay <= 3600 {
				s.RestartDelaySeconds = restartDelay
			}
		}

		s.Visible = c.PostForm("server_visible") == "on"
		s.Beta = c.PostForm("beta") == "true"
		s.AutoStart = c.PostForm("auto_start") == "on"
		s.AutoUpdate = c.PostForm("auto_update") == "on"
		s.AutoSave = c.PostForm("auto_save") == "on"
		s.AutoPause = c.PostForm("auto_pause") == "on"
		s.WorldID = h.manager.ResolveWorldID(s.World, s.Beta)

		// If game version changed, redeploy server files
		if s.Beta != originalBeta {
			h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) game version changed from %s to %s. Redeploying...",
				s.Name, s.ID,
				map[bool]string{true: "beta", false: "release"}[originalBeta],
				map[bool]string{true: "beta", false: "release"}[s.Beta]))
			if err := s.Deploy(); err != nil {
				h.renderServerPage(c, http.StatusInternalServerError, s, username, fmt.Sprintf("Redeploy failed: %v", err))
				return
			}
		}

		// Save configuration
		h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) configuration updated.", s.Name, s.ID))
		h.manager.Save()
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/server/%d", serverID))
}

func (h *ManagerHandlers) ServerProgressGET(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	progress := h.manager.ServerProgressSnapshot(serverID)
	c.JSON(http.StatusOK, progress)
}

func (h *ManagerHandlers) UpdateStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flush := func() {
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}

	sendSnapshot := func() {
		c.SSEvent("progress", h.manager.ProgressSnapshot())
		flush()
	}

	sendSnapshot()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendSnapshot()
		}
	}
}

func (h *ManagerHandlers) UpdateLogGET(c *gin.Context) {
	logPath := h.manager.Paths.UpdateLogFile()
	if logPath == "" {
		c.String(http.StatusNotFound, "Update log is not available.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.String(http.StatusOK, "Update log is empty.")
			return
		}
		c.String(http.StatusInternalServerError, "Unable to open update log.")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Unable to read update log.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func (h *ManagerHandlers) ManagerLogGET(c *gin.Context) {
	logPath := h.manager.Paths.LogFile()
	if logPath == "" {
		c.String(http.StatusNotFound, "Application log is not available.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.String(http.StatusOK, "Application log is empty.")
			return
		}
		c.String(http.StatusInternalServerError, "Unable to open application log.")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Unable to read application log.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

// API handlers for HTMX/AJAX requests
func (h *ManagerHandlers) APIServerStatus(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var started, saved string
	if s.ServerStarted != nil {
		started = s.ServerStarted.Format(time.RFC3339)
	}
	if s.ServerSaved != nil {
		saved = s.ServerSaved.Format(time.RFC3339)
	}

	liveClients := s.LiveClients()
	players := make([]gin.H, 0, len(liveClients))
	for _, client := range liveClients {
		if client == nil {
			continue
		}
		players = append(players, gin.H{
			"name":         client.Name,
			"steam_id":     client.SteamID,
			"connected_at": client.ConnectDatetime.Format(time.RFC3339),
		})
	}

	history := make([]gin.H, 0, len(s.Clients))
	for _, client := range s.Clients {
		if client == nil {
			continue
		}
		var disconnect string
		if client.DisconnectDatetime != nil {
			disconnect = client.DisconnectDatetime.Format(time.RFC3339)
		}
		history = append(history, gin.H{
			"name":           client.Name,
			"steam_id":       client.SteamID,
			"connected_at":   client.ConnectDatetime.Format(time.RFC3339),
			"disconnect_at":  disconnect,
			"session_length": client.SessionDurationString(),
		})
	}

	lastLine := strings.TrimSpace(s.LastLogLine)

	c.JSON(http.StatusOK, gin.H{
		"id":             s.ID,
		"name":           s.Name,
		"running":        s.IsRunning(),
		"starting":       s.Starting,
		"port":           s.Port,
		"player_count":   len(liveClients),
		"last_log_line":  lastLine,
		"server_started": started,
		"server_saved":   saved,
		"paused":         s.Paused,
		"players":        players,
		"clients":        history,
	})
}

func (h *ManagerHandlers) APIServerStart(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	s.Start()
	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

func (h *ManagerHandlers) APIServerStop(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	s.Stop()
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (h *ManagerHandlers) APIServerLog(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.String(http.StatusNotFound, "Server not found")
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.String(http.StatusNotFound, "Server not found")
		return
	}

	var logPath string
	if s.Paths != nil {
		logPath = s.Paths.ServerOutputFile(s.ID)
	} else if h.manager.Paths != nil {
		logPath = h.manager.Paths.ServerOutputFile(s.ID)
	}

	if logPath == "" {
		c.String(http.StatusNotFound, "Server log is not available.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.String(http.StatusOK, "")
			return
		}
		c.String(http.StatusInternalServerError, "Unable to open server log.")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Unable to read server log.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func (h *ManagerHandlers) APIManagerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"active":              h.manager.IsActive(),
		"updating":            h.manager.IsUpdating(),
		"server_count":        h.manager.ServerCount(),
		"server_count_active": h.manager.ServerCountActive(),
	})
}

func (h *ManagerHandlers) APIStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"totalServers":  h.manager.ServerCount(),
		"activeServers": h.manager.ServerCountActive(),
		"totalPlayers":  h.manager.GetTotalPlayers(),
		"systemHealth":  "100%",
	})
}

func (h *ManagerHandlers) APIServers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"servers": h.manager.Servers,
	})
}

func (h *ManagerHandlers) APIGetStartLocations(c *gin.Context) {
	worldID := c.Query("world")
	if worldID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world parameter required"})
		return
	}

	beta := false
	if betaParam := c.Query("beta"); betaParam != "" {
		if parsed, err := strconv.ParseBool(betaParam); err == nil {
			beta = parsed
		}
	}

	locations := h.manager.GetStartLocationsForWorldVersion(worldID, beta)
	c.JSON(http.StatusOK, gin.H{
		"locations": locations,
	})
}

func (h *ManagerHandlers) APIGetStartConditions(c *gin.Context) {
	worldID := c.Query("world")
	if worldID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world parameter required"})
		return
	}

	beta := false
	if betaParam := c.Query("beta"); betaParam != "" {
		if parsed, err := strconv.ParseBool(betaParam); err == nil {
			beta = parsed
		}
	}

	conditions := h.manager.GetStartConditionsForWorldVersion(worldID, beta)
	c.JSON(http.StatusOK, gin.H{
		"conditions": conditions,
	})
}

func (h *ManagerHandlers) NewServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	h.renderNewServerForm(c, http.StatusOK, username, nil)
}

func (h *ManagerHandlers) NewServerPOST(c *gin.Context) {
	username, _ := c.Get("username")

	if !middleware.ValidateFormData(c, []string{"name", "world", "difficulty", "port", "max_clients", "save_interval", "beta"}) {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Missing required fields"})
		return
	}

	// Parse form data
	name := middleware.SanitizeString(c.PostForm("name"))
	world := middleware.SanitizeString(c.PostForm("world"))
	startLocation := middleware.SanitizeString(c.PostForm("start_location"))
	startCondition := middleware.SanitizeString(c.PostForm("start_condition"))
	difficulty := middleware.SanitizeString(c.PostForm("difficulty"))
	password := c.PostForm("password")
	authSecret := c.PostForm("auth_secret")

	// Check if server name is unique
	if !h.manager.IsServerNameAvailable(name, -1) {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Server name already exists. Please choose a unique name."})
		return
	}

	port, err := middleware.ValidatePort(c.PostForm("port"))
	if err != nil {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Invalid port number"})
		return
	}

	// Check if port is available (unique and at least 3 ports apart)
	if !h.manager.IsPortAvailable(port, -1) {
		suggestedPort := h.manager.GetNextAvailablePort(port)
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": fmt.Sprintf("Port %d is not available. Ports must be unique and at least 3 apart. Try port %d.", port, suggestedPort)})
		return
	}

	maxClients, err := strconv.Atoi(c.PostForm("max_clients"))
	if err != nil || maxClients < 1 || maxClients > 100 {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Invalid max players (1-100)"})
		return
	}

	saveInterval, err := strconv.Atoi(c.PostForm("save_interval"))
	if err != nil || saveInterval < 60 || saveInterval > 3600 {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Invalid save interval (60-3600)"})
		return
	}

	restartDelayStr := c.PostForm("restart_delay_seconds")
	restartDelay := models.DefaultRestartDelaySeconds
	if restartDelayStr != "" {
		if val, convErr := strconv.Atoi(restartDelayStr); convErr == nil && val >= 0 && val <= 3600 {
			restartDelay = val
		} else {
			h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Invalid restart delay (0-3600)"})
			return
		}
	}

	beta := c.PostForm("beta") == "true"
	autoStart := c.PostForm("auto_start") == "on"
	autoUpdate := c.PostForm("auto_update") == "on"
	autoSave := c.PostForm("auto_save") == "on"
	autoPause := c.PostForm("auto_pause") == "on"
	serverVisible := c.PostForm("server_visible") == "on"
	worldID := h.manager.ResolveWorldID(world, beta)
	if worldID == "" {
		worldID = world
	}

	cfg := &models.ServerConfig{
		Name:                name,
		World:               world,
		WorldID:             worldID,
		StartLocation:       startLocation,
		StartCondition:      startCondition,
		Difficulty:          difficulty,
		Port:                port,
		Password:            password,
		AuthSecret:          authSecret,
		MaxClients:          maxClients,
		SaveInterval:        saveInterval,
		Visible:             serverVisible,
		Beta:                beta,
		AutoStart:           autoStart,
		AutoUpdate:          autoUpdate,
		AutoSave:            autoSave,
		AutoPause:           autoPause,
		RestartDelaySeconds: restartDelay,
	}

	newServer, err := h.manager.AddServer(cfg)
	if err != nil {
		h.renderNewServerForm(c, http.StatusInternalServerError, username, gin.H{"error": "Failed to create server"})
		return
	}

	// Deploy server: build directory structure and copy game files based on beta flag
	if err := newServer.Deploy(); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Initial deploy for server %s (ID: %d) failed: %v", newServer.Name, newServer.ID, err))
	}

	// Redirect to server configuration page
	c.Redirect(http.StatusFound, fmt.Sprintf("/server/%d", newServer.ID))
}

// SetupGET shows the initial setup page if components are missing
func (h *ManagerHandlers) SetupGET(c *gin.Context) {
	if !h.manager.IsUpdating() {
		h.manager.CheckMissingComponents()
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{
		"missingComponents": h.manager.GetMissingComponents(),
		"paths":             h.manager.Paths,
		"deployErrors":      h.manager.GetDeployErrors(),
	})
}

// SetupSkipPOST allows user to skip the setup and continue anyway
func (h *ManagerHandlers) SetupSkipPOST(c *gin.Context) {
	h.manager.NeedsUploadPrompt = false
	h.manager.Log.Write("User skipped initial setup")

	if strings.Contains(c.GetHeader("Accept"), "application/json") {
		c.JSON(http.StatusOK, gin.H{
			"status":  "skipped",
			"message": "Setup skipped",
		})
		return
	}

	c.Redirect(http.StatusFound, "/manager")
}

// SetupInstallPOST runs the full installation of all missing components asynchronously
func (h *ManagerHandlers) SetupInstallPOST(c *gin.Context) {
	if h.manager.SetupInProgress {
		c.JSON(http.StatusConflict, gin.H{
			"status":  "error",
			"message": "Setup is already in progress",
		})
		return
	}

	missing := h.manager.GetMissingComponents()
	if len(missing) == 0 {
		h.manager.CheckMissingComponents()
		missing = h.manager.GetMissingComponents()
	}

	deployList, fallbackAll := determineSetupDeployTargets(missing, h.manager.ServerCount())
	if !fallbackAll && len(deployList) == 0 {
		message := "No missing components detected"
		h.manager.Log.Write("Setup requested but no missing components detected; skipping automatic install")
		c.JSON(http.StatusOK, gin.H{
			"status":  "noop",
			"message": message,
		})
		return
	}

	h.manager.SetupInProgress = true
	h.manager.Log.Write("User initiated automatic setup for missing components")

	go func(missing []string, deployList []manager.DeployType, fallbackAll bool) {
		defer func() {
			h.manager.SetupInProgress = false
		}()

		if fallbackAll || len(deployList) == 0 {
			h.manager.Log.Write("Automatic setup will deploy all components")
			if err := h.manager.Deploy(manager.DeployTypeAll); err != nil {
				h.manager.Log.Write(fmt.Sprintf("Automatic setup failed: %v", err))
				return
			}
			h.manager.CheckMissingComponents()
			if remaining := h.manager.GetMissingComponents(); len(remaining) > 0 {
				h.manager.Log.Write(fmt.Sprintf("Components still missing after setup: %v", remaining))
				return
			}
			h.manager.Log.Write("Automatic setup completed successfully")
			return
		}

		componentOrder := make([]string, len(deployList))
		for i, dt := range deployList {
			componentOrder[i] = string(dt)
		}
		h.manager.Log.Write(fmt.Sprintf("Automatic setup will deploy components: %s", strings.Join(componentOrder, ", ")))

		allSucceeded := true
		for _, dt := range deployList {
			if err := h.manager.Deploy(dt); err != nil {
				h.manager.Log.Write(fmt.Sprintf("Automatic setup failed while deploying %s: %v", dt, err))
				allSucceeded = false
			}
		}

		h.manager.CheckMissingComponents()
		if remaining := h.manager.GetMissingComponents(); len(remaining) > 0 {
			h.manager.Log.Write(fmt.Sprintf("Components still missing after setup: %v", remaining))
			allSucceeded = false
		}

		if allSucceeded {
			h.manager.Log.Write("Automatic setup completed successfully")
		}
	}(append([]string(nil), missing...), append([]manager.DeployType(nil), deployList...), fallbackAll)

	message := "Setup started in background"
	if fallbackAll {
		message = "Setup started for all components"
	} else if len(missing) > 0 {
		message = fmt.Sprintf("Setup started for: %s", strings.Join(missing, ", "))
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "started",
		"message": message,
	})
}

// SetupStatusGET returns the current status of the setup process
func (h *ManagerHandlers) SetupStatusGET(c *gin.Context) {
	updating := h.manager.IsUpdating()
	if !updating {
		h.manager.CheckMissingComponents()
	}
	c.JSON(http.StatusOK, gin.H{
		"inProgress":        h.manager.SetupInProgress,
		"needsUploadPrompt": h.manager.NeedsUploadPrompt,
		"updating":          updating,
		"errors":            h.manager.GetDeployErrors(),
		"missingComponents": h.manager.GetMissingComponents(),
		"lastUpdateLog":     h.manager.LastUpdateLogLine(),
	})
}

func determineSetupDeployTargets(missing []string, serverCount int) ([]manager.DeployType, bool) {
	if len(missing) == 0 {
		return nil, false
	}

	required := make(map[manager.DeployType]bool)
	needsServerRedeploy := false
	fallbackAll := false

	for _, component := range missing {
		switch component {
		case "SteamCMD":
			required[manager.DeployTypeSteamCMD] = true
		case "Stationeers Release":
			required[manager.DeployTypeRelease] = true
			needsServerRedeploy = true
		case "Stationeers LaunchPad":
			required[manager.DeployTypeLaunchPad] = true
			needsServerRedeploy = true
		case "BepInEx":
			required[manager.DeployTypeBepInEx] = true
			needsServerRedeploy = true
		default:
			fallbackAll = true
		}
	}

	if fallbackAll {
		return nil, true
	}

	if needsServerRedeploy && serverCount > 0 {
		required[manager.DeployTypeServers] = true
	}

	ordered := []manager.DeployType{
		manager.DeployTypeSteamCMD,
		manager.DeployTypeRelease,
		manager.DeployTypeBeta,
		manager.DeployTypeBepInEx,
		manager.DeployTypeLaunchPad,
		manager.DeployTypeServers,
	}

	var result []manager.DeployType
	for _, dt := range ordered {
		if required[dt] {
			result = append(result, dt)
		}
	}

	return result, false
}

// SetupUpdatePOST triggers automatic update/download of missing components
func (h *ManagerHandlers) SetupUpdatePOST(c *gin.Context) {
	h.manager.NeedsUploadPrompt = false
	h.manager.Log.Write("User requested auto-update for missing components")

	// Trigger deployment of all components
	if err := h.startDeployAsync(manager.DeployTypeAll); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Unable to start setup update: %v", err))
		c.Redirect(http.StatusFound, "/login?message=Unable to start update. Another deployment may already be running.")
		return
	}

	c.Redirect(http.StatusFound, "/login?message=Update started. Please wait for components to download.")
}
