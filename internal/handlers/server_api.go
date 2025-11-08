package handlers

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"archive/zip"
	"net/http"
	"os"
	"io"
    "mime/multipart"
	"path/filepath"
	"sdsm/internal/middleware"
	"sdsm/internal/models"
	"sdsm/internal/utils"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// --- Realtime update helpers ---

func (h *ManagerHandlers) broadcastServerStatus(s *models.Server) {
	if h == nil || h.hub == nil || s == nil {
		return
	}
	payload := map[string]any{
		"type":     "server_status",
		"serverId": s.ID,
		"status": map[string]any{
			"name":        s.Name,
			"port":        s.Port,
			"world":       s.World,
			"playerCount": s.ClientCount(),
			"maxPlayers":  s.MaxClients,
			"running":     s.IsRunning(),
			"starting":    s.Starting,
			"stopping":    s.Stopping,
			"stopping_eta": func() int {
				if s.Stopping && !s.StoppingEnds.IsZero() {
					remaining := int(time.Until(s.StoppingEnds).Seconds())
					if remaining < 0 { return 0 }
					return remaining
				}
				return 0
			}(),
			"paused":      s.Paused,
			"storming":    s.Storming,
		},
	}
	if msg, err := json.Marshal(payload); err == nil {
		h.hub.Broadcast(msg)
	}
}

// ServerClientsGET issues a CLIENTS command and returns current live clients after brief delay.
func (h *ManagerHandlers) ServerClientsGET(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":"invalid server id"}); return }
	s := h.manager.ServerByID(serverID)
	if s == nil { c.JSON(http.StatusNotFound, gin.H{"error":"server not found"}); return }
	if !s.IsRunning() { c.JSON(http.StatusConflict, gin.H{"error":"server not running"}); return }
	// Issue command and return immediately; log handlers will reconcile clients asynchronously.
	if err := s.SendCommand("console", "CLIENTS"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error":"failed to issue CLIENTS"}); return
	}
	c.JSON(http.StatusAccepted, gin.H{"status":"queued"})
}

// shutdownDelayForServer mirrors the server's shutdown delay logic for handler usage.
func shutdownDelayForServer(s *models.Server) time.Duration {
	if s == nil { return 0 }
	if s.ShutdownDelaySeconds > 0 { return time.Duration(s.ShutdownDelaySeconds) * time.Second }
	if s.ShutdownDelaySeconds == 0 { return 0 }
	return 2 * time.Second
}

func (h *ManagerHandlers) broadcastStats() {
	if h == nil || h.hub == nil {
		return
	}
	stats := map[string]int{
		"totalServers":  h.manager.ServerCount(),
		"activeServers": h.manager.ServerCountActive(),
		"totalPlayers":  h.manager.GetTotalPlayers(),
	}
	payload := map[string]any{
		"type":  "stats_update",
		"stats": stats,
	}
	if msg, err := json.Marshal(payload); err == nil {
		h.hub.Broadcast(msg)
	}
}

func (h *ManagerHandlers) broadcastServersChanged() {
	if h == nil || h.hub == nil {
		return
	}
	payload := map[string]any{"type": "servers_changed"}
	if msg, err := json.Marshal(payload); err == nil {
		h.hub.Broadcast(msg)
	}
}

