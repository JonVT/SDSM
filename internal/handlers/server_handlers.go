package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sdsm/internal/middleware"
	"sdsm/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *ManagerHandlers) ServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

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

	if role != "admin" {
		if user, ok := username.(string); ok {
			if h.userStore == nil || !h.userStore.CanAccess(user, s.ID) {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "You do not have access to this server."})
				return
			}
		}
	}

	h.renderServerPage(c, http.StatusOK, s, username, "")
}

func (h *ManagerHandlers) ServerPOST(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

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

	if role != "admin" {
		if user, ok := username.(string); ok {
			if h.userStore == nil || !h.userStore.CanAccess(user, s.ID) {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "You do not have access to this server."})
				return
			}
		}
	}

	acceptsJSON := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json")
	isXHR := strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")
	isHX := strings.EqualFold(c.GetHeader("HX-Request"), "true")
	isAsync := acceptsJSON || isXHR || isHX

	switch {
	case c.PostForm("set_player_saves") != "":
		// Persist Player Saves preference and return JSON/toast
		val := strings.TrimSpace(c.PostForm("set_player_saves"))
		enabled := strings.EqualFold(val, "true") || val == "1" || strings.EqualFold(val, "on")
		s.PlayerSaves = enabled
		h.manager.Save()
		if isAsync {
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Preference Saved")
			c.Header("X-Toast-Message", fmt.Sprintf("Player Saves %s", map[bool]string{true: "enabled", false: "disabled"}[enabled]))
			c.JSON(http.StatusOK, gin.H{"status": "ok", "player_saves": enabled})
			return
		}
		// Non-async fallthrough will redirect at the end
	case c.PostForm("unban") != "":
		steamID := strings.TrimSpace(c.PostForm("unban"))
		if steamID != "" {
			_ = s.RemoveBlacklistID(steamID)
			if isAsync {
				c.Header("X-Toast-Type", "success")
				c.Header("X-Toast-Title", "Player Unbanned")
				c.Header("X-Toast-Message", fmt.Sprintf("%s removed from blacklist", steamID))
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
				return
			}
		}

	case c.PostForm("ban") != "":
		banVal := strings.TrimSpace(c.PostForm("ban"))
		steamID := ""
		// Prefer direct SteamID
		if banVal != "" {
			steamID = banVal
		}
		// If value looks like a name (no digits) or didn't match any known ID, try resolve by name
		if steamID != "" {
			// If it's a name, try to resolve to steam id from live or history
			isProbablyName := true
			for _, ch := range steamID {
				if ch >= '0' && ch <= '9' {
					isProbablyName = false
					break
				}
			}
			if isProbablyName {
				candidate := strings.ToLower(steamID)
				steamID = ""
				for _, c := range s.LiveClients() {
					if c != nil && strings.EqualFold(c.Name, candidate) {
						steamID = c.SteamID
						break
					}
				}
				if steamID == "" {
					for _, c := range s.Clients {
						if c != nil && strings.EqualFold(c.Name, candidate) {
							steamID = c.SteamID
							break
						}
					}
				}
			}
		}
		if steamID == "" {
			if isAsync {
				c.Header("X-Toast-Type", "error")
				c.Header("X-Toast-Title", "Ban Failed")
				c.Header("X-Toast-Message", "Unable to determine Steam ID for ban.")
				c.JSON(http.StatusBadRequest, gin.H{"error": "steam id not found"})
				return
			}
		} else {
			// If server is running, try BAN console command first
			commandSent := false
			if s.Running {
				if err := s.SendCommand("console", "BAN "+steamID); err == nil {
					commandSent = true
					if isAsync {
						c.Header("X-Toast-Type", "success")
						c.Header("X-Toast-Title", "Player Banned")
						c.Header("X-Toast-Message", fmt.Sprintf("BAN command sent for %s", steamID))
						c.JSON(http.StatusOK, gin.H{"status": "ok"})
						return
					}
				}
			}
			// Fallback or server not running: add to blacklist file
			if !commandSent {
				_ = s.AddBlacklistID(steamID)
				if isAsync {
					c.Header("X-Toast-Type", "success")
					c.Header("X-Toast-Title", "Player Banned")
					c.Header("X-Toast-Message", fmt.Sprintf("%s added to blacklist", steamID))
					c.JSON(http.StatusOK, gin.H{"status": "ok"})
					return
				}
			}
		}

	case c.PostForm("add_ban") != "":
		steamID := strings.TrimSpace(c.PostForm("ban_steam_id"))
		if steamID != "" {
			if err := s.AddBlacklistID(steamID); err != nil {
				if isAsync {
					c.Header("X-Toast-Type", "error")
					c.Header("X-Toast-Title", "Add Failed")
					c.Header("X-Toast-Message", "Could not add to blacklist: "+err.Error())
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
			} else if isAsync {
				c.Header("X-Toast-Type", "success")
				c.Header("X-Toast-Title", "Added to Blacklist")
				c.Header("X-Toast-Message", fmt.Sprintf("%s added to blacklist", steamID))
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
				return
			}
		}

	case c.PostForm("start") != "":
		s.Start()
		if isAsync {
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Server Started")
			c.Header("X-Toast-Message", s.Name+" is starting...")
			c.JSON(http.StatusOK, gin.H{"status": "started"})
			return
		}
	case c.PostForm("restart") != "":
		go s.Restart()
		if isAsync {
			c.Header("X-Toast-Type", "info")
			c.Header("X-Toast-Title", "Server Restarting")
			c.Header("X-Toast-Message", s.Name+" is restarting...")
			c.JSON(http.StatusOK, gin.H{"status": "restarting"})
			return
		}
	case c.PostForm("stop") != "":
		s.Stop()
		if isAsync {
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Server Stopped")
			c.Header("X-Toast-Message", s.Name+" has been stopped.")
			c.JSON(http.StatusOK, gin.H{"status": "stopped"})
			return
		}
	case c.PostForm("deploy") != "":
		if role != "admin" {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required to deploy servers."})
			return
		}
		if err := s.Deploy(); err != nil {
			errMsg := fmt.Sprintf("Deploy failed: %v", err)
			if isAsync {
				c.Header("X-Toast-Type", "error")
				c.Header("X-Toast-Title", "Deploy Failed")
				c.Header("X-Toast-Message", errMsg)
				c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
			} else {
				h.renderServerPage(c, http.StatusInternalServerError, s, username, errMsg)
			}
			return
		}
		if isAsync {
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Deploy Started")
			c.Header("X-Toast-Message", s.Name+" deployment started.")
			c.JSON(http.StatusOK, gin.H{"status": "deploying"})
			return
		}
	case c.PostForm("update_server") != "":
		if role != "admin" {
			c.Header("X-Toast-Type", "error")
			c.Header("X-Toast-Title", "Permission Denied")
			c.Header("X-Toast-Message", "Admin privileges required to update server files.")
			if isAsync {
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
			h.renderServerPage(c, http.StatusForbidden, s, username, "Admin privileges required.")
			return
		}
		if s.Logger != nil {
			s.Logger.Write("Update Server requested via UI; redeploying server files.")
		}
		if isAsync {
			if h.manager.IsServerUpdateRunning(s.ID) {
				c.Header("X-Toast-Type", "info")
				c.Header("X-Toast-Title", "Update Running")
				c.Header("X-Toast-Message", s.Name+" update already running.")
				c.JSON(http.StatusOK, gin.H{"status": "running"})
				return
			}
			h.startServerUpdateAsync(s)
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Update Started")
			c.Header("X-Toast-Message", s.Name+" update started.")
			c.JSON(http.StatusOK, gin.H{"status": "started"})
			return
		}
		if err := s.Deploy(); err != nil {
			errMsg := fmt.Sprintf("Update failed: %v", err)
			h.renderServerPage(c, http.StatusInternalServerError, s, username, errMsg)
			return
		}
	case c.PostForm("delete") != "":
		if role != "admin" {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required to delete servers."})
			return
		}
		if s.Running {
			s.Stop()
		}
		if s.Paths != nil {
			if err := s.Paths.DeleteServerDirectory(s.ID, s.Logger); err != nil {
				s.Logger.Write(fmt.Sprintf("Failed to delete server directory: %v", err))
			}
		}
		for i, srv := range h.manager.Servers {
			if srv.ID == serverID {
				h.manager.Servers = append(h.manager.Servers[:i], h.manager.Servers[i+1:]...)
				break
			}
		}
		h.manager.Save()
		c.Redirect(http.StatusFound, "/dashboard")
		return
	case c.PostForm("update") != "":
		if role != "admin" {
			if isAsync {
				c.Header("X-Toast-Type", "error")
				c.Header("X-Toast-Title", "Permission Denied")
				c.Header("X-Toast-Message", "Admin privileges required to change startup parameters.")
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
			h.renderServerPage(c, http.StatusForbidden, s, username, "Admin privileges required to change startup parameters.")
			return
		}
		originalBeta := s.Beta
		// Capture originals for core start parameters to decide if we need a (future) save purge
		origWorld := s.World
		origStartLoc := s.StartLocation
		origStartCond := s.StartCondition

		oldName := s.Name
		// Track original game port to keep SCONPort in sync on change
		originalPort := s.Port
		if name := middleware.SanitizeString(c.PostForm("name")); name != "" {
			if !h.manager.IsServerNameAvailable(name, s.ID) {
				if isAsync {
					c.Header("X-Toast-Type", "error")
					c.Header("X-Toast-Title", "Update Failed")
					c.Header("X-Toast-Message", "Server name already exists. Please choose a unique name.")
					c.JSON(http.StatusBadRequest, gin.H{"error": "name not available"})
					return
				}
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
				if h.manager.IsPortAvailable(port, s.ID) {
					s.Port = port
				} else {
					suggestedPort := h.manager.GetNextAvailablePort(port)
					if isAsync {
						c.Header("X-Toast-Type", "error")
						c.Header("X-Toast-Title", "Update Failed")
						c.Header("X-Toast-Message", fmt.Sprintf("Port %d is not available. Try %d.", port, suggestedPort))
						c.JSON(http.StatusBadRequest, gin.H{"error": "port not available", "suggested": suggestedPort})
						return
					}
					h.renderServerPage(c, http.StatusBadRequest, s, username, fmt.Sprintf("Port %d is not available. Try port %d.", port, suggestedPort))
					return
				}
			}
		}

		// If port changed, default SCONPort to game port + 1
		if s.Port > 0 && s.Port != originalPort {
			s.SCONPort = s.Port + 1
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

		if shutdownDelayStr := c.PostForm("shutdown_delay_seconds"); shutdownDelayStr != "" {
			if shutdownDelay, err := strconv.Atoi(shutdownDelayStr); err == nil && shutdownDelay >= 0 && shutdownDelay <= 3600 {
				s.ShutdownDelaySeconds = shutdownDelay
			}
		}

		// Welcome Message (optional, single-line)
		if wm := middleware.SanitizeString(c.PostForm("welcome_message")); wm != "" || c.PostForm("welcome_message") == "" {
			// Always set (allow clearing)
			clean := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(wm, "\r", " "), "\n", " "))
			if len(clean) > 300 {
				clean = clean[:300]
			}
			s.WelcomeMessage = clean
		}

		// Extended settings
		if v := strings.TrimSpace(c.PostForm("max_auto_saves")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				s.MaxAutoSaves = n
			}
		}
		if v := strings.TrimSpace(c.PostForm("max_quick_saves")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				s.MaxQuickSaves = n
			}
		}
		s.DeleteSkeletonOnDecay = c.PostForm("delete_skeleton_on_decay") == "on"
		s.UseSteamP2P = c.PostForm("use_steam_p2p") == "on"
		if v := strings.TrimSpace(c.PostForm("disconnect_timeout")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1000 && n <= 600000 {
				s.DisconnectTimeout = n
			}
		}

		s.Visible = c.PostForm("server_visible") == "on"
		s.Beta = c.PostForm("beta") == "true"
		s.AutoStart = c.PostForm("auto_start") == "on"
		s.AutoUpdate = c.PostForm("auto_update") == "on"
		s.AutoSave = c.PostForm("auto_save") == "on"
		s.AutoPause = c.PostForm("auto_pause") == "on"
		s.WorldID = h.manager.ResolveWorldID(s.World, s.Beta)

		// Determine if core start parameters changed
		coreChanged := (strings.TrimSpace(origWorld) != strings.TrimSpace(s.World)) ||
			(strings.TrimSpace(origStartLoc) != strings.TrimSpace(s.StartLocation)) ||
			(strings.TrimSpace(origStartCond) != strings.TrimSpace(s.StartCondition))
		if coreChanged {
			// Stub: mark pending purge but do not delete saves yet
			s.PendingSavePurge = true
			if s.Logger != nil {
				s.Logger.Write("Core start parameters changed; pending save purge flagged (stub, no deletion yet)")
			}
		}

		if s.Beta != originalBeta {
			h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) game version changed; redeploying...", s.Name, s.ID))
			if err := s.Deploy(); err != nil {
				if isAsync {
					c.Header("X-Toast-Type", "error")
					c.Header("X-Toast-Title", "Redeploy Failed")
					c.Header("X-Toast-Message", err.Error())
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				h.renderServerPage(c, http.StatusInternalServerError, s, username, fmt.Sprintf("Redeploy failed: %v", err))
				return
			}
		}

		// If the server name changed, rename the saves/<ServerName> directory accordingly.
		if strings.TrimSpace(oldName) != "" && strings.TrimSpace(s.Name) != "" && !strings.EqualFold(strings.TrimSpace(oldName), strings.TrimSpace(s.Name)) {
			var savesBase string
			if s.Paths != nil {
				savesBase = s.Paths.ServerSavesDir(s.ID)
			} else if h.manager.Paths != nil {
				savesBase = h.manager.Paths.ServerSavesDir(s.ID)
			}
			if strings.TrimSpace(savesBase) != "" {
				oldDir := filepath.Join(savesBase, oldName)
				newDir := filepath.Join(savesBase, s.Name)
				if st, err := os.Stat(oldDir); err == nil && st.IsDir() {
					if _, err2 := os.Stat(newDir); os.IsNotExist(err2) {
						// Simple rename when target doesn't exist
						if err := os.Rename(oldDir, newDir); err != nil && s.Logger != nil {
							s.Logger.Write(fmt.Sprintf("Failed to rename saves directory %s -> %s: %v", oldDir, newDir, err))
						} else if s.Logger != nil {
							s.Logger.Write(fmt.Sprintf("Renamed saves directory: %s -> %s", oldDir, newDir))
						}
					} else {
						// Target exists: merge contents best-effort, then remove old dir if empty
						entries, _ := os.ReadDir(oldDir)
						for _, e := range entries {
							src := filepath.Join(oldDir, e.Name())
							dst := filepath.Join(newDir, e.Name())
							// If destination exists, skip to avoid overwriting
							if _, err := os.Stat(dst); err == nil {
								continue
							}
							_ = os.Rename(src, dst)
						}
						// Attempt to remove oldDir if empty
						_ = os.Remove(oldDir)
						if s.Logger != nil {
							s.Logger.Write(fmt.Sprintf("Merged saves from %s into %s due to name change", oldDir, newDir))
						}
					}
				}
			}
		}

		h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) configuration updated.", s.Name, s.ID))
		h.manager.Save()
		if isAsync {
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Settings Updated")
			c.Header("X-Toast-Message", "Startup parameters saved.")
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
	case c.PostForm("set_language") != "":
		// Language is no longer a startup parameter; handle dedicated async change
		lang := middleware.SanitizeString(c.PostForm("language"))
		if lang == "" {
			if acceptsJSON {
				c.Header("X-Toast-Type", "error")
				c.Header("X-Toast-Title", "Language Not Set")
				c.Header("X-Toast-Message", "No language provided.")
				c.JSON(http.StatusBadRequest, gin.H{"error": "missing language"})
				return
			}
		} else {
			allowed := h.manager.GetLanguagesForVersion(s.Beta)
			ok := false
			for _, a := range allowed {
				if strings.EqualFold(a, lang) {
					ok = true
					break
				}
			}
			if !ok && len(allowed) > 0 {
				if acceptsJSON {
					c.Header("X-Toast-Type", "error")
					c.Header("X-Toast-Title", "Invalid Language")
					c.Header("X-Toast-Message", "Selected language is not available for this version.")
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid language"})
					return
				}
			}
			s.Language = lang
			h.manager.Save()
			if acceptsJSON {
				c.Header("X-Toast-Type", "success")
				c.Header("X-Toast-Title", "Language Updated")
				c.Header("X-Toast-Message", fmt.Sprintf("Language set to %s", lang))
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
				return
			}
		}
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

func (h *ManagerHandlers) NewServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	h.renderNewServerForm(c, http.StatusOK, username, nil)
}

