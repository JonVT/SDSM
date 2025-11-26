// Package handlers exposes HTTP handlers for the SDSM web UI and API.
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"
	"sdsm/internal/models"
	"sdsm/internal/utils"

	"github.com/gin-gonic/gin"
)

// note: setup overlay moved to /setup; no fallback string needed here

// ManagerHandlers wires HTTP handlers to a Manager instance.
type ManagerHandlers struct {
	manager   *manager.Manager
	userStore *manager.UserStore
	hub       *middleware.Hub

	progressMu        sync.Mutex
	progressLoopAlive bool
}

// NewManagerHandlers constructs a handler set bound to the provided Manager and UserStore.
func NewManagerHandlers(mgr *manager.Manager, us *manager.UserStore) *ManagerHandlers {
	return &ManagerHandlers{manager: mgr, userStore: us}
}

// NewManagerHandlersWithHub constructs handlers with an attached websocket hub for realtime updates.
func NewManagerHandlersWithHub(mgr *manager.Manager, us *manager.UserStore, hub *middleware.Hub) *ManagerHandlers {
	return &ManagerHandlers{manager: mgr, userStore: us, hub: hub}
}

// APIManagerLogsList returns a JSON array of available *.log files in the manager's logs directory.
func (h *ManagerHandlers) APIManagerLogsList(c *gin.Context) {
	// Admin only
	if c.GetString("role") != "admin" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	logsDir := ""
	if h.manager != nil && h.manager.Paths != nil {
		logsDir = h.manager.Paths.LogsDir()
	}
	if strings.TrimSpace(logsDir) == "" {
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

// APIManagerLogTail mirrors APIServerLogTail but reads from the manager logs directory.
// Query: name (required), offset (-1 for tail), back, max
func (h *ManagerHandlers) APIManagerLogTail(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	name := filepath.Base(strings.TrimSpace(c.Query("name")))
	if name == "" || !strings.HasSuffix(strings.ToLower(name), ".log") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log file"})
		return
	}
	logsDir := ""
	if h.manager != nil && h.manager.Paths != nil {
		logsDir = h.manager.Paths.LogsDir()
	}
	if strings.TrimSpace(logsDir) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "logs directory not available"})
		return
	}
	logPath := filepath.Join(logsDir, name)
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
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
	// Parse params
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
	var offset int64 = -1
	if offStr := strings.TrimSpace(c.Query("offset")); offStr != "" {
		if v, err := strconv.ParseInt(offStr, 10, 64); err == nil {
			offset = v
		}
	}
	var start, length int64
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
		_, _ = file.ReadAt(buf, start)
	}
	next := start + length
	c.JSON(http.StatusOK, gin.H{"data": string(buf), "offset": next, "size": size, "reset": reset})
}

// APIManagerLogClear truncates a manager log file (admin only).
// JSON/Form params: name=<log filename>
func (h *ManagerHandlers) APIManagerLogClear(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	name := c.PostForm("name")
	if name == "" {
		// allow JSON
		var body struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err == nil && body.Name != "" {
			name = body.Name
		}
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || !strings.HasSuffix(strings.ToLower(name), ".log") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log file"})
		return
	}
	logsDir := ""
	if h.manager != nil && h.manager.Paths != nil {
		logsDir = h.manager.Paths.LogsDir()
	}
	if strings.TrimSpace(logsDir) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "logs directory not available"})
		return
	}
	path := filepath.Join(logsDir, name)
	// Truncate (create if missing)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to clear log"})
		return
	}
	_ = f.Close()
	ToastWarn(c, "Log Cleared", name+" truncated")
	c.JSON(http.StatusOK, gin.H{"status": "cleared"})
}

// APIManagerLogDownload streams a manager log file as an attachment (admin only).
func (h *ManagerHandlers) APIManagerLogDownload(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	name := filepath.Base(strings.TrimSpace(c.Query("name")))
	if name == "" || !strings.HasSuffix(strings.ToLower(name), ".log") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log file"})
		return
	}
	logsDir := ""
	if h.manager != nil && h.manager.Paths != nil {
		logsDir = h.manager.Paths.LogsDir()
	}
	if strings.TrimSpace(logsDir) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "logs directory not available"})
		return
	}
	path := filepath.Join(logsDir, name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to access log"})
		return
	}
	c.FileAttachment(path, name)
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

