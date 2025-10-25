package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"sdsm/internal/middleware"
	"sdsm/internal/models"

	"github.com/gin-gonic/gin"
)

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

	isAsync := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json") ||
		strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")

	switch {
	case c.PostForm("start") != "":
		s.Start()
	case c.PostForm("restart") != "":
		go s.Restart()
	case c.PostForm("stop") != "":
		s.Stop()
	case c.PostForm("deploy") != "":
		if err := s.Deploy(); err != nil {
			errMsg := fmt.Sprintf("Deploy failed: %v", err)
			if isAsync {
				c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
			} else {
				h.renderServerPage(c, http.StatusInternalServerError, s, username, errMsg)
			}
			return
		}
	case c.PostForm("update_server") != "":
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
	case c.PostForm("delete") != "":
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
		originalBeta := s.Beta

		if name := middleware.SanitizeString(c.PostForm("name")); name != "" {
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

		if s.Beta != originalBeta {
			h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) game version changed; redeploying...", s.Name, s.ID))
			if err := s.Deploy(); err != nil {
				h.renderServerPage(c, http.StatusInternalServerError, s, username, fmt.Sprintf("Redeploy failed: %v", err))
				return
			}
		}

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

	name := middleware.SanitizeString(c.PostForm("name"))
	world := middleware.SanitizeString(c.PostForm("world"))
	startLocation := middleware.SanitizeString(c.PostForm("start_location"))
	startCondition := middleware.SanitizeString(c.PostForm("start_condition"))
	difficulty := middleware.SanitizeString(c.PostForm("difficulty"))
	password := c.PostForm("password")
	authSecret := c.PostForm("auth_secret")

	if !h.manager.IsServerNameAvailable(name, -1) {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Server name already exists. Please choose a unique name."})
		return
	}

	port, err := middleware.ValidatePort(c.PostForm("port"))
	if err != nil {
		h.renderNewServerForm(c, http.StatusBadRequest, username, gin.H{"error": "Invalid port number"})
		return
	}

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

	restartDelay := models.DefaultRestartDelaySeconds
	if restartDelayStr := c.PostForm("restart_delay_seconds"); restartDelayStr != "" {
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

	if err := newServer.Deploy(); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Initial deploy for server %s (ID: %d) failed: %v", newServer.Name, newServer.ID, err))
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/server/%d", newServer.ID))
}