func (h *ManagerHandlers) NewServerPOST(c *gin.Context) {
	username, _ := c.Get("username")
	name := middleware.SanitizeString(c.PostForm("name"))
	world := middleware.SanitizeString(c.PostForm("world"))
	startLocation := middleware.SanitizeString(c.PostForm("start_location"))
	startCondition := middleware.SanitizeString(c.PostForm("start_condition"))
	difficulty := middleware.SanitizeString(c.PostForm("difficulty"))
	password := c.PostForm("password")
	authSecret := c.PostForm("auth_secret")
	betaRaw := strings.TrimSpace(c.PostForm("beta"))
	beta := betaRaw == "true"
	autoStart := c.PostForm("auto_start") == "on"
	autoUpdate := c.PostForm("auto_update") == "on"
	autoSave := c.PostForm("auto_save") == "on"
	autoPause := c.PostForm("auto_pause") == "on"
	playerSaves := c.PostForm("player_saves") == "on"
	// Extended settings for saves/network
	maxAutoSaves := 5
	if v := strings.TrimSpace(c.PostForm("max_auto_saves")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			maxAutoSaves = n
		}
	}
	maxQuickSaves := 5
	if v := strings.TrimSpace(c.PostForm("max_quick_saves")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			maxQuickSaves = n
		}
	}
	deleteSkeletonOnDecay := c.PostForm("delete_skeleton_on_decay") == "on"
	useSteamP2P := c.PostForm("use_steam_p2p") == "on"
	disconnectTimeout := 10000
	if v := strings.TrimSpace(c.PostForm("disconnect_timeout")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1000 && n <= 600000 {
			disconnectTimeout = n
		}
	}
	serverVisible := c.PostForm("server_visible") == "on"
	welcomeMessage := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(middleware.SanitizeString(c.PostForm("welcome_message")), "\r", " "), "\n", " "))
	if len(welcomeMessage) > 300 {
		welcomeMessage = welcomeMessage[:300]
	}

	formState := gin.H{
		"name":                     name,
		"world":                    world,
		"start_location":           startLocation,
		"start_condition":          startCondition,
		"difficulty":               difficulty,
		"port":                     c.PostForm("port"),
		"max_clients":              c.PostForm("max_clients"),
		"password":                 password,
		"auth_secret":              authSecret,
		"save_interval":            c.PostForm("save_interval"),
		"restart_delay_seconds":    c.PostForm("restart_delay_seconds"),
		"beta":                     betaRaw,
		"auto_start":               autoStart,
		"auto_update":              autoUpdate,
		"auto_save":                autoSave,
		"auto_pause":               autoPause,
		"server_visible":           serverVisible,
		"player_saves":             playerSaves,
		"max_auto_saves":           maxAutoSaves,
		"max_quick_saves":          maxQuickSaves,
		"delete_skeleton_on_decay": deleteSkeletonOnDecay,
		"use_steam_p2p":            useSteamP2P,
		"disconnect_timeout":       disconnectTimeout,
		"welcome_message":          welcomeMessage,
	}
	// Reflect Player Saves already included; keep extended settings for redisplay
	// Player Saves preference for UI (client-side feature)
	// We'll read the posted value below and also stash it here for error redisplay
	// so the checkbox state persists when validation fails.

	if name == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Server name is required.",
			"form":  formState,
		})
		return
	}

	if world == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "World selection is required.",
			"form":  formState,
		})
		return
	}

	if startLocation == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Start location is required.",
			"form":  formState,
		})
		return
	}

	if startCondition == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Start condition is required.",
			"form":  formState,
		})
		return
	}

	if difficulty == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Difficulty selection is required.",
			"form":  formState,
		})
		return
	}

	if betaRaw == "" {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Game version selection is required.",
			"form":  formState,
		})
		return
	}

	if !h.manager.IsServerNameAvailable(name, -1) {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": fmt.Sprintf("Server name '%s' already exists. Please choose a unique name.", name),
			"form":  formState,
		})
		return
	}

	port, err := middleware.ValidatePort(c.PostForm("port"))
	if err != nil {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": fmt.Sprintf("Invalid port number: %s", c.PostForm("port")),
			"form":  formState,
		})
		return
	}
	formState["port"] = fmt.Sprintf("%d", port)

	if !h.manager.IsPortAvailable(port, -1) {
		suggestedPort := h.manager.GetNextAvailablePort(port)
		formState["port"] = fmt.Sprintf("%d", suggestedPort)
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": fmt.Sprintf("Port %d is not available. Ports must be unique and at least 3 apart. Try port %d.", port, suggestedPort),
			"form":  formState,
		})
		return
	}

	maxClients, err := strconv.Atoi(c.PostForm("max_clients"))
	if err != nil || maxClients < 1 || maxClients > 100 {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Invalid max players (1-100)",
			"form":  formState,
		})
		return
	}
	formState["max_clients"] = fmt.Sprintf("%d", maxClients)

	saveInterval, err := strconv.Atoi(c.PostForm("save_interval"))
	if err != nil || saveInterval < 60 || saveInterval > 3600 {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
			"error": "Invalid save interval (60-3600)",
			"form":  formState,
		})
		return
	}
	formState["save_interval"] = fmt.Sprintf("%d", saveInterval)

	restartDelay := models.DefaultRestartDelaySeconds
	if restartDelayStr := c.PostForm("restart_delay_seconds"); restartDelayStr != "" {
		if val, convErr := strconv.Atoi(restartDelayStr); convErr == nil && val >= 0 && val <= 3600 {
			restartDelay = val
		} else {
			h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
				"error": "Invalid restart delay (0-3600)",
				"form":  formState,
			})
			return
		}
	}
	// Shutdown delay (seconds) before issuing QUIT on Stop
	shutdownDelay := 2
	if shutdownDelayStr := c.PostForm("shutdown_delay_seconds"); shutdownDelayStr != "" {
		if val, convErr := strconv.Atoi(shutdownDelayStr); convErr == nil && val >= 0 && val <= 3600 {
			shutdownDelay = val
		} else {
			h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{
				"error": "Invalid shutdown delay (0-3600)",
				"form":  formState,
			})
			return
		}
	}
	formState["restart_delay_seconds"] = fmt.Sprintf("%d", restartDelay)
	worldID := h.manager.ResolveWorldID(world, beta)
	if worldID == "" {
		worldID = world
	}

	cfg := &models.ServerConfig{
		Name:                  name,
		World:                 world,
		WorldID:               worldID,
		Language:              "",
		StartLocation:         startLocation,
		StartCondition:        startCondition,
		Difficulty:            difficulty,
		Port:                  port,
		Password:              password,
		AuthSecret:            authSecret,
		MaxClients:            maxClients,
		SaveInterval:          saveInterval,
		Visible:               serverVisible,
		Beta:                  beta,
		AutoStart:             autoStart,
		AutoUpdate:            autoUpdate,
		AutoSave:              autoSave,
		AutoPause:             autoPause,
		PlayerSaves:           playerSaves,
		MaxAutoSaves:          maxAutoSaves,
		MaxQuickSaves:         maxQuickSaves,
		DeleteSkeletonOnDecay: deleteSkeletonOnDecay,
		UseSteamP2P:           useSteamP2P,
		DisconnectTimeout:     disconnectTimeout,
		RestartDelaySeconds:   restartDelay,
		ShutdownDelaySeconds:  shutdownDelay,
		WelcomeMessage:        welcomeMessage,
	}

	// Set default language based on selected channel
	// Prefer English when available; otherwise fall back to the first available language
	langs := h.manager.GetLanguagesForVersion(beta)
	if len(langs) > 0 {
		// Default to first, but upgrade to English if present
		cfg.Language = langs[0]
		for _, l := range langs {
			if strings.EqualFold(l, "english") {
				cfg.Language = l
				break
			}
		}
	}

	newServer, err := h.manager.AddServer(cfg)
	if err != nil {
		h.renderNewServerForm(c, http.StatusInternalServerError, username, gin.H{
			"error": "Failed to create server",
			"form":  formState,
		})
		return
	}

	if err := newServer.Deploy(); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Initial deploy for server %s (ID: %d) failed: %v", newServer.Name, newServer.ID, err))
	}

	// If Player Saves preference was selected on create, pass it via a query param so the
	// server page can persist it to localStorage for this server id.
	if playerSaves {
		c.Redirect(http.StatusFound, fmt.Sprintf("/server/%d?player_saves=1", newServer.ID))
		return
	}
	c.Redirect(http.StatusFound, fmt.Sprintf("/server/%d", newServer.ID))
}