// buildWorldSelectionDataForLanguage mirrors buildWorldSelectionData but localizes
// world names and option lists using the provided language.
func (h *ManagerHandlers) buildWorldSelectionDataForLanguage(language string) (
	map[string][]string, // worldIDs by version
	map[string][]gin.H, // world displays [{id,name}] by version
	map[string]map[string]gin.H, // worldData by version keyed by worldID
) {
	// Always use stable world IDs for linkage
	// Use stable world IDs (language-independent) to avoid mismatches after localization
	worldIDs := map[string][]string{
		"release": h.manager.GetWorldIDsByVersion(false),
		"beta":    h.manager.GetWorldIDsByVersion(true),
	}

	// Build display metadata per world ID using the requested language
	worldDisplays := map[string][]gin.H{
		"release": {},
		"beta":    {},
	}
	for _, id := range worldIDs["release"] {
		info := h.manager.GetWorldInfoWithLanguage(id, false, language)
		name := id
		if info.Name != "" {
			name = info.Name
		}
		worldDisplays["release"] = append(worldDisplays["release"], gin.H{"id": id, "name": name})
	}
	for _, id := range worldIDs["beta"] {
		info := h.manager.GetWorldInfoWithLanguage(id, true, language)
		name := id
		if info.Name != "" {
			name = info.Name
		}
		worldDisplays["beta"] = append(worldDisplays["beta"], gin.H{"id": id, "name": name})
	}

	// Build per-world option data (locations/conditions) keyed by ID
	worldData := map[string]map[string]gin.H{
		"release": {},
		"beta":    {},
	}
	for _, id := range worldIDs["release"] {
		worldData["release"][id] = gin.H{
			"locations":  h.manager.GetStartLocationsForWorldVersionWithLanguage(id, false, language),
			"conditions": h.manager.GetStartConditionsForWorldVersionWithLanguage(id, false, language),
		}
	}
	for _, id := range worldIDs["beta"] {
		worldData["beta"][id] = gin.H{
			"locations":  h.manager.GetStartLocationsForWorldVersionWithLanguage(id, true, language),
			"conditions": h.manager.GetStartConditionsForWorldVersionWithLanguage(id, true, language),
		}
	}

	return worldIDs, worldDisplays, worldData
}