func (h *ManagerHandlers) APIServerStatus(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC: operators must be assigned to the server
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
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

	resp := gin.H{
		"id":             s.ID,
		"name":           s.Name,
		"running":        s.IsRunning(),
		"starting":       s.Starting,
		"stopping":       s.Stopping,
		"stopping_eta": func() int {
			if s.Stopping && !s.StoppingEnds.IsZero() {
				rem := int(time.Until(s.StoppingEnds).Seconds())
				if rem < 0 { return 0 }
				return rem
			}
			return 0
		}(),
		"port":           s.Port,
		"max_clients":    s.MaxClients,
		"player_count":   len(liveClients),
		"players":        players,
		"last_log_line":  lastLine,
		"server_started": started,
		"server_saved":   saved,
		"paused":         s.Paused,
		"storming":       s.Storming,
		"clients":        history,
		"chat_messages":  chatMessages,
		"banned":         banned,
	}
	if s.PendingSavePurge {
		resp["pending_save_purge"] = true
	}
	if strings.TrimSpace(s.LastError) != "" {
		resp["last_error"] = s.LastError
		if s.LastErrorAt != nil {
			resp["last_error_at"] = s.LastErrorAt.Format(time.RFC3339)
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (h *ManagerHandlers) APIServerStart(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// If server is currently in a delayed shutdown window, treat Start as a cancellation request.
	if s.Running && s.Stopping {
		if s.CancelStop() {
			// Broadcast updated state (still running, stopping cleared)
			h.broadcastServerStatus(s)
			h.broadcastStats()
			if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
				c.Header("HX-Trigger", "refresh")
				c.Header("X-Toast-Type", "success")
				c.Header("X-Toast-Title", "Shutdown Canceled")
				c.Header("X-Toast-Message", s.Name+" will keep running.")
				c.HTML(http.StatusOK, "server_card.html", s)
				return
			}
			c.Header("X-Toast-Type", "success")
			c.Header("X-Toast-Title", "Shutdown Canceled")
			c.Header("X-Toast-Message", s.Name+" will keep running.")
			c.JSON(http.StatusOK, gin.H{"status": "shutdown_canceled"})
			return
		}
	}

	s.Start()
	// Broadcast status + stats for realtime dashboards
	h.broadcastServerStatus(s)
	h.broadcastStats()
	// If requested via HTMX for HTML swap, return a single server card fragment
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		// Trigger a stats refresh on the page (stats-grid listens to 'refresh')
		c.Header("HX-Trigger", "refresh")
		c.Header("X-Toast-Type", "success")
		c.Header("X-Toast-Title", "Server Started")
		c.Header("X-Toast-Message", s.Name+" is starting...")
		c.HTML(http.StatusOK, "server_card.html", s)
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Started")
	c.Header("X-Toast-Message", s.Name+" is starting...")
	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

func (h *ManagerHandlers) APIServerStop(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Use asynchronous stop so UI can show 'Stopping' state and allow cancellation.
	s.StopAsync(func(srv *models.Server) {
		// Broadcast each significant state change (scheduled, canceled, final stopped)
		h.broadcastServerStatus(srv)
		h.broadcastStats()
	})

	// Initial response reflects scheduling (server still running for delayed stops)
	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Header("HX-Trigger", "refresh")
		c.Header("X-Toast-Type", "info")
		c.Header("X-Toast-Title", "Shutdown Scheduled")
		if shutdownDelayForServer(s) > 0 {
			// Local timeframe formatter (duplicate of models.Server.formatTimeframe logic, kept unexported there)
			formatTf := func(d time.Duration) string {
				secs := int(d.Seconds())
				if secs < 60 { if secs == 1 { return "1 second" }; return fmt.Sprintf("%d seconds", secs) }
				m := secs / 60; rem := secs % 60
				if rem == 0 { if m == 1 { return "1 minute" }; return fmt.Sprintf("%d minutes", m) }
				if m == 1 { if rem == 1 { return "1 minute 1 second" }; return fmt.Sprintf("1 minute %d seconds", rem) }
				if rem == 1 { return fmt.Sprintf("%d minutes 1 second", m) }
				return fmt.Sprintf("%d minutes %d seconds", m, rem)
			}
			c.Header("X-Toast-Message", fmt.Sprintf("%s will shut down in %s (click Keep Running to cancel).", s.Name, formatTf(shutdownDelayForServer(s))))
		} else {
			c.Header("X-Toast-Message", s.Name+" is stopping now.")
		}
		c.HTML(http.StatusOK, "server_card.html", s)
		return
	}
	c.Header("X-Toast-Type", "info")
	c.Header("X-Toast-Title", "Shutdown Scheduled")
	if shutdownDelayForServer(s) > 0 {
		formatTf := func(d time.Duration) string {
			secs := int(d.Seconds())
			if secs < 60 { if secs == 1 { return "1 second" }; return fmt.Sprintf("%d seconds", secs) }
			m := secs / 60; rem := secs % 60
			if rem == 0 { if m == 1 { return "1 minute" }; return fmt.Sprintf("%d minutes", m) }
			if m == 1 { if rem == 1 { return "1 minute 1 second" }; return fmt.Sprintf("1 minute %d seconds", rem) }
			if rem == 1 { return fmt.Sprintf("%d minutes 1 second", m) }
			return fmt.Sprintf("%d minutes %d seconds", m, rem)
		}
		d := shutdownDelayForServer(s)
		c.Header("X-Toast-Message", fmt.Sprintf("%s will shut down in %s.", s.Name, formatTf(d)))
		c.JSON(http.StatusOK, gin.H{"status": "scheduled", "eta_seconds": int(d.Seconds())})
		return
	}
	c.Header("X-Toast-Message", s.Name+" is stopping now.")
	c.JSON(http.StatusOK, gin.H{"status": "stopping"})
}

// APIServerRestart restarts a server asynchronously
func (h *ManagerHandlers) APIServerRestart(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	go s.Restart()
	// Early broadcast to indicate restarting/starting state; follow-ups will arrive via polling
	h.broadcastServerStatus(s)
	h.broadcastStats()
	c.Header("X-Toast-Type", "info")
	c.Header("X-Toast-Title", "Server Restarting")
	c.Header("X-Toast-Message", s.Name+" is restarting...")
	c.JSON(http.StatusOK, gin.H{"status": "restarting"})
}

func (h *ManagerHandlers) APIServerDelete(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
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

	// Broadcast that server roster changed and stats should update
	h.broadcastServersChanged()
	h.broadcastStats()

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

	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.String(http.StatusForbidden, "forbidden")
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.String(http.StatusNotFound, "Server not found")
		return
	}

	var logPath string
	// Optional specific log file by name (must be in server logs dir and end with .log)
	if name := strings.TrimSpace(c.Query("name")); name != "" {
		base := filepath.Base(name)
		if !strings.HasSuffix(strings.ToLower(base), ".log") {
			c.String(http.StatusBadRequest, "invalid log file")
			return
		}
		var logsDir string
		if s.Paths != nil {
			logsDir = s.Paths.ServerLogsDir(s.ID)
		} else if h.manager.Paths != nil {
			logsDir = h.manager.Paths.ServerLogsDir(s.ID)
		}
		if logsDir == "" {
			c.String(http.StatusNotFound, "Logs directory not available")
			return
		}
		logPath = filepath.Join(logsDir, base)
	} else {
		// Default to combined stdout/stderr capture
		if s.Paths != nil {
			logPath = s.Paths.ServerOutputFile(s.ID)
		} else if h.manager.Paths != nil {
			logPath = h.manager.Paths.ServerOutputFile(s.ID)
		}
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

// APIServerLogsList returns a JSON array of available *.log files in the server's logs directory.
func (h *ManagerHandlers) APIServerLogsList(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC: operators must be assigned to the server
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var logsDir string
	if s.Paths != nil {
		logsDir = s.Paths.ServerLogsDir(s.ID)
	} else if h.manager.Paths != nil {
		logsDir = h.manager.Paths.ServerLogsDir(s.ID)
	}
	if logsDir == "" {
		c.JSON(http.StatusOK, gin.H{"files": []string{}})
		return
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"files": []string{}})
		return
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".log") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// APIServerUpdateSettings updates server startup/config parameters (admin-only for many fields).
// Accepts either application/json or x-www-form-urlencoded similar to ServerPOST("update").
func (h *ManagerHandlers) APIServerUpdateSettings(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	role := c.GetString("role")
	if role != "admin" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Permission Denied")
		c.Header("X-Toast-Message", "Admin privileges required to change startup parameters.")
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Extract inputs from JSON or form
	var body map[string]string
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var tmp map[string]any
		if err := c.ShouldBindJSON(&tmp); err == nil {
			body = make(map[string]string, len(tmp))
			for k, v := range tmp {
				body[k] = strings.TrimSpace(fmt.Sprint(v))
			}
		}
	}
	if body == nil {
		body = map[string]string{}
		_ = c.Request.ParseForm()
		for k, v := range c.Request.PostForm {
			if len(v) > 0 {
				body[k] = strings.TrimSpace(v[0])
			}
		}
	}

	// Capture originals to decide on save purge flag and redeploy
	originalBeta := s.Beta
	origWorld := s.World
	origStartLoc := s.StartLocation
	origStartCond := s.StartCondition

	if name := middleware.SanitizeString(body["name"]); name != "" {
		if !h.manager.IsServerNameAvailable(name, s.ID) {
			c.Header("X-Toast-Type", "error")
			c.Header("X-Toast-Title", "Update Failed")
			c.Header("X-Toast-Message", "Server name already exists. Please choose a unique name.")
			c.JSON(http.StatusBadRequest, gin.H{"error": "name not available"})
			return
		}
		s.Name = name
	}
	if world := middleware.SanitizeString(body["world"]); world != "" {
		s.World = world
	}
	if startLocation := middleware.SanitizeString(body["start_location"]); startLocation != "" {
		s.StartLocation = startLocation
	}
	if startCondition := middleware.SanitizeString(body["start_condition"]); startCondition != "" {
		s.StartCondition = startCondition
	}
	if difficulty := middleware.SanitizeString(body["difficulty"]); difficulty != "" {
		s.Difficulty = difficulty
	}

	// Track port change to keep SCONPort in sync (SCON uses game port + 1)
	originalPort := s.Port
	if portStr := body["port"]; portStr != "" {
		if port, err := middleware.ValidatePort(portStr); err == nil {
			if h.manager.IsPortAvailable(port, s.ID) {
				s.Port = port
			} else {
				suggestedPort := h.manager.GetNextAvailablePort(port)
				c.Header("X-Toast-Type", "error")
				c.Header("X-Toast-Title", "Update Failed")
				c.Header("X-Toast-Message", fmt.Sprintf("Port %d is not available. Try %d.", port, suggestedPort))
				c.JSON(http.StatusBadRequest, gin.H{"error": "port not available", "suggested": suggestedPort})
				return
			}
		}
	}

	// If the game port changed, recompute SCONPort to follow convention
	if s.Port > 0 && s.Port != originalPort {
		s.SCONPort = s.Port + 1
	}

	if v := body["password"]; v != "" {
		s.Password = v
	}
	if v := body["auth_secret"]; v != "" {
		s.AuthSecret = v
	}

	// Welcome Message (optional, allow clearing). Sanitize and clamp length.
	if wm, ok := body["welcome_message"]; ok {
		clean := middleware.SanitizeString(wm)
		clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r", " "), "\n", " ")
		clean = strings.TrimSpace(clean)
		if len(clean) > 300 {
			clean = clean[:300]
		}
		s.WelcomeMessage = clean
	}
	if wbm, ok := body["welcome_back_message"]; ok {
		clean := middleware.SanitizeString(wbm)
		clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r", " "), "\n", " ")
		clean = strings.TrimSpace(clean)
		if len(clean) > 300 { clean = clean[:300] }
		s.WelcomeBackMessage = clean
	}
	if wd, ok := body["welcome_delay_seconds"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(wd)); err == nil && n >= 0 && n <= 600 {
			s.WelcomeDelaySeconds = n
		}
	}

	if maxClientsStr := body["max_clients"]; maxClientsStr != "" {
		if maxClients, err := strconv.Atoi(maxClientsStr); err == nil && maxClients >= 1 && maxClients <= 100 {
			s.MaxClients = maxClients
		}
	}
	if saveIntervalStr := body["save_interval"]; saveIntervalStr != "" {
		if saveInterval, err := strconv.Atoi(saveIntervalStr); err == nil && saveInterval >= 60 && saveInterval <= 3600 {
			s.SaveInterval = saveInterval
		}
	}
	if restartDelayStr := body["restart_delay_seconds"]; restartDelayStr != "" {
		if restartDelay, err := strconv.Atoi(restartDelayStr); err == nil && restartDelay >= 0 && restartDelay <= 3600 {
			s.RestartDelaySeconds = restartDelay
		}
	}
	if shutdownDelayStr := body["shutdown_delay_seconds"]; shutdownDelayStr != "" {
		if shutdownDelay, err := strconv.Atoi(shutdownDelayStr); err == nil && shutdownDelay >= 0 && shutdownDelay <= 3600 {
			s.ShutdownDelaySeconds = shutdownDelay
		}
	}
	if v := strings.TrimSpace(body["max_auto_saves"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			s.MaxAutoSaves = n
		}
	}
	if v := strings.TrimSpace(body["max_quick_saves"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			s.MaxQuickSaves = n
		}
	}
	s.DeleteSkeletonOnDecay = body["delete_skeleton_on_decay"] == "on" || body["delete_skeleton_on_decay"] == "true" || body["delete_skeleton_on_decay"] == "1"
	s.UseSteamP2P = body["use_steam_p2p"] == "on" || body["use_steam_p2p"] == "true" || body["use_steam_p2p"] == "1"
	if v := strings.TrimSpace(body["disconnect_timeout"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1000 && n <= 600000 {
			s.DisconnectTimeout = n
		}
	}

	s.Visible = body["server_visible"] == "on" || body["server_visible"] == "true" || body["server_visible"] == "1"
	s.Beta = body["beta"] == "true"
	s.AutoStart = body["auto_start"] == "on" || body["auto_start"] == "true" || body["auto_start"] == "1"
	s.AutoUpdate = body["auto_update"] == "on" || body["auto_update"] == "true" || body["auto_update"] == "1"
	s.AutoSave = body["auto_save"] == "on" || body["auto_save"] == "true" || body["auto_save"] == "1"
	s.AutoPause = body["auto_pause"] == "on" || body["auto_pause"] == "true" || body["auto_pause"] == "1"
	// Allow toggling PlayerSaves via API as part of settings
	if pv, ok := body["player_saves"]; ok {
		v := strings.TrimSpace(pv)
		s.PlayerSaves = v == "on" || v == "true" || v == "1"
	}
	s.WorldID = h.manager.ResolveWorldID(s.World, s.Beta)

	// Core change flag
	coreChanged := (strings.TrimSpace(origWorld) != strings.TrimSpace(s.World)) || (strings.TrimSpace(origStartLoc) != strings.TrimSpace(s.StartLocation)) || (strings.TrimSpace(origStartCond) != strings.TrimSpace(s.StartCondition))
	if coreChanged {
		s.PendingSavePurge = true
		if s.Logger != nil {
			s.Logger.Write("Core start parameters changed; pending save purge flagged (stub, no deletion yet)")
		}
	}

	if s.Beta != originalBeta {
		h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) game version changed; redeploying...", s.Name, s.ID))
		if err := s.Deploy(); err != nil {
			c.Header("X-Toast-Type", "error")
			c.Header("X-Toast-Title", "Redeploy Failed")
			c.Header("X-Toast-Message", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) configuration updated.", s.Name, s.ID))
	h.manager.Save()

	// Broadcast realtime update to dashboard clients if hub is available
	if h.hub != nil {
		payload := map[string]any{
			"type":     "server_status",
			"serverId": s.ID,
			"status": map[string]any{
				"name":        s.Name,
				"port":        s.Port,
				"world":       s.World,
				"playerCount": len(s.LiveClients()),
				"maxPlayers":  s.MaxClients,
				"running":     s.IsRunning(),
				"starting":    s.Starting,
				"paused":      s.Paused,
			},
		}
		if msg, err := json.Marshal(payload); err == nil {
			h.hub.Broadcast(msg)
		}
		// Also broadcast updated stats after a settings change (e.g., max players change)
		h.broadcastStats()
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Settings Updated")
	c.Header("X-Toast-Message", "Startup parameters saved.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerSetLanguage updates only the language setting (no redeploy here).
func (h *ManagerHandlers) APIServerSetLanguage(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var req struct {
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Language) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language required"})
		return
	}
	allowed := h.manager.GetLanguagesForVersion(s.Beta)
	ok := false
	for _, a := range allowed {
		if strings.EqualFold(a, req.Language) {
			ok = true
			break
		}
	}
	if !ok && len(allowed) > 0 {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Invalid Language")
		c.Header("X-Toast-Message", "Selected language is not available for this version.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid language"})
		return
	}
	s.Language = req.Language
	h.manager.Save()
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Language Updated")
	c.Header("X-Toast-Message", fmt.Sprintf("Language set to %s", req.Language))
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerUpdateServerFiles starts server files redeploy/update
func (h *ManagerHandlers) APIServerUpdateServerFiles(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
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
}

// APIServersCreate creates a new server (admin only). Expects JSON body mapping fields of ServerConfig.
func (h *ManagerHandlers) APIServersCreate(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	// Support both JSON and x-www-form-urlencoded bodies
	var req struct {
		Name                  string `json:"name"`
		World                 string `json:"world"`
		StartLocation         string `json:"start_location"`
		StartCondition        string `json:"start_condition"`
		Difficulty            string `json:"difficulty"`
		Port                  int    `json:"port"`
		MaxClients            int    `json:"max_clients"`
		Password              string `json:"password"`
		AuthSecret            string `json:"auth_secret"`
		SaveInterval          int    `json:"save_interval"`
		RestartDelaySeconds   int    `json:"restart_delay_seconds"`
		ShutdownDelaySeconds  int    `json:"shutdown_delay_seconds"`
		Beta                  bool   `json:"beta"`
		AutoStart             bool   `json:"auto_start"`
		AutoUpdate            bool   `json:"auto_update"`
		AutoSave              bool   `json:"auto_save"`
		AutoPause             bool   `json:"auto_pause"`
		PlayerSaves           bool   `json:"player_saves"`
		MaxAutoSaves          int    `json:"max_auto_saves"`
		MaxQuickSaves         int    `json:"max_quick_saves"`
		DeleteSkeletonOnDecay bool   `json:"delete_skeleton_on_decay"`
		UseSteamP2P           bool   `json:"use_steam_p2p"`
		DisconnectTimeout     int    `json:"disconnect_timeout"`
		Visible               bool   `json:"server_visible"`
	}
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
	} else {
		// Form-encoded fallback
		_ = c.Request.ParseForm()
		req.Name = middleware.SanitizeString(c.PostForm("name"))
		req.World = middleware.SanitizeString(c.PostForm("world"))
		req.StartLocation = middleware.SanitizeString(c.PostForm("start_location"))
		req.StartCondition = middleware.SanitizeString(c.PostForm("start_condition"))
		req.Difficulty = middleware.SanitizeString(c.PostForm("difficulty"))
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("port"))); err == nil { req.Port = v }
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("max_clients"))); err == nil { req.MaxClients = v }
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("save_interval"))); err == nil { req.SaveInterval = v }
		if v := strings.TrimSpace(c.PostForm("restart_delay_seconds")); v != "" { if n, err := strconv.Atoi(v); err == nil { req.RestartDelaySeconds = n } }
		if v := strings.TrimSpace(c.PostForm("shutdown_delay_seconds")); v != "" { if n, err := strconv.Atoi(v); err == nil { req.ShutdownDelaySeconds = n } }
		req.Password = c.PostForm("password")
		req.AuthSecret = c.PostForm("auth_secret")
		// Booleans: interpret on/true/1
		parseBool := func(s string) bool { s = strings.TrimSpace(strings.ToLower(s)); return s == "on" || s == "true" || s == "1" }
		req.Beta = strings.TrimSpace(c.PostForm("beta")) == "true"
		req.AutoStart = parseBool(c.PostForm("auto_start"))
		req.AutoUpdate = parseBool(c.PostForm("auto_update"))
		req.AutoSave = parseBool(c.PostForm("auto_save"))
		req.AutoPause = parseBool(c.PostForm("auto_pause"))
		req.PlayerSaves = parseBool(c.PostForm("player_saves"))
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("max_auto_saves"))); err == nil { req.MaxAutoSaves = v }
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("max_quick_saves"))); err == nil { req.MaxQuickSaves = v }
		req.DeleteSkeletonOnDecay = parseBool(c.PostForm("delete_skeleton_on_decay"))
		req.UseSteamP2P = parseBool(c.PostForm("use_steam_p2p"))
		if v, err := strconv.Atoi(strings.TrimSpace(c.PostForm("disconnect_timeout"))); err == nil { req.DisconnectTimeout = v }
		req.Visible = parseBool(c.PostForm("server_visible"))
	}

	// Validation similar to NewServerPOST
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Server name is required."})
		return
	}
	if strings.TrimSpace(req.World) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "World selection is required."})
		return
	}
	if strings.TrimSpace(req.StartLocation) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start location is required."})
		return
	}
	if strings.TrimSpace(req.StartCondition) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start condition is required."})
		return
	}
	if strings.TrimSpace(req.Difficulty) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Difficulty selection is required."})
		return
	}
	if !h.manager.IsServerNameAvailable(req.Name, -1) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Server name '%s' already exists.", req.Name)})
		return
	}
	if _, err := middleware.ValidatePort(fmt.Sprintf("%d", req.Port)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid port"})
		return
	}
	if !h.manager.IsPortAvailable(req.Port, -1) {
		suggested := h.manager.GetNextAvailablePort(req.Port)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Port %d not available", req.Port), "suggested": suggested})
		return
	}
	if req.MaxClients < 1 || req.MaxClients > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid max players (1-100)"})
		return
	}
	if req.SaveInterval < 60 || req.SaveInterval > 3600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid save interval (60-3600)"})
		return
	}
	if req.RestartDelaySeconds < 0 || req.RestartDelaySeconds > 3600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid restart delay (0-3600)"})
		return
	}
	if req.ShutdownDelaySeconds < 0 || req.ShutdownDelaySeconds > 3600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid shutdown delay (0-3600)"})
		return
	}

	worldID := h.manager.ResolveWorldID(req.World, req.Beta)
	if worldID == "" {
		worldID = req.World
	}

	cfg := &models.ServerConfig{
		Name:                  req.Name,
		World:                 req.World,
		WorldID:               worldID,
		Language:              "",
		StartLocation:         req.StartLocation,
		StartCondition:        req.StartCondition,
		Difficulty:            req.Difficulty,
		Port:                  req.Port,
		Password:              req.Password,
		AuthSecret:            req.AuthSecret,
		MaxClients:            req.MaxClients,
		SaveInterval:          req.SaveInterval,
		Visible:               req.Visible,
		Beta:                  req.Beta,
		AutoStart:             req.AutoStart,
		AutoUpdate:            req.AutoUpdate,
		AutoSave:              req.AutoSave,
		AutoPause:             req.AutoPause,
		PlayerSaves:           req.PlayerSaves,
		MaxAutoSaves:          ifZero(req.MaxAutoSaves, 5),
		MaxQuickSaves:         ifZero(req.MaxQuickSaves, 5),
		DeleteSkeletonOnDecay: req.DeleteSkeletonOnDecay,
		UseSteamP2P:           req.UseSteamP2P,
		DisconnectTimeout:     ifZero(req.DisconnectTimeout, 10000),
		RestartDelaySeconds:   req.RestartDelaySeconds,
		ShutdownDelaySeconds:  req.ShutdownDelaySeconds,
	}

	// Default language selection similar to page flow
	langs := h.manager.GetLanguagesForVersion(req.Beta)
	if len(langs) > 0 {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create server"})
		return
	}
	if err := newServer.Deploy(); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Initial deploy failed for %s (ID:%d): %v", newServer.Name, newServer.ID, err))
	}

	// Broadcast that a new server exists + updated stats
	h.broadcastServersChanged()
	h.broadcastStats()
	h.broadcastServerStatus(newServer)

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Created")
	c.Header("X-Toast-Message", newServer.Name+" created.")
	c.JSON(http.StatusOK, gin.H{"server_id": newServer.ID})
}

