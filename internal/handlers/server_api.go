package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sdsm/internal/models"
	"sdsm/internal/utils"
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
		"port":           s.Port,
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
		if e.IsDir() { continue }
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".log") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// APIServerLogTail streams a chunk of a log starting from a byte offset. It supports:
//  - offset >= 0: read from offset up to 'max' bytes
//  - offset == -1: read the last 'back' bytes from end (or 0 if back not provided)
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
		"data":  string(buf),
		"offset": next,
		"size":   size,
		"reset":  reset,
	})
}

// APIServerCommand accepts a command to be sent to the running server process via stdin.
// JSON body: { "type": "console"|"chat", "payload": "..." }
func (h *ManagerHandlers) APIServerCommand(c *gin.Context) {
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

// APIServerSaves lists server save files for a given type (e.g., auto, manual).
// Query params:
//   type: one of "auto" | "manual"
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
		if err != nil { return }
		for _, e := range entries {
			if e.IsDir() { continue }
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") { continue }
			info, _ := e.Info()
			size := int64(0)
			var ts time.Time
			if info != nil {
				size = info.Size()
				ts = info.ModTime()
			}
			baseDisplay := strings.TrimSuffix(name, ".save")
			items = append(items, item{ Type: "manual", Name: baseDisplay, Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(baseDir, name), ts: ts })
		}
	}

	// Grouped structure for player saves
	type playerGroup struct {
		SteamID string `json:"steam_id"`
		Name    string `json:"name"`
		Items   []item `json:"items"`
	}

	if sub == "manualsave" {
		for _, d := range dirs { addManualFromDir(d) }
	} else if sub == "player" {
		// Build grouped result from playersave dir
		groupsByID := make(map[string]*playerGroup)
		entries, err := os.ReadDir(dir)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"groups": []any{}})
			return
		}
		for _, e := range entries {
			if e.IsDir() { continue }
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") { continue }
			base := strings.TrimSuffix(lower, ".save")
			// Expect ddmmyy_hhmmss_<steamid>
			parts := strings.Split(base, "_")
			if len(parts) < 3 { continue }
			datePart := parts[0]
			timePart := parts[1]
			steam := parts[2]
			if len(datePart) != 6 || len(timePart) != 6 { continue }
			if len(steam) != 17 { continue }
			// digits only check
			digits := true
			for i := 0; i < len(steam); i++ { if steam[i] < '0' || steam[i] > '9' { digits = false; break } }
			if !digits { continue }
			dd := atoiSafe(datePart[0:2])
			mm := atoiSafe(datePart[2:4])
			yy := atoiSafe(datePart[4:6])
			hh := atoiSafe(timePart[0:2])
			min := atoiSafe(timePart[2:4])
			ss := atoiSafe(timePart[4:6])
			year := 2000 + yy
			if mm < 1 || mm > 12 || dd < 1 || dd > 31 { continue }
			ts := time.Date(year, time.Month(mm), dd, hh, min, ss, 0, time.Local)
			info, _ := e.Info()
			size := int64(0)
			if info != nil { size = info.Size() }
			// Ensure group exists
			g, ok := groupsByID[steam]
			if !ok {
				g = &playerGroup{SteamID: steam, Name: s.ResolveNameForSteamID(steam), Items: []item{}}
				groupsByID[steam] = g
			}
			g.Items = append(g.Items, item{ Type: "player", Name: "", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts })
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
			if ai == "" { return false }
			if aj == "" { return true }
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
			if e.IsDir() { continue }
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".save") { continue }
			info, _ := e.Info()
			size := int64(0)
			if info != nil { size = info.Size() }
			switch sub {
		case "autosave":
			// Expect ddmmyy_hhmmss_auto.save
			base := strings.TrimSuffix(lower, ".save")
			parts := strings.Split(base, "_")
			if len(parts) < 3 { continue }
			datePart := parts[0]
			timePart := parts[1]
			if !strings.Contains(parts[2], "auto") { continue }
			if len(datePart) != 6 || len(timePart) != 6 { continue }
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
			if month < 1 || month > 12 || day < 1 || day > 31 { continue }
			ts := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
			items = append(items, item{ Type: "auto", Name: "Auto", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts })
		case "quicksave":
			// Expect ddmmyy_hhmmss_quick.save
			base := strings.TrimSuffix(lower, ".save")
			parts := strings.Split(base, "_")
			if len(parts) < 3 { continue }
			datePart := parts[0]
			timePart := parts[1]
			if !strings.Contains(parts[2], "quick") { continue }
			if len(datePart) != 6 || len(timePart) != 6 { continue }
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
			if month < 1 || month > 12 || day < 1 || day > 31 { continue }
			ts := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
			items = append(items, item{ Type: "quick", Name: "Quick", Filename: name, Datetime: ts.UTC().Format(time.RFC3339), Size: size, Path: filepath.Join(dir, name), ts: ts })
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
//   type: one of "auto" | "manual"
//   name: filename (e.g., ddmmyy_hhmmss_auto.save or <filename>.save)
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

	var req struct{ SteamID string `json:"steam_id"` }
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SteamID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steam_id required"})
		return
	}

	added := s.AddPlayerSaveExclude(req.SteamID)
	if added {
		h.manager.Save()
	}

	c.Header("X-Toast-Type", "success")
	c.Header("X-Toast-Title", "Player Excluded")
	c.Header("X-Toast-Message", "This player will be excluded from future player saves.")
	c.JSON(http.StatusOK, gin.H{"status": "ok", "added": added})
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