func (h *ManagerHandlers) serverStatusPayload(c *gin.Context, s *models.Server, username interface{}, errMsg string) gin.H {
	// Refresh runtime flags so the template does not show stale state after restarts.
	s.IsRunning()

	// Sort live and historical clients so most recent connections appear first in UI.
	// Live: newest ConnectDatetime first. History: likewise.
	liveSorted := append([]*models.Client(nil), s.LiveClients()...)
	if len(liveSorted) > 1 {
		sort.SliceStable(liveSorted, func(i, j int) bool {
			return liveSorted[i].ConnectDatetime.After(liveSorted[j].ConnectDatetime)
		})
	}
	historySorted := append([]*models.Client(nil), s.Clients...)
	if len(historySorted) > 1 {
		sort.SliceStable(historySorted, func(i, j int) bool {
			return historySorted[i].ConnectDatetime.After(historySorted[j].ConnectDatetime)
		})
	}

	// Localize to the server's configured language when building world/difficulty lists
	worldIDs, worldDisplays, worldData := h.buildWorldSelectionDataForLanguage(s.Language)
	worldInfo := h.manager.GetWorldInfoWithLanguage(s.World, s.Beta, s.Language)
	// Resolve the server's configured world reference to a canonical technical ID for this channel
	resolvedWorldID := h.manager.ResolveWorldID(s.WorldID, s.Beta)

	// Compute detected SCON URL from runtime logs (fallback handled in method)
	sconPort := s.CurrentSCONPort()
	sconURL := fmt.Sprintf("http://localhost:%d/", sconPort)

	role := c.GetString("role")

	// Compute previous/next server IDs for navigation with wraparound.
	prevID := 0
	nextID := 0
	if h.manager != nil {
		uname, _ := username.(string)
		accessible := make([]int, 0, len(h.manager.Servers))
		var all bool
		var assignedList []int
		if role == string(manager.RoleOperator) && h.userStore != nil {
			all, assignedList, _ = h.userStore.GetAssignments(uname)
		}
		for _, srv := range h.manager.Servers {
			if srv == nil {
				continue
			}
			if role == string(manager.RoleAdmin) {
				accessible = append(accessible, srv.ID)
			} else if role == string(manager.RoleOperator) {
				if all {
					accessible = append(accessible, srv.ID)
				} else {
					for _, id := range assignedList {
						if id == srv.ID {
							accessible = append(accessible, srv.ID)
							break
						}
					}
				}
			}
		}
		if len(accessible) > 1 {
			sort.Ints(accessible)
			idx := -1
			for i, id := range accessible {
				if id == s.ID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				// Modular wraparound
				prevID = accessible[(idx-1+len(accessible))%len(accessible)]
				nextID = accessible[(idx+1)%len(accessible)]
			}
		}
		// Lookup names for tooltips if IDs resolved
		if prevID > 0 {
			if ps := h.manager.ServerByID(prevID); ps != nil {
				c.Set("prev_server_name", ps.Name)
			}
		}
		if nextID > 0 {
			if ns := h.manager.ServerByID(nextID); ns != nil {
				c.Set("next_server_name", ns.Name)
			}
		}
	}
	payload := gin.H{
		"server":           s,
		"liveClients":      liveSorted,
		"historyClients":   historySorted,
		"manager":          h.manager,
		"username":         username,
		"role":             role,
		"prev_server_id":   prevID,
		"next_server_id":   nextID,
		"prev_server_name": c.GetString("prev_server_name"),
		"next_server_name": c.GetString("next_server_name"),
		"worldInfo":        worldInfo,
		// Canonical world ID used by the client as initial selection
		"resolved_world_id": resolvedWorldID,
		// Stable world IDs and localized display names
		"worldIds":  worldIDs,
		"worlds":    worldDisplays,
		"worldData": worldData,
		// Per-channel difficulties for live switching on the status page (localized)
		"release_difficulties": h.manager.GetDifficultiesForVersionWithLanguage(false, s.Language),
		"beta_difficulties":    h.manager.GetDifficultiesForVersionWithLanguage(true, s.Language),
		// Per-channel languages for per-server selection
		"release_languages": h.manager.GetLanguagesForVersion(false),
		"beta_languages":    h.manager.GetLanguagesForVersion(true),
		"serverPath":        h.manager.Paths.ServerDir(s.ID),
		"banned":            s.BannedEntries(),
		"scon_port":         sconPort,
		"scon_url":          sconURL,
	}

	if errMsg != "" {
		payload["error"] = errMsg
	}

	payload["currentServerPath"] = fmt.Sprintf("server/%d", s.ID)

	return payload
}

func (h *ManagerHandlers) renderServerPage(c *gin.Context, status int, s *models.Server, username interface{}, errMsg string) {
	payload := h.serverStatusPayload(c, s, username, errMsg)
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

	// Determine default difficulty from release channel using helper (prefers "Normal")
	defaultDifficulty := DefaultDifficulty(h.manager, false)

	formDefaults := gin.H{
		"name":                     suggestedName,
		"world":                    defaultWorld,
		"start_location":           defaultStartLocation,
		"start_condition":          defaultStartCondition,
		"difficulty":               defaultDifficulty,
		"port":                     fmt.Sprintf("%d", h.manager.GetNextAvailablePort(defaultServerPort)),
		"max_clients":              "10",
		"password":                 "",
		"auth_secret":              "",
		"save_interval":            "300",
		"restart_delay_seconds":    fmt.Sprintf("%d", models.DefaultRestartDelaySeconds),
		"beta":                     defaultBetaValue,
		"auto_start":               false,
		"auto_update":              false,
		"auto_save":                true,
		"player_saves":             true,
		"auto_pause":               true,
		"delete_skeleton_on_decay": false,
		// Steam P2P removed; always disabled
		"use_steam_p2p":  false,
		"server_visible": true,
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

func (h *ManagerHandlers) renderError(c *gin.Context, code int, message string) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	// If the request is from HTMX, render the error partial
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(code, "error.html", gin.H{"error": message, "title": "Error"})
		return
	}

	// Otherwise, render the full frame with the error
	c.HTML(code, "frame.html", gin.H{
		"error":     message,
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "error",
		"title":     "Error",
	})
}