func ifZero[T ~int](val T, def T) T {
	if val == 0 {
		return def
	}
	return val
}

// settingsXML holds a subset of Stationeers settings we care about for prefill.
type settingsXML struct {
	XMLName               xml.Name `xml:"Settings"`
	ServerMaxPlayers      *int     `xml:"ServerMaxPlayers"`
	AutoSave              *bool    `xml:"AutoSave"`
	SaveInterval          *int     `xml:"SaveInterval"`
	AutoPauseServer       *bool    `xml:"AutoPauseServer"`
	DeleteSkeletonOnDecay *bool    `xml:"DeleteSkeletonOnDecay"`
	UseSteamP2P           *bool    `xml:"UseSteamP2P"`
	DisconnectTimeout     *int     `xml:"DisconnectTimeout"`
	ServerVisible         *bool    `xml:"ServerVisible"`
	GamePort              *int     `xml:"GamePort"`
	ServerPassword        *string  `xml:"ServerPassword"`
	ServerAuthSecret      *string  `xml:"ServerAuthSecret"`
}

func parseSettingsFromSaveZip(path string) (*settingsXML, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var entry *zip.File
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if name == "settings.xml" {
			entry = f
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("settings.xml not found in save")
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var s settingsXML
	dec := xml.NewDecoder(rc)
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// worldMetaXML represents key fields from world_meta.xml used for prefill.
type worldMetaXML struct {
	XMLName       xml.Name `xml:"WorldMetaData"`
	WorldName     string   `xml:"WorldName"`
	WorldFileName string   `xml:"WorldFileName"`
	GameVersion   string   `xml:"GameVersion"`
}

func parseWorldMetaFromSaveZip(path string) (*worldMetaXML, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var entry *zip.File
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if name == "world_meta.xml" {
			entry = f
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("world_meta.xml not found in save")
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var m worldMetaXML
	dec := xml.NewDecoder(rc)
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// saveUploadToTemp copies a multipart upload to a temporary file and returns its path.
func (h *ManagerHandlers) saveUploadToTemp(c *gin.Context, fh *multipart.FileHeader, pattern string) (string, error) {
	if fh == nil {
		return "", fmt.Errorf("no file header")
	}
	src, err := fh.Open()
	if err != nil {
		if h.manager != nil && h.manager.Log != nil { h.manager.Log.Write("Upload open failed: "+err.Error()) }
		return "", err
	}
	defer src.Close()
	tmpf, err := os.CreateTemp("", pattern)
	if err != nil {
		if h.manager != nil && h.manager.Log != nil { h.manager.Log.Write("Temp file create failed: "+err.Error()) }
		return "", err
	}
	tmp := tmpf.Name()
	_, copyErr := io.Copy(tmpf, src)
	cerr := tmpf.Close()
	if copyErr != nil {
		if h.manager != nil && h.manager.Log != nil { h.manager.Log.Write("Upload copy failed: "+copyErr.Error()) }
		os.Remove(tmp)
		return "", copyErr
	}
	if cerr != nil {
		if h.manager != nil && h.manager.Log != nil { h.manager.Log.Write("Temp file close failed: "+cerr.Error()) }
	}
	return tmp, nil
}

// APIServersAnalyzeSave parses an uploaded .save (multipart, field name "save_file") and
// returns minimal metadata for UI prefill: world and world_file_name (server name).
// Response: { world: string, world_file_name: string }
func (h *ManagerHandlers) APIServersAnalyzeSave(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	file, err := c.FormFile("save_file")
	if err != nil || file == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "save_file (.save) is required"})
		return
	}
	base := strings.ToLower(filepath.Base(file.Filename))
	if !strings.HasSuffix(base, ".save") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid save file; must end with .save"})
		return
	}
	tmp, err := h.saveUploadToTemp(c, file, "analyze-*.save")
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save upload"}); return }
	defer os.Remove(tmp)

	meta, merr := parseWorldMetaFromSaveZip(tmp)
	if merr != nil || meta == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_meta.xml not found in save"})
		return
	}
	// Best-effort parse of settings.xml for defaults
	resp := gin.H{
		"world":           strings.TrimSpace(meta.WorldName),
		"world_file_name": strings.TrimSpace(meta.WorldFileName),
	}
	if settings, perr := parseSettingsFromSaveZip(tmp); perr == nil && settings != nil {
		if settings.GamePort != nil && *settings.GamePort > 0 { resp["port"] = *settings.GamePort }
		if settings.ServerMaxPlayers != nil && *settings.ServerMaxPlayers > 0 { resp["max_clients"] = *settings.ServerMaxPlayers }
		if settings.ServerPassword != nil { resp["password"] = *settings.ServerPassword }
		if settings.ServerAuthSecret != nil { resp["auth_secret"] = *settings.ServerAuthSecret }
		if settings.ServerVisible != nil { resp["server_visible"] = *settings.ServerVisible }
		if settings.AutoSave != nil { resp["auto_save"] = *settings.AutoSave }
		if settings.SaveInterval != nil && *settings.SaveInterval > 0 { resp["save_interval"] = *settings.SaveInterval }
		if settings.AutoPauseServer != nil { resp["auto_pause"] = *settings.AutoPauseServer }
		if settings.DeleteSkeletonOnDecay != nil { resp["delete_skeleton_on_decay"] = *settings.DeleteSkeletonOnDecay }
		if settings.UseSteamP2P != nil { resp["use_steam_p2p"] = *settings.UseSteamP2P }
		if settings.DisconnectTimeout != nil && *settings.DisconnectTimeout > 0 { resp["disconnect_timeout"] = *settings.DisconnectTimeout }
	}
	c.JSON(http.StatusOK, resp)
}

