package handlers

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

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
			"is_admin":     client.IsAdmin,
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
			"is_admin":       client.IsAdmin,
		})
	}

	chatMessages := make([]gin.H, 0, len(s.Chat))
	for _, entry := range s.Chat {
		if entry == nil {
			continue
		}
		timestamp := ""
		if !entry.Datetime.IsZero() {
			timestamp = entry.Datetime.Format(time.RFC3339)
		}
		chatMessages = append(chatMessages, gin.H{
			"player":  entry.Name,
			"message": entry.Message,
			"time":    timestamp,
		})
	}

	lastLine := strings.TrimSpace(s.LastLogLine)

	// Banned list (from Blacklist.txt cross-referenced with players.log for names)
	banned := make([]gin.H, 0)
	for _, be := range s.BannedEntries() {
		banned = append(banned, gin.H{
			"name":     be.Name,
			"steam_id": be.SteamID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"id":             s.ID,
		"name":           s.Name,
		"running":        s.IsRunning(),
		"starting":       s.Starting,
		"port":           s.Port,
		"player_count":   len(liveClients),
		"players":        players,
		"last_log_line":  lastLine,
		"server_started": started,
		"server_saved":   saved,
		"paused":         s.Paused,
		"clients":        history,
		"chat_messages":  chatMessages,
		"banned":         banned,
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
	// If requested via HTMX for HTML swap, return a single server card fragment
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		// Trigger a stats refresh on the page (stats-grid listens to 'refresh')
		c.Header("HX-Trigger", "refresh")
		c.Header("X-Toast-Type", "success")
		c.Header("X-Toast-Title", "Server Started")
		c.Header("X-Toast-Message", s.Name+" is starting…")
		c.HTML(http.StatusOK, "server_card.html", s)
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Started")
	c.Header("X-Toast-Message", s.Name+" is starting…")
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
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		// Trigger a stats refresh on the page (stats-grid listens to 'refresh')
		c.Header("HX-Trigger", "refresh")
		c.Header("X-Toast-Type", "success")
		c.Header("X-Toast-Title", "Server Stopped")
		c.Header("X-Toast-Message", s.Name+" has been stopped.")
		c.HTML(http.StatusOK, "server_card.html", s)
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Stopped")
	c.Header("X-Toast-Message", s.Name+" has been stopped.")
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (h *ManagerHandlers) APIServerDelete(c *gin.Context) {
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

	// Stop if running
	if s.IsRunning() {
		s.Stop()
	}

	// Delete server directory if paths available
	if s.Paths != nil {
		if err := s.Paths.DeleteServerDirectory(s.ID, s.Logger); err != nil {
			if s.Logger != nil {
				s.Logger.Write("Failed to delete server directory: " + err.Error())
			}
		}
	} else if h.manager.Paths != nil {
		if err := h.manager.Paths.DeleteServerDirectory(s.ID, s.Logger); err != nil {
			if s.Logger != nil {
				s.Logger.Write("Failed to delete server directory: " + err.Error())
			}
		}
	}

	// Remove from manager list
	for i, srv := range h.manager.Servers {
		if srv.ID == serverID {
			h.manager.Servers = append(h.manager.Servers[:i], h.manager.Servers[i+1:]...)
			break
		}
	}
	h.manager.Save()

	// If requested via HTMX for HTML swap, return empty body and trigger stats refresh + toast
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Header("HX-Trigger", "refresh")
		c.Header("X-Toast-Type", "success")
		c.Header("X-Toast-Title", "Server Deleted")
		c.Header("X-Toast-Message", s.Name+" has been deleted.")
		c.Status(http.StatusOK)
		return
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Deleted")
	c.Header("X-Toast-Message", s.Name+" has been deleted.")
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
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

// APIServerCommand accepts a command to be sent to the running server process via stdin.
// JSON body: { "type": "console"|"chat", "payload": "..." }
func (h *ManagerHandlers) APIServerCommand(c *gin.Context) {
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

	// Parse JSON body
	var req struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Command Failed")
		c.Header("X-Toast-Message", "Invalid request body.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Payload) == "" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Command Failed")
		c.Header("X-Toast-Message", "Empty command.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty command"})
		return
	}

	if err := s.SendCommand(req.Type, req.Payload); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Command Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Command Sent")
	c.Header("X-Toast-Message", "Your command was sent to the server.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
	totalServers := h.manager.ServerCount()
	activeServers := h.manager.ServerCountActive()
	totalPlayers := h.manager.GetTotalPlayers()

	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.HTML(http.StatusOK, "stats.html", gin.H{
			"totalServers":  totalServers,
			"activeServers": activeServers,
			"totalPlayers":  totalPlayers,
			"systemHealth":  "100%",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"totalServers":  totalServers,
		"activeServers": activeServers,
		"totalPlayers":  totalPlayers,
		"systemHealth":  "100%",
	})
}

func (h *ManagerHandlers) APIServers(c *gin.Context) {
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.HTML(http.StatusOK, "server_cards.html", gin.H{
			"servers": h.manager.Servers,
		})
		return
	}
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