func (h *ManagerHandlers) startDeployAsync(deployType manager.DeployType) error {
	if err := h.manager.StartDeployAsync(deployType); err != nil {
		return err
	}
	h.ensureProgressBroadcastLoop()
	return nil
}

func (h *ManagerHandlers) ensureProgressBroadcastLoop() {
	if h.hub == nil {
		return
	}
	h.progressMu.Lock()
	if h.progressLoopAlive {
		h.progressMu.Unlock()
		return
	}
	h.progressLoopAlive = true
	h.progressMu.Unlock()

	go func() {
		defer func() {
			h.progressMu.Lock()
			h.progressLoopAlive = false
			h.progressMu.Unlock()
		}()

		var lastPayload string
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			snapshot := h.manager.ProgressSnapshot()
			payload := map[string]any{
				"type":     "manager_progress",
				"snapshot": snapshot,
			}
			if msg, err := json.Marshal(payload); err == nil {
				if payloadStr := string(msg); payloadStr != lastPayload {
					lastPayload = payloadStr
					h.hub.Broadcast(msg)
				}
			}

			if !snapshot.Updating {
				return
			}

			<-ticker.C
		}
	}()
}

func (h *ManagerHandlers) startServerUpdateAsync(s *models.Server) {
	// Discord notification: update started
	h.manager.NotifyServerEvent(s, "update-started", "Server file update started.")
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
			// Discord notification: update failed
			h.manager.NotifyServerEvent(s, "update-failed", fmt.Sprintf("Server file update failed: %v", err))
			return
		}

		// Persist a deploy snapshot capturing manager's deployed component versions
		if err := h.writeServerDeploySnapshot(s); err != nil {
			if s.Logger != nil {
				s.Logger.Write("Warning: failed to write deploy snapshot: " + err.Error())
			}
		}

		h.manager.ServerProgressComplete(s.ID, "Completed", nil)
		// Discord notification: update completed
		h.manager.NotifyServerEvent(s, "update-completed", "Server file update completed successfully.")
	}()
}