// APIServersCreateFromSave creates a new server using posted fields and an uploaded .save file.
// Multipart form fields mirror APIServersCreate; file field name: "save_file".
func (h *ManagerHandlers) APIServersCreateFromSave(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}

	// Read modifiable fields from form where applicable
	formName := middleware.SanitizeString(c.PostForm("name"))
	difficulty := middleware.SanitizeString(c.PostForm("difficulty"))
	// Non-modifiable (derived) fields
	name := ""
	world := ""
	startLocation := ""
	startCondition := ""
	beta := strings.TrimSpace(c.PostForm("beta")) == "true"
	password := c.PostForm("password")
	authSecret := c.PostForm("auth_secret")
	autoStart := c.PostForm("auto_start") == "on"
	autoUpdate := c.PostForm("auto_update") == "on"
	autoSave := c.PostForm("auto_save") == "on"
	autoPause := c.PostForm("auto_pause") == "on"
	playerSaves := c.PostForm("player_saves") == "on"
	deleteSkeletonOnDecay := c.PostForm("delete_skeleton_on_decay") == "on"
	useSteamP2P := c.PostForm("use_steam_p2p") == "on"
	serverVisible := c.PostForm("server_visible") == "on"
	welcomeMessage := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(middleware.SanitizeString(c.PostForm("welcome_message")), "\r", " "), "\n", " "))
	if len(welcomeMessage) > 300 { welcomeMessage = welcomeMessage[:300] }
	welcomeBackMessage := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(middleware.SanitizeString(c.PostForm("welcome_back_message")), "\r", " "), "\n", " "))
	if len(welcomeBackMessage) > 300 { welcomeBackMessage = welcomeBackMessage[:300] }

	// Numeric fields with defaults
	port, _ := middleware.ValidatePort(c.PostForm("port")) // validate later for availability
	maxClients, _ := strconv.Atoi(c.PostForm("max_clients"))
	if maxClients <= 0 { maxClients = 10 }
	saveInterval, _ := strconv.Atoi(c.PostForm("save_interval"))
	if saveInterval <= 0 { saveInterval = 300 }
	restartDelay := models.DefaultRestartDelaySeconds
	if v := strings.TrimSpace(c.PostForm("restart_delay_seconds")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 3600 { restartDelay = n }
	}
	shutdownDelay := 2
	if v := strings.TrimSpace(c.PostForm("shutdown_delay_seconds")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 3600 { shutdownDelay = n }
	}
	maxAutoSaves := ifZero(parseIntSafe(c.PostForm("max_auto_saves"), 0), 5)
	maxQuickSaves := ifZero(parseIntSafe(c.PostForm("max_quick_saves"), 0), 5)
	disconnectTimeout := ifZero(parseIntSafe(c.PostForm("disconnect_timeout"), 0), 10000)

	// Handle file upload early so we can parse world_meta.xml first
	file, err := c.FormFile("save_file")
	if err != nil || file == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "save_file (.save) is required"})
		return
	}
	base := strings.ToLower(filepath.Base(file.Filename))
	if !strings.HasSuffix(base, ".save") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid save file; must end with .save"})
		return
	}
	// Save to temp path first
	tmp, err := h.saveUploadToTemp(c, file, "upload-*.save")
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save upload"}); return }

	// Primary: parse world_meta.xml from zip (REQUIRED)
	var worldIDForDefaults string
	meta, merr := parseWorldMetaFromSaveZip(tmp)
	if merr != nil || meta == nil {
		os.Remove(tmp)
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_meta.xml not found in save"})
		return
	}
	if w := strings.TrimSpace(meta.WorldName); w != "" { world = w }
	if strings.TrimSpace(formName) != "" {
		name = formName
	} else if wf := strings.TrimSpace(meta.WorldFileName); wf != "" {
		name = middleware.SanitizeString(wf)
	}

	// Secondary: parse settings.xml from zip (best effort) for ancillary settings only.
	if settings, perr := parseSettingsFromSaveZip(tmp); perr == nil && settings != nil {
		// Do NOT override user-provided overrides: port, max players, server visible, password, auth secret
		if settings.AutoSave != nil { autoSave = *settings.AutoSave }
		if settings.SaveInterval != nil && *settings.SaveInterval > 0 { saveInterval = *settings.SaveInterval }
		if settings.AutoPauseServer != nil { autoPause = *settings.AutoPauseServer }
		if settings.DeleteSkeletonOnDecay != nil { deleteSkeletonOnDecay = *settings.DeleteSkeletonOnDecay }
		if settings.UseSteamP2P != nil { useSteamP2P = *settings.UseSteamP2P }
		if settings.DisconnectTimeout != nil && *settings.DisconnectTimeout > 0 { disconnectTimeout = *settings.DisconnectTimeout }
	}

	// Basic validations (after parsing save metadata)
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Server name is required."})
		return
	}
	if !h.manager.IsServerNameAvailable(name, -1) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Server name '%s' already exists.", name)})
		return
	}
	if strings.TrimSpace(world) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "World not found in save metadata."})
		return
	}

	// Choose sensible defaults for start parameters and difficulty, since the save drives state
	worldIDForDefaults = h.manager.ResolveWorldID(world, beta)
	if strings.TrimSpace(worldIDForDefaults) == "" { worldIDForDefaults = world }
	// Default start location/condition from first available options for world
	if locs := h.manager.GetStartLocationsForWorldVersion(worldIDForDefaults, beta); len(locs) > 0 {
		startLocation = locs[0].ID
	}
	if conds := h.manager.GetStartConditionsForWorldVersion(worldIDForDefaults, beta); len(conds) > 0 {
		startCondition = conds[0].ID
	}
	// Default difficulty only if not provided by user
	if strings.TrimSpace(difficulty) == "" {
		diffs := h.manager.GetDifficultiesForVersion(beta)
		if len(diffs) > 0 {
			difficulty = diffs[0]
			for _, d := range diffs { if strings.EqualFold(d, "Normal") { difficulty = d; break } }
		} else {
			difficulty = "Normal"
		}
	}

	// Validate/adjust port
	if port == 0 {
		if p, err := middleware.ValidatePort("26017"); err == nil { port = p }
	}
	if !h.manager.IsPortAvailable(port, -1) {
		port = h.manager.GetNextAvailablePort(port)
	}
	// Validate remaining numeric ranges
	if maxClients < 1 || maxClients > 100 { maxClients = 10 }
	if saveInterval < 60 || saveInterval > 3600 { saveInterval = 300 }

	worldID := h.manager.ResolveWorldID(world, beta)
	if strings.TrimSpace(worldID) == "" { worldID = world }

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
		WelcomeBackMessage:    welcomeBackMessage,
	}

	// Default language selection similar to other create paths
	langs := h.manager.GetLanguagesForVersion(beta)
	if len(langs) > 0 {
		cfg.Language = langs[0]
		for _, l := range langs { if strings.EqualFold(l, "english") { cfg.Language = l; break } }
	}

	newServer, err := h.manager.AddServer(cfg)
	if err != nil {
		os.Remove(tmp)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create server"})
		return
	}

	// Ensure directory exists and move the .save into saves/<ServerName>/
	var savesDir string
	if newServer.Paths != nil { savesDir = newServer.Paths.ServerSavesDir(newServer.ID) } else if h.manager.Paths != nil { savesDir = h.manager.Paths.ServerSavesDir(newServer.ID) }
	if strings.TrimSpace(savesDir) != "" {
		targetDir := filepath.Join(savesDir, newServer.Name)
		_ = os.MkdirAll(targetDir, 0o755)
		safeBase := middleware.SanitizeFilename(file.Filename)
		if !strings.HasSuffix(strings.ToLower(safeBase), ".save") { safeBase = safeBase + ".save" }
		target := filepath.Join(targetDir, filepath.Base(safeBase))
		// If target exists, append timestamp to avoid overwrite
		if _, err := os.Stat(target); err == nil {
			ts := time.Now().Format("20060102-150405")
			nameOnly := strings.TrimSuffix(filepath.Base(safeBase), ".save")
			target = filepath.Join(targetDir, fmt.Sprintf("%s-%s.save", nameOnly, ts))
		}
		if err := os.Rename(tmp, target); err != nil {
			// Fallback to copy then remove temp
			if in, e1 := os.Open(tmp); e1 == nil {
				if out, e2 := os.Create(target); e2 == nil {
					_, _ = io.Copy(out, in)
					out.Close()
				}
				in.Close()
			}
			_ = os.Remove(tmp)
		}
	} else {
		_ = os.Remove(tmp)
	}

	// Best-effort initial deploy
	if err := newServer.Deploy(); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Initial deploy failed for %s (ID:%d): %v", newServer.Name, newServer.ID, err))
	}

	// Broadcast roster + stats
	h.broadcastServersChanged()
	h.broadcastStats()
	h.broadcastServerStatus(newServer)

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Server Created")
	c.Header("X-Toast-Message", newServer.Name+" created from save.")
	c.JSON(http.StatusOK, gin.H{"server_id": newServer.ID})
}