// writeServerDeploySnapshot stores a per-server snapshot of component versions at the time
// server files were last copied. This enables computing an "update needed" indicator later.
func (h *ManagerHandlers) writeServerDeploySnapshot(s *models.Server) error {
	if h == nil || h.manager == nil || s == nil {
		return fmt.Errorf("invalid context")
	}
	// Resolve path helpers from server when available, otherwise from manager
	var paths *utils.Paths
	if s.Paths != nil {
		paths = s.Paths
	} else {
		paths = h.manager.Paths
	}
	if paths == nil {
		return fmt.Errorf("paths unavailable")
	}
	// Ensure settings directory exists
	if err := os.MkdirAll(paths.ServerSettingsDir(s.ID), 0o755); err != nil {
		return err
	}
	// Build snapshot payload
	snap := struct {
		Timestamp         string `json:"timestamp"`
		Beta              bool   `json:"beta"`
		ReleaseDeployed   string `json:"release_deployed"`
		BetaDeployed      string `json:"beta_deployed"`
		BepInExDeployed   string `json:"bepinex_deployed"`
		LaunchPadDeployed string `json:"launchpad_deployed"`
		SCONDeployed      string `json:"scon_deployed"`
	}{
		Timestamp:         time.Now().Format(time.RFC3339),
		Beta:              s.Beta,
		ReleaseDeployed:   strings.TrimSpace(h.manager.ReleaseDeployed()),
		BetaDeployed:      strings.TrimSpace(h.manager.BetaDeployed()),
		BepInExDeployed:   strings.TrimSpace(h.manager.BepInExDeployed()),
		LaunchPadDeployed: strings.TrimSpace(h.manager.LaunchPadDeployed()),
		SCONDeployed:      strings.TrimSpace(h.manager.SCONDeployed()),
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	// Write atomically: temp file then rename
	dst := paths.ServerDeploySnapshotFile(s.ID)
	tmp, err := os.CreateTemp(paths.ServerSettingsDir(s.ID), "deploy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// Best-effort permissions
	_ = os.Chmod(dst, 0o644)
	return nil
}

// Home redirects to the login page (root entry point).
func (h *ManagerHandlers) Home(c *gin.Context) {
	c.Redirect(http.StatusFound, "/login")
}

// Frame renders the outer frame shell with server list and active status.
func (h *ManagerHandlers) Frame(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	servers := h.manager.Servers
	if role != "admin" {
		user, _ := username.(string)
		filtered := make([]*models.Server, 0, len(servers))
		for _, s := range servers {
			if h.userStore != nil && h.userStore.CanAccess(user, s.ID) {
				filtered = append(filtered, s)
			}
		}
		servers = filtered
	}

	data := gin.H{
		"active":    h.manager.IsActive(),
		"servers":   servers,
		"username":  username,
		"role":      role,
		"buildTime": h.manager.BuildTime(),
		"page":      "dashboard", // Default to dashboard
		"title":     "Dashboard",
	}
	c.HTML(http.StatusOK, "frame.html", data)
}

// Dashboard renders the main dashboard page, showing a list of servers.
func (h *ManagerHandlers) Dashboard(c *gin.Context) {
	user, _ := c.Get("user")
	role := c.GetString("role")
	username, _ := c.Get("username")

	servers := h.manager.Servers
	if role != "admin" {
		uname, _ := username.(string)
		filtered := make([]*models.Server, 0, len(servers))
		for _, s := range servers {
			if h.userStore != nil && h.userStore.CanAccess(uname, s.ID) {
				filtered = append(filtered, s)
			}
		}
		servers = filtered
	}

	rootPath := ""
	if h.manager != nil && h.manager.Paths != nil {
		rootPath = h.manager.Paths.RootPath
	}

	data := gin.H{
		"servers":   servers,
		"user":      user,
		"username":  username,
		"role":      role,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"rootPath":  rootPath,
		"page":      "dashboard",
		"title":     "Dashboard",
	}

	// If this is an HTMX request, render only the content
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "dashboard.html", data)
		return
	}

	// Otherwise, render the full frame
	c.HTML(http.StatusOK, "frame.html", data)
}

// ManagerGET renders the manager settings/status page with version info,
// missing component warnings, deploy state, and game-data scanner warnings.
func (h *ManagerHandlers) ManagerGET(c *gin.Context) {
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	warnings := h.manager.MissingComponents
	autoUpdateTime := ""
	if !h.manager.UpdateTime.IsZero() {
		autoUpdateTime = h.manager.UpdateTime.Format("15:04")
	}

	servers := h.manager.Servers
	activeCount := 0
	totalPlayers := 0
	for _, s := range servers {
		if s.IsRunning() {
			activeCount++
		}
		totalPlayers += s.ClientCount()
	}
	systemHealth := fmt.Sprintf("%.0f%%", h.manager.SystemHealthPercent())
	systemTelemetry := h.manager.SystemTelemetry()

	data := gin.H{
		"username":                            username,
		"role":                                role,
		"root_path":                           h.manager.Paths.RootPath,
		"steam_id":                            h.manager.SteamID,
		"port":                                h.manager.Port,
		"auto_update_time":                    autoUpdateTime,
		"auto_update":                         h.manager.StartupUpdate,
		"start_update":                        h.manager.StartupUpdate,
		"detached":                            h.manager.DetachedServers,
		"tray_enabled":                        h.manager.TrayEnabled,
		"updating":                            h.manager.IsUpdating(),
		"buildTime":                           h.manager.BuildTime(),
		"server_count_active":                 h.manager.ActiveServerCount(),
		"game_data_warnings":                  warnings,
		"auto_port_forward_manager":           h.manager.AutoPortForwardManager,
		"tls_enabled":                         h.manager.TLSEnabled,
		"tls_cert":                            h.manager.TLSCertPath,
		"tls_key":                             h.manager.TLSKeyPath,
		"discord_manager_webhook":             h.manager.DiscordManagerWebhook,
		"discord_default_webhook":             h.manager.DiscordDefaultWebhook,
		"notify_enable_deploy":                h.manager.NotifyEnableDeploy,
		"notify_enable_server":                h.manager.NotifyEnableServer,
		"notify_deploy_on_started":            h.manager.NotifyDeployOnStarted,
		"notify_deploy_on_completed":          h.manager.NotifyDeployOnCompleted,
		"notify_deploy_on_completed_error":    h.manager.NotifyDeployOnCompletedError,
		"notify_on_start":                     h.manager.NotifyOnStart,
		"notify_on_stopping":                  h.manager.NotifyOnStopping,
		"notify_on_stopped":                   h.manager.NotifyOnStopped,
		"notify_on_restart":                   h.manager.NotifyOnRestart,
		"notify_on_update_started":            h.manager.NotifyOnUpdateStarted,
		"notify_on_update_completed":          h.manager.NotifyOnUpdateCompleted,
		"notify_on_update_failed":             h.manager.NotifyOnUpdateFailed,
		"notify_deploy_release":               h.manager.NotifyDeployRelease,
		"notify_deploy_beta":                  h.manager.NotifyDeployBeta,
		"notify_deploy_bepinex":               h.manager.NotifyDeployBepInEx,
		"notify_deploy_launchpad":             h.manager.NotifyDeployLaunchPad,
		"notify_deploy_scon":                  h.manager.NotifyDeploySCON,
		"notify_deploy_steamcmd":              h.manager.NotifyDeploySteamCMD,
		"notify_deploy_servers":               h.manager.NotifyDeployServers,
		"notify_msg_start":                    h.manager.NotifyMsgStart,
		"notify_msg_stopping":                 h.manager.NotifyMsgStopping,
		"notify_msg_stopped":                  h.manager.NotifyMsgStopped,
		"notify_msg_restart":                  h.manager.NotifyMsgRestart,
		"notify_msg_update_started":           h.manager.NotifyMsgUpdateStarted,
		"notify_msg_update_completed":         h.manager.NotifyMsgUpdateCompleted,
		"notify_msg_update_failed":            h.manager.NotifyMsgUpdateFailed,
		"notify_msg_deploy_started":           h.manager.NotifyMsgDeployStarted,
		"notify_msg_deploy_completed":         h.manager.NotifyMsgDeployCompleted,
		"notify_msg_deploy_completed_error":   h.manager.NotifyMsgDeployCompletedError,
		"notify_color_start":                  h.manager.NotifyColorStart,
		"notify_color_stopping":               h.manager.NotifyColorStopping,
		"notify_color_stopped":                h.manager.NotifyColorStopped,
		"notify_color_restart":                h.manager.NotifyColorRestart,
		"notify_color_update_started":         h.manager.NotifyColorUpdateStarted,
		"notify_color_update_completed":       h.manager.NotifyColorUpdateCompleted,
		"notify_color_update_failed":          h.manager.NotifyColorUpdateFailed,
		"notify_color_deploy_started":         h.manager.NotifyColorDeployStarted,
		"notify_color_deploy_completed":       h.manager.NotifyColorDeployCompleted,
		"notify_color_deploy_completed_error": h.manager.NotifyColorDeployCompletedError,
		"totalServers":                        len(servers),
		"activeServers":                       activeCount,
		"totalPlayers":                        totalPlayers,
		"systemHealth":                        systemHealth,
		"systemTelemetry":                     systemTelemetry,
		"servers":                             h.manager.Servers,
		"active":                              h.manager.IsActive(),
		"page":                                "manager",
		"title":                               "Manager Settings",
	}

	// If this is an HTMX request, render only the manager content
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "manager.html", data)
		return
	}

	// Otherwise, render the full frame with manager selected
	c.HTML(http.StatusOK, "frame.html", data)
}