func parseIntSafe(s string, def int) int { if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil { return v }; return def }

// APIServerLogTail streams a chunk of a log starting from a byte offset. It supports:
//   - offset >= 0: read from offset up to 'max' bytes
//   - offset == -1: read the last 'back' bytes from end (or 0 if back not provided)
//
// Returns JSON: { data: string, offset: nextOffset, size: fileSize, reset: bool }
func (h *ManagerHandlers) APIServerLogTail(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	base := filepath.Base(name)
	if !strings.HasSuffix(strings.ToLower(base), ".log") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log file"})
		return
	}

	var logsDir string
	if s.Paths != nil {
		logsDir = s.Paths.ServerLogsDir(s.ID)
	} else if h.manager.Paths != nil {
		logsDir = h.manager.Paths.ServerLogsDir(s.ID)
	}
	if logsDir == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "logs directory not available"})
		return
	}
	logPath := filepath.Join(logsDir, base)

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusOK, gin.H{"data": "", "offset": 0, "size": 0, "reset": false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to open log"})
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to stat log"})
		return
	}
	size := info.Size()

	// Parse query params
	const defaultMax = 65536
	const defaultBack = 8192
	max := defaultMax
	if m := strings.TrimSpace(c.Query("max")); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 && v <= 1_000_000 {
			max = v
		}
	}
	back := defaultBack
	if b := strings.TrimSpace(c.Query("back")); b != "" {
		if v, err := strconv.Atoi(b); err == nil && v >= 0 && v <= 1_000_000 {
			back = v
		}
	}
	offStr := strings.TrimSpace(c.Query("offset"))
	var offset int64 = -1
	if offStr != "" {
		if v, err := strconv.ParseInt(offStr, 10, 64); err == nil {
			offset = v
		}
	}

	var start int64
	var length int64
	reset := false

	switch {
	case offset == -1:
		if int64(back) >= size {
			start = 0
		} else {
			start = size - int64(back)
		}
		length = size - start
		if length > int64(max) {
			start = size - int64(max)
			length = int64(max)
		}
	case offset > size:
		// File was truncated/rotated; read last 'back' bytes
		reset = true
		if int64(back) >= size {
			start = 0
		} else {
			start = size - int64(back)
		}
		length = size - start
		if length > int64(max) {
			start = size - int64(max)
			length = int64(max)
		}
	default:
		start = offset
		rem := size - start
		if rem <= 0 {
			c.JSON(http.StatusOK, gin.H{"data": "", "offset": start, "size": size, "reset": false})
			return
		}
		if rem > int64(max) {
			length = int64(max)
		} else {
			length = rem
		}
	}

	if length < 0 {
		length = 0
	}
	if start < 0 {
		start = 0
	}

	buf := make([]byte, length)
	if length > 0 {
		if _, err := file.ReadAt(buf, start); err != nil {
			// We still return what we could (buf zeroed when unread)
		}
	}

	next := start + length
	c.JSON(http.StatusOK, gin.H{
		"data":   string(buf),
		"offset": next,
		"size":   size,
		"reset":  reset,
	})
}

// APIServerCommand accepts a command to be sent to the running server process via stdin.
// JSON body: { "type": "console"|"chat", "payload": "..." }
// Legacy APIServerCommand removed in favor of explicit endpoints.