// ManagerVersionsGET returns latest/deployed component version info as JSON for async loading.
func (h *ManagerHandlers) ManagerVersionsGET(c *gin.Context) {
	// Ensure HTMX swaps this fragment into the version grid on the
	// manager page, even if no explicit hx-target was provided and
	// the body has a global hx-target (e.g. #content-area).
	if c.GetHeader("HX-Target") == "" {
		c.Header("HX-Target", "version-status-grid")
	}

	c.HTML(http.StatusOK, "version_status_grid.html", gin.H{
		"release_latest":     h.manager.ReleaseLatest(),
		"beta_latest":        h.manager.BetaLatest(),
		"steamcmd_latest":    h.manager.SteamCmdLatest(),
		"bepinex_latest":     h.manager.BepInExLatest(),
		"launchpad_latest":   h.manager.LaunchPadLatest(),
		"scon_latest":        h.manager.SCONLatest(),
		"release_deployed":   h.manager.ReleaseDeployed(),
		"beta_deployed":      h.manager.BetaDeployed(),
		"steamcmd_deployed":  h.manager.SteamCmdDeployed(),
		"bepinex_deployed":   h.manager.BepInExDeployed(),
		"launchpad_deployed": h.manager.LaunchPadDeployed(),
		"scon_deployed":      h.manager.SCONDeployed(),
	})
}

// TokensHelpGET renders a simple reference page for chat token usage.
func (h *ManagerHandlers) TokensHelpGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "tokens.html", gin.H{
			"username": username,
			"role":     role,
		})
		return
	}

	// Otherwise, render the full frame, which will then load the content.
	c.HTML(http.StatusOK, "frame.html", gin.H{
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "help/tokens",
		"title":     "Chat Tokens",
	})
}

// CommandsHelpGET renders a reference page for console commands parsed from docs/Commands.txt.
func (h *ManagerHandlers) CommandsHelpGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")
	data, err := os.ReadFile(filepath.Join("docs", "Commands.txt"))
	if err != nil {
		h.renderError(c, http.StatusInternalServerError, "Unable to read Commands.txt")
		return
	}
	type cmdInfo struct {
		Name        string
		Usages      []string
		UsageStr    string
		Description string
		Anchor      string
	}
	var commands []cmdInfo
	var letters []string
	seenLetters := map[string]bool{}
	var current *cmdInfo
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Continuation lines: original line had leading space OR no tabs and we have a current block
		if (line != trimmed && current != nil) || (!strings.Contains(line, "\t") && current != nil) {
			if current.Description != "" {
				current.Description += "\n" + trimmed
			} else {
				current.Description = trimmed
			}
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) == 0 {
			continue
		}
		// Special case: some commands (e.g. FILE | -FILE) have multi-line usage and description
		// and forceallowsave line contains ':' which should split name and description.
		name := strings.TrimSpace(parts[0])
		usageRaw := ""
		desc := ""
		if len(parts) > 1 {
			usageRaw = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			desc = strings.TrimSpace(parts[len(parts)-1])
		}
		// If only name present but contains a colon, split at first ':' into name (left) and desc (right)
		if usageRaw == "" && desc == "" {
			if idx := strings.Index(name, ":"); idx > 0 {
				desc = strings.TrimSpace(name[idx+1:])
				name = strings.TrimSpace(name[:idx])
			}
		}
		usageRaw = strings.TrimSuffix(strings.TrimPrefix(usageRaw, "["), "]")
		var usages []string
		if usageRaw != "" {
			for _, u := range strings.Split(usageRaw, ",") {
				ut := strings.TrimSpace(u)
				if ut != "" {
					usages = append(usages, ut)
				}
			}
		}
		usageStr := strings.Join(usages, " ")
		// Determine anchor for first occurrence of starting letter
		letter := ""
		if name != "" {
			r := []rune(strings.ToUpper(string(name[0])))
			if len(r) > 0 {
				letter = string(r[0])
			}
		}
		anchor := ""
		if letter != "" && !seenLetters[letter] {
			anchor = "cmd-" + letter
			seenLetters[letter] = true
			letters = append(letters, letter)
		}
		info := cmdInfo{Name: name, Usages: usages, UsageStr: usageStr, Description: desc, Anchor: anchor}
		commands = append(commands, info)
		current = &commands[len(commands)-1]
	}

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "commands.html", gin.H{
			"username": username,
			"role":     role,
			"commands": commands,
			"letters":  letters,
		})
		return
	}

	// Otherwise, render the full frame, which will then load the content.
	c.HTML(http.StatusOK, "frame.html", gin.H{
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "help/commands",
		"title":     "Console Commands",
		"commands":  commands,
		"letters":   letters,
	})
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
	// Parse beta first, since resolving world name may depend on it.
	beta := srv.Beta
	if qb := strings.TrimSpace(c.Query("beta")); qb != "" {
		if parsed, perr := strconv.ParseBool(qb); perr == nil {
			beta = parsed
		}
	}
	worldID := srv.WorldID
	if w := strings.TrimSpace(c.Query("world")); w != "" {
		// If the client sent a localized display name, resolve it to the technical ID
		if resolved := h.manager.ResolveWorldID(w, beta); strings.TrimSpace(resolved) != "" {
			worldID = resolved
		} else {
			worldID = w
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

// ManagerLogGET renders the manager log viewer page.
func (h *ManagerHandlers) ManagerLogGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	if role != "admin" {
		h.renderError(c, http.StatusForbidden, "Admin privileges required.")
		return
	}

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "logs.html", gin.H{
			"username": username,
			"role":     role,
		})
		return
	}

	// Otherwise, render the full frame, which will then load the content.
	c.HTML(http.StatusOK, "frame.html", gin.H{
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "logs/sdsm",
		"title":     "Manager Logs",
	})
}

// UpdateLogGET renders the update log viewer page.
func (h *ManagerHandlers) UpdateLogGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	if role != "admin" {
		h.renderError(c, http.StatusForbidden, "Admin privileges required.")
		return
	}

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "logs.html", gin.H{
			"username": username,
			"role":     role,
		})
		return
	}

	// Otherwise, render the full frame, which will then load the content.
	c.HTML(http.StatusOK, "frame.html", gin.H{
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "logs/update",
		"title":     "Update Logs",
	})
}

// render wraps c.HTML with a panic-recovery mechanism to log template execution errors.
// This helps diagnose blank-page issues where a template error causes a panic.
func (h *ManagerHandlers) render(c *gin.Context, name string, data gin.H) {
	defer func() {
		if r := recover(); r != nil {
			// Log the panic
			err := fmt.Errorf("panic recovered while rendering template %s: %v", name, r)
			if h.manager != nil && h.manager.Log != nil {
				h.manager.Log.Write(err.Error())
			} else {
				log.Println(err)
			}
			// Also log stack trace for more context
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			stackTrace := string(buf[:n])
			if h.manager != nil && h.manager.Log != nil {
				h.manager.Log.Write(fmt.Sprintf("Stack trace:\n%s", stackTrace))
			} else {
				log.Printf("Stack trace:\n%s", stackTrace)
			}

			// Return an error to the user.
			// Avoid re-rendering another template here to prevent recursive panics.
			c.String(http.StatusInternalServerError, "An internal error occurred while rendering the page. Please check the SDSM logs for details.")
		}
	}()

	// Add common data to every render call that uses the frame
	if name == "frame.html" {
		if _, ok := data["active"]; !ok {
			data["active"] = h.manager.IsActive()
		}
		if _, ok := data["buildTime"]; !ok {
			data["buildTime"] = h.manager.BuildTime()
		}
		if _, ok := data["servers"]; !ok {
			// This is a simplified server list for the frame navigation.
			// The specific page handler should provide the fully filtered list if needed.
			data["servers"] = h.manager.Servers
		}
	}

	c.HTML(http.StatusOK, name, data)
}