// APIServerChat sends a chat message to the server. JSON: { "message": "..." }
func (h *ManagerHandlers) APIServerChat(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// RBAC
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Chat Failed")
		c.Header("X-Toast-Message", "Message is required.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "message required"})
		return
	}
	msg := s.RenderChatMessage(req.Message, nil)
	if err := s.SendCommand("chat", msg); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Chat Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Message Sent")
	c.Header("X-Toast-Message", "Chat message sent.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerConsole sends an arbitrary console command to the server via SCON/stdin.
// JSON: { "command": "..." }
func (h *ManagerHandlers) APIServerConsole(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// RBAC: allow admins, or assigned operators
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var req struct { Command string `json:"command"` }
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Command) == "" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Command Failed")
		c.Header("X-Toast-Message", "Command is required.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "command required"})
		return
	}
	if err := s.SendCommand("console", req.Command); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Command Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Command Sent")
	c.Header("X-Toast-Message", "Command dispatched to server.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerSCONHealth checks if the SCON HTTP endpoint is reachable for a server.
// It performs a lightweight GET request to the /command path and treats any HTTP response
// (2xx-5xx) as "reachable" to distinguish from connection errors. Returns JSON:
// { reachable: bool, status: int, url: string }
func (h *ManagerHandlers) APIServerSCONHealth(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// RBAC
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Determine SCON port using dynamic detection (BepInEx LogOutput), with fallback
	port := s.CurrentSCONPort()
	url := fmt.Sprintf("http://localhost:%d/command", port)

	// Lightweight probe: try GET (expecting 404/405/200). Any response means reachable.
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, reqErr := client.Do(req)
	if reqErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"reachable": false,
			"status":    0,
			"url":       url,
			"error":     reqErr.Error(),
		})
		return
	}
	defer resp.Body.Close()
	c.JSON(http.StatusOK, gin.H{
		"reachable": true,
		"status":    resp.StatusCode,
		"url":       url,
	})
}

// APIServerSave triggers a manual save using the FILE save command.
func (h *ManagerHandlers) APIServerSave(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	if err := s.SendCommand("console", "FILE save"); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Save Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Save Requested")
	c.Header("X-Toast-Message", "Manual save requested.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerSaveAs triggers a manual named save via FILE saveas <name>.
// JSON: { "name": "<filename base without .save>" }
func (h *ManagerHandlers) APIServerSaveAs(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Save As Failed")
		c.Header("X-Toast-Message", "Invalid request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	base := middleware.SanitizeFilename(strings.TrimSpace(req.Name))
	base = strings.TrimSuffix(base, ".save")
	base = strings.TrimSuffix(base, ".SAVE")
	if base == "" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Save As Failed")
		c.Header("X-Toast-Message", "Name is required.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if len(base) > 100 {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Save As Failed")
		c.Header("X-Toast-Message", "Name too long (max 100).")
		c.JSON(http.StatusBadRequest, gin.H{"error": "name too long"})
		return
	}
	if err := s.SendCommand("console", "FILE saveas "+base); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Save As Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Save As Requested")
	c.Header("X-Toast-Message", "Manual save requested.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// resolveLoadPath determines a safe absolute path for a save file given a logical type and filename.
func (h *ManagerHandlers) resolveLoadPath(s *models.Server, saveType, filename string) (string, error) {
	if s == nil || strings.TrimSpace(filename) == "" {
		return "", errors.New("invalid request")
	}
	// Normalize
	t := strings.ToLower(strings.TrimSpace(saveType))
	base := filepath.Base(filename)
	if !strings.HasSuffix(strings.ToLower(base), ".save") {
		return "", errors.New("invalid filename")
	}
	var savesDir string
	if s.Paths != nil {
		savesDir = s.Paths.ServerSavesDir(s.ID)
	} else if h.manager.Paths != nil {
		savesDir = h.manager.Paths.ServerSavesDir(s.ID)
	}
	if strings.TrimSpace(savesDir) == "" {
		return "", errors.New("saves directory unavailable")
	}
	var dir string
	switch t {
	case "auto", "autosave", "autosaves":
		dir = filepath.Join(savesDir, s.Name, "autosave")
	case "quick", "quicksave", "quicksaves":
		dir = filepath.Join(savesDir, s.Name, "quicksave")
	case "manual", "manualsave", "manuals":
		// manual saves may be in either manualsave/ or server root
		d1 := filepath.Join(savesDir, s.Name, "manualsave", base)
		// Prefer manualsave folder if exists; else fallback to root
		if st, err := os.Stat(d1); err == nil && !st.IsDir() {
			return d1, nil
		}
		dir = filepath.Join(savesDir, s.Name)
		// compose at bottom
	case "player", "players", "playersave", "playersaves":
		dir = filepath.Join(savesDir, s.Name, "playersave")
	default:
		// Unknown type; reject
		return "", errors.New("unsupported type")
	}
	target := filepath.Join(dir, base)
	// Ensure target resides within dir
	cleanDir := filepath.Clean(dir)
	cleanTarget := filepath.Clean(target)
	if rel, err := filepath.Rel(cleanDir, cleanTarget); err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("invalid path")
	}
	if st, err := os.Stat(cleanTarget); err != nil || st.IsDir() {
		return "", errors.New("save file not found")
	}
	return cleanTarget, nil
}

// APIServerLoad loads a save file from a whitelisted location. JSON: { "type": "auto|quick|manual|player", "name": "file.save" }
func (h *ManagerHandlers) APIServerLoad(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var req struct{ Type, Name string }
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Load Failed")
		c.Header("X-Toast-Message", "Invalid request.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	full, err := h.resolveLoadPath(s, req.Type, req.Name)
	if err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Load Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Quote the file path to handle spaces/special characters. Prefer single quotes to avoid JSON escape noise.
	safe := strings.ReplaceAll(full, "'", "\\'")
	quoted := "'" + safe + "'"
	if err := s.SendCommand("console", "FILE load "+quoted); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Load Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Loading Save")
	c.Header("X-Toast-Message", "Requested loading of selected save.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}


// APIServerStorm toggles storm. JSON: { "start": true|false }
func (h *ManagerHandlers) APIServerStorm(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var req struct {
		Start bool `json:"start"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	cmd := "STORM stop"
	if req.Start {
		cmd = "STORM start"
	}
	if err := s.SendCommand("console", cmd); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Storm Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	if req.Start {
		c.Header("X-Toast-Title", "Storm Started")
		c.Header("X-Toast-Message", "Storm started.")
	} else {
		c.Header("X-Toast-Title", "Storm Ended")
		c.Header("X-Toast-Message", "Storm ended.")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerCleanup runs CLEANUPPLAYERS for a specific scope. JSON: { "scope": "dead|disconnected|all" }
func (h *ManagerHandlers) APIServerCleanup(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var req struct {
		Scope string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope != "dead" && scope != "disconnected" && scope != "all" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope"})
		return
	}
	if err := s.SendCommand("console", "CLEANUPPLAYERS "+scope); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Cleanup Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Cleanup Started")
	c.Header("X-Toast-Message", "Cleanup command sent.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerKick kicks a player by SteamID or name. JSON: { "steam_id": "..." } or { "name": "..." }
func (h *ManagerHandlers) APIServerKick(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	// Capture request allowing either steam_id or name
	type kickReq struct {
		SteamID string `json:"steam_id"`
		Name    string `json:"name"`
	}
	var r kickReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	steam := strings.TrimSpace(r.SteamID)
	name := strings.TrimSpace(r.Name)
	if steam == "" && name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id or name required"})
		return
	}
	// If name provided but no steam id, attempt resolve from live clients
	if steam == "" && name != "" {
		lower := strings.ToLower(name)
		for _, cl := range s.LiveClients() {
			if cl != nil && strings.EqualFold(strings.ToLower(cl.Name), lower) {
				steam = cl.SteamID
				break
			}
		}
		// Fallback to history
		if steam == "" {
			for _, cl := range s.Clients {
				if cl != nil && strings.EqualFold(strings.ToLower(cl.Name), lower) {
					steam = cl.SteamID
					break
				}
			}
		}
		if steam == "" {
			steam = name
		}
	}
	if err := s.SendCommand("console", "KICK "+steam); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Kick Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Realtime: player list and counts may change
	h.broadcastServerStatus(s)
	h.broadcastStats()
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Player Kicked")
	c.Header("X-Toast-Message", "Kick command sent.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerBan bans a player by SteamID or name.
// JSON: { "steam_id": "7656..." } or { "name": "PlayerName" }
func (h *ManagerHandlers) APIServerBan(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	type banReq struct {
		SteamID string `json:"steam_id"`
		Name    string `json:"name"`
	}
	var r banReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	steam := strings.TrimSpace(r.SteamID)
	name := strings.TrimSpace(r.Name)
	if steam == "" && name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id or name required"})
		return
	}
	// Resolve by name if needed
	if steam == "" && name != "" {
		candidate := strings.ToLower(name)
		for _, cl := range s.LiveClients() {
			if cl != nil && strings.EqualFold(cl.Name, candidate) {
				steam = cl.SteamID
				break
			}
		}
		if steam == "" {
			for _, cl := range s.Clients {
				if cl != nil && strings.EqualFold(cl.Name, candidate) {
					steam = cl.SteamID
					break
				}
			}
		}
		if steam == "" {
			steam = name
		} // fallback
	}
	// Try console BAN when running, else write to blacklist file
	if s.Running {
		if err := s.SendCommand("console", "BAN "+steam); err != nil {
			c.Header("X-Toast-Type", "error")
			c.Header("X-Toast-Title", "Ban Failed")
			c.Header("X-Toast-Message", err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Realtime: a live player may be removed as a result of BAN
		h.broadcastServerStatus(s)
		h.broadcastStats()
		c.Header("X-Toast-Type", "success")
		c.Header("X-Toast-Title", "Player Banned")
		c.Header("X-Toast-Message", "BAN command sent.")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if err := s.AddBlacklistID(steam); err != nil {
		c.Header("X-Toast-Type", "error")
		c.Header("X-Toast-Title", "Ban Failed")
		c.Header("X-Toast-Message", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Realtime: ban list changed; broadcast stats (player counts unchanged) and status for consistency
	h.broadcastServerStatus(s)
	h.broadcastStats()
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Player Banned")
	c.Header("X-Toast-Message", steam+" added to blacklist.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServerUnban removes a SteamID from blacklist.
// JSON: { "steam_id": "7656..." }
func (h *ManagerHandlers) APIServerUnban(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}
	var r struct {
		SteamID string `json:"steam_id"`
	}
	if err := c.ShouldBindJSON(&r); err != nil || strings.TrimSpace(r.SteamID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id required"})
		return
	}
	_ = s.RemoveBlacklistID(strings.TrimSpace(r.SteamID))
	// Realtime: unban list changed; broadcast status and stats
	h.broadcastServerStatus(s)
	h.broadcastStats()
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Player Unbanned")
	c.Header("X-Toast-Message", "Removed from blacklist.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIServersStopAll stops all running servers (admin only).
func (h *ManagerHandlers) APIServersStopAll(c *gin.Context) {
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	scheduled := 0
	for _, s := range h.manager.Servers {
		if s != nil && s.IsRunning() {
			s.StopAsync(func(srv *models.Server){
				h.broadcastServerStatus(srv)
				h.broadcastStats()
			})
			scheduled++
		}
	}
	c.Header("X-Toast-Type", "info")
	c.Header("X-Toast-Title", "Shutdown Scheduled")
	c.Header("X-Toast-Message", fmt.Sprintf("Shutting down %d servers.", scheduled))
	c.JSON(http.StatusOK, gin.H{"scheduled": scheduled})
}

// APIServerSaves lists server save files for a given type (e.g., auto, manual).
// Query params:
//
//	type: one of "auto" | "manual"
//
// Returns JSON: { items: [ { type: string, name: string, filename: string, datetime: RFC3339, size: int64, path: string } ] }
func (h *ManagerHandlers) APIServerSaves(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC: operators must be assigned to the server
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Determine base saves path
	var savesDir string
	if s.Paths != nil {
		savesDir = s.Paths.ServerSavesDir(s.ID)
	} else if h.manager.Paths != nil {
		savesDir = h.manager.Paths.ServerSavesDir(s.ID)
	}
	if strings.TrimSpace(savesDir) == "" {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}

	t := strings.ToLower(strings.TrimSpace(c.Query("type")))
	sub := ""
	switch t {
	case "auto", "autosave", "autosaves":
		sub = "autosave"
	case "quick", "quicksave", "quicksaves":
		sub = "quicksave"
	case "manual", "manualsave", "manuals":
		sub = "manualsave"
	case "player", "players", "playersave", "playersaves":
		sub = "player"
	case "":
		// default to auto for now
		sub = "autosave"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported type"})
		return
	}

	// Directory layout (strict):
	//  - autosave:     <savesDir>/<ServerName>/autosave
	//  - quicksave:    <savesDir>/<ServerName>/quicksave
	//  - manual (named): includes files from both <savesDir>/<ServerName>/manualsave and <savesDir>/<ServerName>
	//  - player:         includes files matching <steamid>-YYYYMMDD-HHMMSS.save from both manualsave and server root
	var dir string
	var dirs []string
	switch sub {
	case "autosave":
		dir = filepath.Join(savesDir, s.Name, "autosave")
	case "quicksave":
		dir = filepath.Join(savesDir, s.Name, "quicksave")
	case "manualsave":
		// We'll aggregate from two specific directories
		dirs = []string{
			filepath.Join(savesDir, s.Name, "manualsave"),
			filepath.Join(savesDir, s.Name),
		}
	case "player":
		// Player saves: <savesDir>/<ServerName>/playersave
		dir = filepath.Join(savesDir, s.Name, "playersave")
	default:
		dir = filepath.Join(savesDir, s.Name)
	}

	// (debug logging removed)

	type item struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Datetime string `json:"datetime"`
		Size     int64  `json:"size"`
		Path     string `json:"path"`
		ts       time.Time
	}

	items := make([]item, 0)
	// Helper to read a directory and append manual/named items
	addManualFromDir := func(baseDir string) {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") {
				continue
			}
			info, _ := e.Info()
			size := int64(0)
			var ts time.Time
			if info != nil {
				size = info.Size()
				ts = info.ModTime()
			}
			baseDisplay := strings.TrimSuffix(name, ".save")
			items = append(items, item{Type: "manual", Name: baseDisplay, Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(baseDir, name), ts: ts})
		}
	}

	// Grouped structure for player saves
	type playerGroup struct {
		SteamID string `json:"steam_id"`
		Name    string `json:"name"`
		Items   []item `json:"items"`
	}

	if sub == "manualsave" {
		for _, d := range dirs {
			addManualFromDir(d)
		}
	} else if sub == "player" {
		// Build grouped result from playersave dir
		groupsByID := make(map[string]*playerGroup)
		entries, err := os.ReadDir(dir)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"groups": []any{}})
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") {
				continue
			}
			base := strings.TrimSuffix(lower, ".save")
			// Expect ddmmyy_hhmmss_<steamid>
			parts := strings.Split(base, "_")
			if len(parts) < 3 {
				continue
			}
			datePart := parts[0]
			timePart := parts[1]
			steam := parts[2]
			if len(datePart) != 6 || len(timePart) != 6 {
				continue
			}
			if len(steam) != 17 {
				continue
			}
			// digits only check
			digits := true
			for i := 0; i < len(steam); i++ {
				if steam[i] < '0' || steam[i] > '9' {
					digits = false
					break
				}
			}
			if !digits {
				continue
			}
			dd := atoiSafe(datePart[0:2])
			mm := atoiSafe(datePart[2:4])
			yy := atoiSafe(datePart[4:6])
			hh := atoiSafe(timePart[0:2])
			min := atoiSafe(timePart[2:4])
			ss := atoiSafe(timePart[4:6])
			year := 2000 + yy
			if mm < 1 || mm > 12 || dd < 1 || dd > 31 {
				continue
			}
			ts := time.Date(year, time.Month(mm), dd, hh, min, ss, 0, time.Local)
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			// Ensure group exists
			g, ok := groupsByID[steam]
			if !ok {
				g = &playerGroup{SteamID: steam, Name: s.ResolveNameForSteamID(steam), Items: []item{}}
				groupsByID[steam] = g
			}
			g.Items = append(g.Items, item{Type: "player", Name: "", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts})
		}
		// Sort items within groups and groups by name asc (fallback steam id)
		groups := make([]playerGroup, 0, len(groupsByID))
		for _, g := range groupsByID {
			sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].ts.After(g.Items[j].ts) })
			groups = append(groups, *g)
		}
		sort.Slice(groups, func(i, j int) bool {
			ai := strings.ToLower(strings.TrimSpace(groups[i].Name))
			aj := strings.ToLower(strings.TrimSpace(groups[j].Name))
			if ai == aj {
				return groups[i].SteamID < groups[j].SteamID
			}
			if ai == "" {
				return false
			}
			if aj == "" {
				return true
			}
			return ai < aj
		})
		// Emit grouped JSON
		out := make([]gin.H, 0, len(groups))
		for _, g := range groups {
			// Emit items without internal ts
			gi := make([]gin.H, 0, len(g.Items))
			for _, it := range g.Items {
				gi = append(gi, gin.H{"type": it.Type, "filename": it.Filename, "datetime": it.Datetime, "size": it.Size, "path": it.Path})
			}
			out = append(out, gin.H{"steam_id": g.SteamID, "name": g.Name, "items": gi})
		}
		_ = utils.Paths{}
		c.JSON(http.StatusOK, gin.H{"groups": out})
		return
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"items": []any{}})
			return
		}
		items = make([]item, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") {
				continue
			}
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			switch sub {
			case "autosave":
				// Expect ddmmyy_hhmmss_auto.save
				base := strings.TrimSuffix(lower, ".save")
				parts := strings.Split(base, "_")
				if len(parts) < 3 {
					continue
				}
				datePart := parts[0]
				timePart := parts[1]
				if !strings.Contains(parts[2], "auto") {
					continue
				}
				if len(datePart) != 6 || len(timePart) != 6 {
					continue
				}
				dd := datePart[0:2]
				mm := datePart[2:4]
				yy := datePart[4:6]
				hh := timePart[0:2]
				min := timePart[2:4]
				ss := timePart[4:6]
				day := atoiSafe(dd)
				month := atoiSafe(mm)
				year := 2000 + atoiSafe(yy)
				hour := atoiSafe(hh)
				minute := atoiSafe(min)
				second := atoiSafe(ss)
				if month < 1 || month > 12 || day < 1 || day > 31 {
					continue
				}
				ts := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
				items = append(items, item{Type: "auto", Name: "Auto", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts})
			case "quicksave":
				// Expect ddmmyy_hhmmss_quick.save
				base := strings.TrimSuffix(lower, ".save")
				parts := strings.Split(base, "_")
				if len(parts) < 3 {
					continue
				}
				datePart := parts[0]
				timePart := parts[1]
				if !strings.Contains(parts[2], "quick") {
					continue
				}
				if len(datePart) != 6 || len(timePart) != 6 {
					continue
				}
				dd := datePart[0:2]
				mm := datePart[2:4]
				yy := datePart[4:6]
				hh := timePart[0:2]
				min := timePart[2:4]
				ss := timePart[4:6]
				day := atoiSafe(dd)
				month := atoiSafe(mm)
				year := 2000 + atoiSafe(yy)
				hour := atoiSafe(hh)
				minute := atoiSafe(min)
				second := atoiSafe(ss)
				if month < 1 || month > 12 || day < 1 || day > 31 {
					continue
				}
				ts := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
				items = append(items, item{Type: "quick", Name: "Quick", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].ts.After(items[j].ts) })

	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"type":     it.Type,
			"name":     it.Name,
			"filename": it.Filename,
			"datetime": it.Datetime,
			"size":     it.Size,
			"path":     it.Path,
		})
	}
	_ = utils.Paths{}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// APIServerSaveDelete deletes a save file under the server's saves directory.
// Query params:
//
//	type: one of "auto" | "manual"
//	name: filename (e.g., ddmmyy_hhmmss_auto.save or <filename>.save)
func (h *ManagerHandlers) APIServerSaveDelete(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC: operators must be assigned to the server
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Determine saves base
	var savesDir string
	if s.Paths != nil {
		savesDir = s.Paths.ServerSavesDir(s.ID)
	} else if h.manager.Paths != nil {
		savesDir = h.manager.Paths.ServerSavesDir(s.ID)
	}
	if strings.TrimSpace(savesDir) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "saves directory unavailable"})
		return
	}

	// Parse type and filename
	t := strings.ToLower(strings.TrimSpace(c.Query("type")))
	sub := ""
	switch t {
	case "auto", "autosave", "autosaves":
		sub = "autosave"
	case "quick", "quicksave", "quicksaves":
		sub = "quicksave"
	case "manual", "manualsave", "manuals":
		sub = "manualsave"
	case "player", "players", "playersave", "playersaves":
		sub = "player"
	case "":
		sub = "autosave"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported type"})
		return
	}

	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	base := filepath.Base(name)
	if !strings.HasSuffix(strings.ToLower(base), ".save") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	// Compute target directory for delete (strict per-type directory)
	// For manual/named, allow delete from either <server>/manualsave or <server>/ root
	tryDirs := []string{}
	switch sub {
	case "autosave":
		tryDirs = []string{filepath.Join(savesDir, s.Name, "autosave")}
	case "quicksave":
		tryDirs = []string{filepath.Join(savesDir, s.Name, "quicksave")}
	case "manualsave":
		tryDirs = []string{filepath.Join(savesDir, s.Name, "manualsave"), filepath.Join(savesDir, s.Name)}
	case "player":
		tryDirs = []string{filepath.Join(savesDir, s.Name, "playersave")}
	default:
		tryDirs = []string{filepath.Join(savesDir, s.Name)}
	}

	deleted := false
	for _, d := range tryDirs {
		target := filepath.Join(d, base)
		cleanDir := filepath.Clean(d)
		cleanTarget := filepath.Clean(target)
		if rel, err := filepath.Rel(cleanDir, cleanTarget); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if err := os.Remove(cleanTarget); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Try next directory
				continue
			}
			c.Header("X-Toast-Type", "error")
			c.Header("X-Toast-Title", "Delete Failed")
			c.Header("X-Toast-Message", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
			return
		}
		deleted = true
		break
	}

	if !deleted {
		c.Header("X-Toast-Type", "warning")
		c.Header("X-Toast-Title", "Not Found")
		c.Header("X-Toast-Message", "Save file was already removed.")
		c.JSON(http.StatusOK, gin.H{"status": "not_found"})
		return
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Deleted")
	c.Header("X-Toast-Message", "Save file deleted.")
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func atoiSafe(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
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

// APIServerPlayerSaveExclude adds a Steam ID to the server's player-save exclusion list.
// JSON body: { "steam_id": "7656119..." }
func (h *ManagerHandlers) APIServerPlayerSaveExclude(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC
	role := c.GetString("role")
	if role != "admin" {
		// Allow operators with access as well
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var req struct {
		SteamID string `json:"steam_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SteamID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id required"})
		return
	}

	added := s.AddPlayerSaveExclude(req.SteamID)
	if added {
		h.manager.Save()
	}

	// Additionally delete all current player saves for this SteamID
	// Locations to scan: playersave/ (primary) and manualsave/ (in-flight before move)
	deleted := 0
	steam := strings.TrimSpace(req.SteamID)
	// Resolve base saves directory
	var base string
	if s.Paths != nil {
		base = s.Paths.ServerSavesDir(s.ID)
	} else if h.manager.Paths != nil {
		base = h.manager.Paths.ServerSavesDir(s.ID)
	}
	if strings.TrimSpace(base) != "" {
		tryDeleteIn := func(dir string) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				lower := strings.ToLower(name)
				if !strings.HasSuffix(lower, ".save") {
					continue
				}
				// match *_<steam>.save
				if strings.HasSuffix(strings.TrimSuffix(lower, ".save"), "_"+strings.ToLower(steam)) {
					path := filepath.Join(dir, name)
					_ = os.Remove(path)
					deleted++
				}
				// legacy double-extension case "*.save.save"
				if strings.HasSuffix(lower, ".save.save") {
					baseNo := strings.TrimSuffix(lower, ".save") // drop one .save
					if strings.HasSuffix(strings.TrimSuffix(baseNo, ".save"), "_"+strings.ToLower(steam)) {
						path := filepath.Join(dir, name)
						_ = os.Remove(path)
						deleted++
					}
				}
			}
		}
		tryDeleteIn(filepath.Join(base, s.Name, "playersave"))
		tryDeleteIn(filepath.Join(base, s.Name, "manualsave"))
	}

	// Toast and response
	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Deleted Player Saves")
	if deleted > 0 {
		c.Header("X-Toast-Message", fmt.Sprintf("Excluded and removed %d save(s).", deleted))
	} else {
		c.Header("X-Toast-Message", "Excluded player from future saves.")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "added": added, "deleted": deleted})
}

// APIServerPlayerSaveDeleteAll deletes all player saves for a given SteamID without excluding the player.
// JSON body: { "steam_id": "7656119..." }
func (h *ManagerHandlers) APIServerPlayerSaveDeleteAll(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	// Enforce RBAC
	role := c.GetString("role")
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			if user, ok2 := val.(string); ok2 {
				if h.userStore == nil || !h.userStore.CanAccess(user, serverID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
			}
		}
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found"})
		return
	}

	var req struct {
		SteamID string `json:"steam_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SteamID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id required"})
		return
	}

	steam := strings.TrimSpace(req.SteamID)
	var base string
	if s.Paths != nil {
		base = s.Paths.ServerSavesDir(s.ID)
	} else if h.manager.Paths != nil {
		base = h.manager.Paths.ServerSavesDir(s.ID)
	}
	deleted := 0
	if strings.TrimSpace(base) != "" {
		tryDeleteIn := func(dir string) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				lower := strings.ToLower(name)
				if !strings.HasSuffix(lower, ".save") {
					continue
				}
				if strings.HasSuffix(strings.TrimSuffix(lower, ".save"), "_"+strings.ToLower(steam)) {
					_ = os.Remove(filepath.Join(dir, name))
					deleted++
				}
				if strings.HasSuffix(lower, ".save.save") {
					baseNo := strings.TrimSuffix(lower, ".save")
					if strings.HasSuffix(strings.TrimSuffix(baseNo, ".save"), "_"+strings.ToLower(steam)) {
						_ = os.Remove(filepath.Join(dir, name))
						deleted++
					}
				}
			}
		}
		tryDeleteIn(filepath.Join(base, s.Name, "playersave"))
		tryDeleteIn(filepath.Join(base, s.Name, "manualsave"))
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Deleted Player Files")
	if deleted > 0 {
		c.Header("X-Toast-Message", fmt.Sprintf("Removed %d save(s).", deleted))
	} else {
		c.Header("X-Toast-Message", "No files found for this player.")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
}

func (h *ManagerHandlers) APIServers(c *gin.Context) {
	role := c.GetString("role")
	servers := h.manager.Servers
	if role != "admin" {
		if val, ok := c.Get("username"); ok {
			user, _ := val.(string)
			filtered := make([]*models.Server, 0, len(servers))
			for _, s := range servers {
				if h.userStore != nil && h.userStore.CanAccess(user, s.ID) {
					filtered = append(filtered, s)
				}
			}
			servers = filtered
		} else {
			servers = []*models.Server{}
		}
	}

	if strings.EqualFold(c.GetHeader("HX-Request"), "true") || strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.HTML(http.StatusOK, "server_cards.html", gin.H{
			"servers": servers,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"servers": servers,
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
