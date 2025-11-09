// Package models defines core runtime state for servers, clients, and
// related data structures used by SDSM.
package models

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	fs "io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"sdsm/internal/utils"
)

const DefaultRestartDelaySeconds = 10
const playersLogFileName = "players.log"
const maxChatMessages = 200

// Client represents a player connection on a server, including
// timestamps for connect/disconnect and whether they are an admin.
type Client struct {
	SteamID            string     `json:"steam_id"`
	Name               string     `json:"name"`
	ConnectDatetime    time.Time  `json:"connect_datetime"`
	DisconnectDatetime *time.Time `json:"disconnect_datetime,omitempty"`
	IsAdmin            bool       `json:"is_admin"`
}

// IsOnline reports whether the client is currently connected.
func (c *Client) IsOnline() bool {
	return c != nil && c.DisconnectDatetime == nil
}

// SessionDuration returns the elapsed time of the current or last session.
func (c *Client) SessionDuration() time.Duration {
	if c == nil {
		return 0
	}
	end := time.Now()
	if c.DisconnectDatetime != nil {
		end = *c.DisconnectDatetime
	}
	if end.Before(c.ConnectDatetime) {
		return 0
	}
	return end.Sub(c.ConnectDatetime)
}

// SessionDurationString returns the duration formatted as HH:MM:SS.
func (c *Client) SessionDurationString() string {
	d := c.SessionDuration()
	if d <= 0 {
		return "00:00:00"
	}
	totalSeconds := int(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// Chat is a single chat message captured from the server log stream.
type Chat struct {
	Datetime time.Time `json:"datetime"`
	Name     string    `json:"name"`
	Message  string    `json:"message"`
}

// ServerConfig is a lightweight input model used to create a new server.
// It mirrors fields persisted on Server for initial setup.
type ServerConfig struct {
	Name           string
	World          string
	WorldID        string
	Language       string
	StartLocation  string
	StartCondition string
	Difficulty     string
	Port           int
	Password       string
	AuthSecret     string
	MaxClients     int
	SaveInterval   int
	Visible        bool
	Beta           bool
	AutoStart      bool
	AutoUpdate     bool
	AutoSave       bool
	AutoPause      bool
	// Additional server settings
	MaxAutoSaves          int
	MaxQuickSaves         int
	DeleteSkeletonOnDecay bool
	UseSteamP2P           bool
	DisconnectTimeout     int
	// PlayerSaves enables automatic save creation when players connect.
	PlayerSaves bool
	// ShutdownDelaySeconds: seconds to wait after Stop is requested before issuing QUIT
	ShutdownDelaySeconds int
	Mods                 []string
	RestartDelaySeconds  int
	// WelcomeMessage is an optional chat message broadcast when a player connects
	WelcomeMessage     string
	WelcomeBackMessage string // Sent to returning players (present in players log)
	// WelcomeDelaySeconds controls how long to wait after connect before sending welcome/welcome-back
	WelcomeDelaySeconds int
	// BepInExInitTimeoutSeconds controls how long the initial bootstrap run is allowed
	// to execute on Windows before being force-killed. Defaults to 10 seconds when absent.
	BepInExInitTimeoutSeconds int
}

// Server represents a managed dedicated server instance, including
// runtime state, settings, logs, and client/chat history.
type Server struct {
	ID             int            `json:"id"`
	Proc           *exec.Cmd      `json:"-"`
	Thrd           chan bool      `json:"-"`
	stdin          io.WriteCloser `json:"-"` // deprecated: stdin fallback removed; retained for struct compatibility
	Logger         *utils.Logger  `json:"-"`
	Paths          *utils.Paths   `json:"-"`
	Name           string         `json:"name"`
	World          string         `json:"world"`
	WorldID        string         `json:"world_id"`
	Language       string         `json:"language"`
	StartLocation  string         `json:"start_location"`
	StartCondition string         `json:"start_condition"`
	Difficulty     string         `json:"difficulty"`
	Port           int            `json:"port"`
	SaveInterval   int            `json:"save_interval"`
	AuthSecret     string         `json:"auth_secret"`
	Password       string         `json:"password"`
	MaxClients     int            `json:"max_clients"`
	Visible        bool           `json:"visible"`
	Beta           bool           `json:"beta"`
	AutoStart      bool           `json:"auto_start"`
	AutoUpdate     bool           `json:"auto_update"`
	AutoSave       bool           `json:"auto_save"`
	AutoPause      bool           `json:"auto_pause"`
	// Additional server settings persisted in sdsm.config
	MaxAutoSaves          int  `json:"max_auto_saves"`
	MaxQuickSaves         int  `json:"max_quick_saves"`
	DeleteSkeletonOnDecay bool `json:"delete_skeleton_on_decay"`
	UseSteamP2P           bool `json:"use_steam_p2p"`
	DisconnectTimeout     int  `json:"disconnect_timeout"`
	// PlayerSaves persists the preference to auto-save when players connect
	PlayerSaves bool `json:"player_saves"`
	// PlayerSaveExcludes lists Steam IDs for which player-save automation should be skipped
	PlayerSaveExcludes []string `json:"player_save_excludes"`
	// ShutdownDelaySeconds controls how long to wait after stop button before issuing QUIT
	ShutdownDelaySeconds      int      `json:"shutdown_delay_seconds"`
	Mods                      []string `json:"mods"`
	RestartDelaySeconds       int      `json:"restart_delay_seconds"`
	SCONPort                  int      `json:"scon_port"`                    // Port for SCON plugin HTTP API
	BepInExInitTimeoutSeconds int      `json:"bepinex_init_timeout_seconds"` // Timeout for initial BepInEx bootstrap (Windows)
	// Detached indicates this server should start in its own process group (Unix) and remain
	// running if the manager exits. It is derived from Manager.DetachedServers and is not
	// persisted independently in sdsm.config.
	Detached bool `json:"-"`
	// WelcomeMessage is sent as a chat/SAY message each time a player connects (if non-empty)
	WelcomeMessage      string        `json:"welcome_message"`
	WelcomeBackMessage  string        `json:"welcome_back_message"`
	WelcomeDelaySeconds int           `json:"welcome_delay_seconds"`
	ServerStarted       *time.Time    `json:"server_started,omitempty"`
	ServerSaved         *time.Time    `json:"server_saved,omitempty"`
	Clients             []*Client     `json:"-"`
	Chat                []*Chat       `json:"-"`
	Storming            bool          `json:"-"`
	Paused              bool          `json:"-"`
	Starting            bool          `json:"-"`
	Stopping            bool          `json:"-"` // set true during shutdown delay window
	StoppingEnds        time.Time     `json:"-"` // timestamp when shutdown expected to occur
	StoppingCancel      chan struct{} `json:"-"` // cancellation channel for delayed shutdown
	Running             bool          `json:"-"`
	LastLogLine         string        `json:"-"`
	// LastError is a human-readable description of the last fatal/startup error detected from logs.
	LastError string `json:"last_error,omitempty"`
	// LastErrorAt records when LastError was updated.
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
	// PendingSavePurge is set when core start parameters (world/start location/start condition)
	// have changed and we plan to purge saves before next start. This is currently a stub and
	// does not perform deletion until implemented.
	PendingSavePurge    bool `json:"pending_save_purge,omitempty"`
	progressReporter    func(stage string, processed, total int64)
	restartMu           sync.Mutex
	playerHistoryLoaded bool
	playersLogDirty     bool
	// pendingPlayerSave holds filenames (e.g., ddmmyy_hhmmss_steamid.save) queued to move
	// from manualsave to playersave once the game logs indicate the save completed.
	pendingPlayerSaveMu  sync.Mutex
	pendingPlayerSave    []string
	lastPlayerSaveQueued map[string]time.Time
	// Transient state for parsing CLIENTS response blocks from logs
	clientsScanActive bool
	clientsScanSeen   map[string]struct{}
	clientsScanStart  time.Time
}

// beginClientsScan initializes a transient scan of currently connected clients based on
// a CLIENTS response block in the server log.
func (s *Server) beginClientsScan() {
	s.clientsScanActive = true
	if s.clientsScanSeen == nil {
		s.clientsScanSeen = make(map[string]struct{})
	} else {
		for k := range s.clientsScanSeen {
			delete(s.clientsScanSeen, k)
		}
	}
	s.clientsScanStart = time.Now()
}

// noteClientsScan records a seen client (steamID or name) as part of the active scan and
// ensures the client is present and marked online.
func (s *Server) noteClientsScan(steamID, name string, when time.Time) {
	if !s.clientsScanActive {
		return
	}
	id := strings.TrimSpace(steamID)
	nm := strings.TrimSpace(name)
	key := id
	if key == "" {
		key = strings.ToLower(nm)
	}
	if key == "" {
		return
	}
	s.clientsScanSeen[key] = struct{}{}
	// Ensure client exists and is online
	// Try by SteamID first
	for _, existing := range s.Clients {
		if existing == nil {
			continue
		}
		if id != "" && strings.EqualFold(existing.SteamID, id) {
			// clear disconnect if previously set
			existing.DisconnectDatetime = nil
			if existing.Name == "" && nm != "" {
				existing.Name = nm
			}
			return
		}
	}
	// Fallback by name (if no SteamID)
	if id == "" && nm != "" {
		for _, existing := range s.Clients {
			if existing == nil {
				continue
			}
			if strings.EqualFold(existing.Name, nm) {
				existing.DisconnectDatetime = nil
				return
			}
		}
	}
	// Not found; add a new online client entry
	if when.IsZero() {
		when = time.Now()
	}
	c := &Client{SteamID: id, Name: nm, ConnectDatetime: when}
	if s.recordClientSession(c) {
		s.appendPlayerLog(c)
	}
}

// endClientsScan finalizes a CLIENTS scan by marking any currently live clients not seen
// in the scan as disconnected now.
func (s *Server) endClientsScan() {
	if !s.clientsScanActive {
		return
	}
	now := time.Now()
	seen := s.clientsScanSeen
	for _, existing := range s.LiveClients() {
		if existing == nil {
			continue
		}
		k := strings.TrimSpace(existing.SteamID)
		if k == "" {
			k = strings.ToLower(strings.TrimSpace(existing.Name))
		}
		if k == "" {
			continue
		}
		if _, ok := seen[k]; !ok {
			existing.DisconnectDatetime = &now
		}
	}
	s.rewritePlayersLog()
	s.clientsScanActive = false
}

// queuePendingPlayerSave enqueues a manual save filename to be moved to playersave once completed.
func (s *Server) queuePendingPlayerSave(filename string) {
	if s == nil || strings.TrimSpace(filename) == "" {
		return
	}
	s.pendingPlayerSaveMu.Lock()
	// Init structures lazily
	if s.lastPlayerSaveQueued == nil {
		s.lastPlayerSaveQueued = make(map[string]time.Time)
	}
	// Skip duplicate filenames already queued
	for _, f := range s.pendingPlayerSave {
		if strings.EqualFold(f, filename) {
			s.pendingPlayerSaveMu.Unlock()
			return
		}
	}
	s.pendingPlayerSave = append(s.pendingPlayerSave, filename)
	s.pendingPlayerSaveMu.Unlock()
	if s.Logger != nil {
		s.Logger.Write("Queued player save for move: " + filename)
	}
}

// shouldEnqueuePlayerSave dedupes rapid successive queues per steam ID.
// Returns true if we should enqueue now; enforce a min interval of 10s per SteamID.
func (s *Server) shouldEnqueuePlayerSave(steamID string, at time.Time) bool {
	if s == nil {
		return false
	}
	id := strings.TrimSpace(steamID)
	if id == "" {
		return false
	}
	s.pendingPlayerSaveMu.Lock()
	defer s.pendingPlayerSaveMu.Unlock()
	if s.lastPlayerSaveQueued == nil {
		s.lastPlayerSaveQueued = make(map[string]time.Time)
	}
	prev, ok := s.lastPlayerSaveQueued[id]
	if ok {
		// if previous within 10 seconds, skip
		if at.Sub(prev) < 10*time.Second {
			return false
		}
	}
	s.lastPlayerSaveQueued[id] = at
	return true
}

// tryMoveNextPendingPlayerSave attempts to move the oldest queued manual save into playersave.
// If the source file is not yet present, it leaves the item queued for a future attempt.
func (s *Server) tryMoveNextPendingPlayerSave() {
	if s == nil || s.Paths == nil || strings.TrimSpace(s.Name) == "" {
		return
	}
	s.pendingPlayerSaveMu.Lock()
	defer s.pendingPlayerSaveMu.Unlock()
	if len(s.pendingPlayerSave) == 0 {
		return
	}
	fname := s.pendingPlayerSave[0]
	srcDir := filepath.Join(s.Paths.ServerSavesDir(s.ID), s.Name, "manualsave")
	dstDir := filepath.Join(s.Paths.ServerSavesDir(s.ID), s.Name, "playersave")
	_ = os.MkdirAll(dstDir, 0o755)
	srcPath := filepath.Join(srcDir, fname)
	dstPath := filepath.Join(dstDir, fname)
	// If expected file isn't present yet, check for legacy double-extension case (".save.save")
	if _, err := os.Stat(srcPath); err != nil {
		legacySrc := srcPath + ".save"
		if _, err2 := os.Stat(legacySrc); err2 == nil {
			// Move and normalize to single-extension at destination
			if errMove := os.Rename(legacySrc, dstPath); errMove != nil {
				if s.Logger != nil {
					s.Logger.Write(fmt.Sprintf("Failed to normalize and move player save %s -> %s: %v", legacySrc, dstPath, errMove))
				}
				return
			}
			if s.Logger != nil {
				s.Logger.Write("Normalized double-extension and moved player save to playersave: " + dstPath)
			}
			// Pop the moved filename
			s.pendingPlayerSave = s.pendingPlayerSave[1:]
			return
		}
		// Not yet on disk; keep queued and try again on next save completion
		if s.Logger != nil {
			s.Logger.Write("Pending player save not found yet: " + srcPath)
		}
		return
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to move player save %s -> %s: %v", srcPath, dstPath, err))
		}
		return
	}
	if s.Logger != nil {
		s.Logger.Write("Moved player save to playersave: " + dstPath)
	}
	// Pop the moved filename
	s.pendingPlayerSave = s.pendingPlayerSave[1:]
}

// HasPlayerSaveExclude returns true if the Steam ID is present in the exclusion list.
func (s *Server) HasPlayerSaveExclude(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || s == nil {
		return false
	}
	for _, v := range s.PlayerSaveExcludes {
		if strings.EqualFold(v, id) {
			return true
		}
	}
	return false
}

// AddPlayerSaveExclude appends a Steam ID to the exclusion list if missing.
func (s *Server) AddPlayerSaveExclude(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || s == nil {
		return false
	}
	if s.HasPlayerSaveExclude(id) {
		return false
	}
	s.PlayerSaveExcludes = append(s.PlayerSaveExcludes, id)
	return true
}

func (s *Server) EnsureLogger(paths *utils.Paths) {
	if paths == nil {
		return
	}

	s.Paths = paths

	if s.Logger != nil {
		s.Logger.Close()
	}

	s.Logger = utils.NewLogger(paths.ServerLogFile(s.ID))
	s.loadPlayerHistory()
}

func (s *Server) playersLogPath() string {
	if s.Paths == nil {
		return ""
	}
	return filepath.Join(s.Paths.ServerLogsDir(s.ID), playersLogFileName)
}

// blacklistPath returns the canonical path to the server's Blacklist.txt in the deployed game/bin directory.
func (s *Server) blacklistPath() string {
	if s.Paths == nil {
		return ""
	}
	return filepath.Join(s.Paths.ServerGameDir(s.ID), "Blacklist.txt")
}

// ReadBlacklistIDs reads a comma-separated list of Steam IDs from Blacklist.txt and returns a unique, trimmed list.
// ReadBlacklistIDs reads the server's Blacklist.txt and returns unique Steam IDs.
func (s *Server) ReadBlacklistIDs() []string {
	path := s.blacklistPath()
	if path == "" {
		return []string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	raw := string(data)
	// Support commas and newlines as separators just in case
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, "\r", ",")
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{})
	var ids []string
	for _, p := range parts {
		id := strings.TrimSpace(p)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// WriteBlacklistIDs writes the provided IDs back to Blacklist.txt as a single comma-separated line.
func (s *Server) WriteBlacklistIDs(ids []string) error {
	path := s.blacklistPath()
	if path == "" {
		return fmt.Errorf("blacklist path unavailable")
	}
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Normalize and de-dup
	seen := make(map[string]struct{})
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	content := strings.Join(out, ",")
	return os.WriteFile(path, []byte(content), 0o644)
}

// AddBlacklistID appends the id to Blacklist.txt if not already present.
func (s *Server) AddBlacklistID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty steam id")
	}
	ids := s.ReadBlacklistIDs()
	for _, existing := range ids {
		if existing == id {
			return nil
		}
	}
	ids = append(ids, id)
	return s.WriteBlacklistIDs(ids)
}

// RemoveBlacklistID removes the id from Blacklist.txt if present.
func (s *Server) RemoveBlacklistID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty steam id")
	}
	ids := s.ReadBlacklistIDs()
	if len(ids) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(ids))
	for _, existing := range ids {
		if existing == id {
			continue
		}
		filtered = append(filtered, existing)
	}
	// Always write back to ensure file exists and duplicates removed
	return s.WriteBlacklistIDs(filtered)
}

// ResolveNameForSteamID finds the most recent known player name for the given Steam ID based on history.
func (s *Server) ResolveNameForSteamID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	// Ensure history is loaded
	s.loadPlayerHistory()
	// Scan from end to get most recent
	for i := len(s.Clients) - 1; i >= 0; i-- {
		c := s.Clients[i]
		if c == nil {
			continue
		}
		if c.SteamID == id && strings.TrimSpace(c.Name) != "" {
			return c.Name
		}
	}
	return ""
}

type BannedEntry struct {
	SteamID string `json:"steam_id"`
	Name    string `json:"name"`
}

// BannedEntries returns the list of banned Steam IDs with best-effort names from player history.
// BannedEntries returns banned Steam IDs with best-effort player names.
func (s *Server) BannedEntries() []BannedEntry {
	ids := s.ReadBlacklistIDs()
	if len(ids) == 0 {
		return []BannedEntry{}
	}
	result := make([]BannedEntry, 0, len(ids))
	for _, id := range ids {
		name := s.ResolveNameForSteamID(id)
		result = append(result, BannedEntry{SteamID: id, Name: name})
	}
	return result
}

func (s *Server) loadPlayerHistory() {
	if s.playerHistoryLoaded {
		return
	}
	path := s.playersLogPath()
	if path == "" {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to open players log: %v", err))
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	deduped := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 5 {
			continue
		}
		connect, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		var disconnect *time.Time
		if fields[3] != "" {
			if t, err := time.Parse(time.RFC3339, fields[3]); err == nil {
				disconnect = &t
			}
		}
		isAdmin := false
		if len(fields) >= 6 {
			isAdmin = strings.TrimSpace(fields[5]) == "1"
		}
		client := &Client{
			SteamID:            fields[0],
			Name:               fields[1],
			ConnectDatetime:    connect,
			DisconnectDatetime: disconnect,
			IsAdmin:            isAdmin,
		}
		if !s.recordClientSession(client) {
			deduped = true
		}
	}
	if err := scanner.Err(); err != nil && s.Logger != nil {
		s.Logger.Write(fmt.Sprintf("Error reading players log: %v", err))
	}
	if deduped {
		s.markPlayersLogDirty()
	}
	s.playerHistoryLoaded = true
}

func (s *Server) appendPlayerLog(c *Client) {
	s.flushPlayersLogIfDirty()

	path := s.playersLogPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to create players log dir: %v", err))
		}
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to open players log for append: %v", err))
		}
		return
	}
	defer file.Close()

	disconnect := ""
	if c.DisconnectDatetime != nil {
		disconnect = c.DisconnectDatetime.Format(time.RFC3339)
	}
	adminFlag := "0"
	if c.IsAdmin {
		adminFlag = "1"
	}
	entry := fmt.Sprintf("%s,%s,%s,%s,%s,%s\n",
		c.SteamID,
		strings.ReplaceAll(c.Name, ",", " "),
		c.ConnectDatetime.Format(time.RFC3339),
		disconnect,
		c.SessionDurationString(),
		adminFlag,
	)
	if _, err := file.WriteString(entry); err != nil && s.Logger != nil {
		s.Logger.Write(fmt.Sprintf("Failed to write players log entry: %v", err))
	}
}

func (s *Server) rewritePlayersLog() {
	path := s.playersLogPath()
	if path == "" {
		return
	}

	entries := make([]string, 0, len(s.Clients))
	seen := make(map[string]struct{})
	for _, client := range s.Clients {
		if client == nil {
			continue
		}
		disconnect := ""
		if client.DisconnectDatetime != nil {
			disconnect = client.DisconnectDatetime.Format(time.RFC3339)
		}
		adminFlag := "0"
		if client.IsAdmin {
			adminFlag = "1"
		}
		key := sessionKey(client.SteamID, client.Name, client.ConnectDatetime)
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		entry := fmt.Sprintf("%s,%s,%s,%s,%s,%s",
			client.SteamID,
			strings.ReplaceAll(client.Name, ",", " "),
			client.ConnectDatetime.Format(time.RFC3339),
			disconnect,
			client.SessionDurationString(),
			adminFlag,
		)
		entries = append(entries, entry)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to create players log dir during update: %v", err))
		}
		return
	}

	tempPath := path + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to open temp players log: %v", err))
		}
		return
	}
	for _, entry := range entries {
		if _, err := file.WriteString(entry + "\n"); err != nil {
			file.Close()
			os.Remove(tempPath)
			if s.Logger != nil {
				s.Logger.Write(fmt.Sprintf("Failed to write temp players log: %v", err))
			}
			return
		}
	}
	if err := file.Close(); err != nil {
		os.Remove(tempPath)
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed closing temp players log: %v", err))
		}
		return
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to replace players log: %v", err))
		}
		return
	}

	s.playersLogDirty = false
}

// LiveClients returns currently connected clients only (empty slice if none).
func (s *Server) LiveClients() []*Client {
	if len(s.Clients) == 0 {
		return []*Client{}
	}
	live := make([]*Client, 0, len(s.Clients))
	for _, client := range s.Clients {
		if client != nil && client.IsOnline() {
			live = append(live, client)
		}
	}
	return live
}

func (s *Server) addChatMessage(name string, when time.Time, message string) {
	name = strings.TrimSpace(name)
	message = strings.TrimSpace(message)
	if name == "" || message == "" {
		return
	}
	if when.IsZero() {
		when = time.Now()
	}
	s.Chat = append(s.Chat, &Chat{
		Datetime: when,
		Name:     name,
		Message:  message,
	})
	if excess := len(s.Chat) - maxChatMessages; excess > 0 {
		s.Chat = append([]*Chat(nil), s.Chat[excess:]...)
	}
}

// LastConnectedPlayerName returns the display name of the most recent player to connect
// based on ConnectDatetime across all known client sessions. Returns an empty string
// when no player history exists.
func (s *Server) LastConnectedPlayerName() string {
	if s == nil || len(s.Clients) == 0 {
		return ""
	}
	var latest *Client
	for _, c := range s.Clients {
		if c == nil {
			continue
		}
		if latest == nil || c.ConnectDatetime.After(latest.ConnectDatetime) {
			latest = c
		}
	}
	if latest == nil {
		return ""
	}
	return strings.TrimSpace(latest.Name)
}

// expandChatTokens performs token substitution for chat/SAY messages.
// Supported tokens (case-insensitive, bracketed):
//
//	[ServerName], [WorldName], [WorldID], [StartLocation], [StartCondition],
//	[Date], [Time], [LastPlayer]
func (s *Server) expandChatTokens(msg string) string {
	if s == nil || strings.TrimSpace(msg) == "" {
		return msg
	}
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")
	lastPlayer := s.LastConnectedPlayerName()
	tokens := map[string]string{
		"servername":     strings.TrimSpace(s.Name),
		"worldname":      strings.TrimSpace(s.World),
		"worldid":        strings.TrimSpace(s.WorldID),
		"startlocation":  strings.TrimSpace(s.StartLocation),
		"startcondition": strings.TrimSpace(s.StartCondition),
		"date":           dateStr,
		"time":           timeStr,
		"lastplayer":     lastPlayer,
	}
	// Fast path: check if there's a '[' present at all
	if !strings.Contains(msg, "[") {
		return msg
	}
	// Replace tokens in a single pass by scanning for bracketed words
	var b strings.Builder
	b.Grow(len(msg))
	for i := 0; i < len(msg); {
		ch := msg[i]
		if ch != '[' {
			b.WriteByte(ch)
			i++
			continue
		}
		// Attempt to parse [Token]
		end := strings.IndexByte(msg[i+1:], ']')
		if end < 0 {
			// No closing bracket; write the rest and break
			b.WriteString(msg[i:])
			break
		}
		end += i + 1
		key := strings.TrimSpace(msg[i+1 : end])
		lower := strings.ToLower(key)
		if val, ok := tokens[lower]; ok {
			b.WriteString(val)
		} else {
			// Not a recognized token; keep original brackets
			b.WriteString(msg[i : end+1])
		}
		i = end + 1
	}
	return b.String()
}

func (s *Server) resetChat() {
	if len(s.Chat) == 0 {
		s.Chat = nil
		return
	}
	for i := range s.Chat {
		s.Chat[i] = nil
	}
	s.Chat = nil
}

func (s *Server) markClientAdmin(name, steamID string) {
	if name == "" && steamID == "" {
		return
	}

	updated := false
	for _, client := range s.Clients {
		if client == nil {
			continue
		}
		if steamID != "" && client.SteamID == steamID {
			if !client.IsAdmin {
				client.IsAdmin = true
				updated = true
			}
			continue
		}
		if name != "" && strings.EqualFold(client.Name, name) {
			if !client.IsAdmin {
				client.IsAdmin = true
				updated = true
			}
		}
	}

	if updated {
		s.markPlayersLogDirty()
		s.flushPlayersLogIfDirty()
	}
}

func sessionKey(steamID, name string, connect time.Time) string {
	id := strings.TrimSpace(steamID)
	if id == "" {
		id = strings.ToLower(strings.TrimSpace(name))
	}
	if id == "" {
		return ""
	}
	return fmt.Sprintf("%s|%s", id, connect.UTC().Format(time.RFC3339Nano))
}

func mergeClientSession(target, source *Client) {
	if target == nil || source == nil {
		return
	}
	if source.Name != "" {
		target.Name = source.Name
	}
	if source.SteamID != "" {
		target.SteamID = source.SteamID
	}
	if !source.ConnectDatetime.IsZero() {
		target.ConnectDatetime = source.ConnectDatetime
	}
	if source.DisconnectDatetime != nil {
		target.DisconnectDatetime = source.DisconnectDatetime
	}
	if source.IsAdmin {
		target.IsAdmin = true
	}
}

func sameSession(existing, candidate *Client) bool {
	if existing == nil || candidate == nil {
		return false
	}
	if existing.SteamID != "" && candidate.SteamID != "" && existing.SteamID == candidate.SteamID {
		diff := existing.ConnectDatetime.Sub(candidate.ConnectDatetime)
		if diff < 0 {
			diff = -diff
		}
		if diff <= time.Second {
			return true
		}
	}
	if existing.SteamID == "" && candidate.SteamID == "" && existing.Name != "" && candidate.Name != "" && strings.EqualFold(existing.Name, candidate.Name) {
		diff := existing.ConnectDatetime.Sub(candidate.ConnectDatetime)
		if diff < 0 {
			diff = -diff
		}
		if diff <= time.Second {
			return true
		}
	}
	return false
}

func (s *Server) recordClientSession(c *Client) bool {
	if c == nil {
		return false
	}

	key := sessionKey(c.SteamID, c.Name, c.ConnectDatetime)
	if key != "" {
		for _, existing := range s.Clients {
			if existing == nil {
				continue
			}
			if sessionKey(existing.SteamID, existing.Name, existing.ConnectDatetime) == key {
				mergeClientSession(existing, c)
				s.markPlayersLogDirty()
				return false
			}
		}
	}

	for _, existing := range s.Clients {
		if sameSession(existing, c) {
			mergeClientSession(existing, c)
			s.markPlayersLogDirty()
			return false
		}
	}

	s.Clients = append(s.Clients, c)
	return true
}

func (s *Server) markPlayersLogDirty() {
	s.playersLogDirty = true
}

func (s *Server) flushPlayersLogIfDirty() {
	if !s.playersLogDirty {
		return
	}
	s.rewritePlayersLog()
}

func NewServerFromConfig(serverID int, paths *utils.Paths, cfg *ServerConfig) *Server {
	cfgProvided := cfg != nil
	if cfg == nil {
		cfg = &ServerConfig{}
	}

	s := &Server{
		ID:       serverID,
		Paths:    paths,
		Clients:  []*Client{},
		Chat:     []*Chat{},
		Mods:     append([]string{}, cfg.Mods...),
		Paused:   false,
		Starting: false,
		Running:  false,
	}

	if len(s.Mods) == 0 {
		s.Mods = []string{}
	}

	if cfg.Name != "" {
		s.Name = cfg.Name
	} else {
		s.Name = fmt.Sprintf("Stationeers Server %d", serverID)
	}

	if cfg.World != "" {
		s.World = cfg.World
	} else {
		s.World = "Mars2"
	}

	s.WorldID = strings.TrimSpace(cfg.WorldID)
	if s.WorldID == "" {
		s.WorldID = s.World
	}

	if cfg.Language != "" {
		s.Language = cfg.Language
	} else {
		// Default language to English when unspecified
		s.Language = "English"
	}

	if cfg.StartLocation != "" {
		s.StartLocation = cfg.StartLocation
	}

	if cfg.StartCondition != "" {
		s.StartCondition = cfg.StartCondition
	}

	if cfg.Difficulty != "" {
		s.Difficulty = cfg.Difficulty
	} else {
		s.Difficulty = "Normal"
	}

	if cfg.Port > 0 {
		s.Port = cfg.Port
	} else {
		s.Port = 26017
	}
	// Default SCON port to game port + 1 unless explicitly set elsewhere
	if s.SCONPort == 0 && s.Port > 0 {
		s.SCONPort = s.Port + 1
	}

	if cfg.SaveInterval > 0 {
		s.SaveInterval = cfg.SaveInterval
	} else {
		s.SaveInterval = 300
	}

	if cfg.MaxClients > 0 {
		s.MaxClients = cfg.MaxClients
	} else {
		s.MaxClients = 10
	}

	s.Password = cfg.Password
	s.AuthSecret = cfg.AuthSecret
	s.Visible = cfg.Visible
	if !cfgProvided {
		s.Visible = true
	}

	s.Beta = cfg.Beta
	s.AutoStart = cfg.AutoStart
	s.AutoUpdate = cfg.AutoUpdate
	s.AutoSave = cfg.AutoSave
	if !cfgProvided {
		s.AutoSave = true
	}
	s.AutoPause = cfg.AutoPause
	if !cfgProvided {
		s.AutoPause = true
	}
	// Persist extended settings with defaults when not provided
	if cfg.MaxAutoSaves > 0 {
		s.MaxAutoSaves = cfg.MaxAutoSaves
	} else {
		s.MaxAutoSaves = 5
	}
	if cfg.MaxQuickSaves > 0 {
		s.MaxQuickSaves = cfg.MaxQuickSaves
	} else {
		s.MaxQuickSaves = 5
	}
	s.DeleteSkeletonOnDecay = cfg.DeleteSkeletonOnDecay
	s.UseSteamP2P = cfg.UseSteamP2P
	if cfg.DisconnectTimeout > 0 {
		s.DisconnectTimeout = cfg.DisconnectTimeout
	} else {
		s.DisconnectTimeout = 10000
	}
	// Persist PlayerSaves preference (defaults to false when not provided)
	s.PlayerSaves = cfg.PlayerSaves
	// Persist ShutdownDelaySeconds (defaults to 2 when not provided/negative)
	if cfg.ShutdownDelaySeconds >= 0 {
		s.ShutdownDelaySeconds = cfg.ShutdownDelaySeconds
	} else {
		s.ShutdownDelaySeconds = 2
	}

	// Welcome message (optional)
	s.WelcomeMessage = strings.TrimSpace(cfg.WelcomeMessage)
	s.WelcomeBackMessage = strings.TrimSpace(cfg.WelcomeBackMessage)
	if cfg.WelcomeDelaySeconds >= 0 {
		s.WelcomeDelaySeconds = cfg.WelcomeDelaySeconds
	} else {
		s.WelcomeDelaySeconds = 1
	}

	if cfg.RestartDelaySeconds >= 0 {
		s.RestartDelaySeconds = cfg.RestartDelaySeconds
	} else {
		s.RestartDelaySeconds = DefaultRestartDelaySeconds
	}

	// BepInEx init timeout (Windows only usage). Default 10s.
	if cfg.BepInExInitTimeoutSeconds > 0 {
		s.BepInExInitTimeoutSeconds = cfg.BepInExInitTimeoutSeconds
	} else {
		s.BepInExInitTimeoutSeconds = 10
	}

	s.EnsureLogger(paths)

	return s
}

func NewServer(serverID int, paths *utils.Paths, data string) *Server {
	s := &Server{
		ID:                        serverID,
		Paths:                     paths,
		Clients:                   []*Client{},
		Chat:                      []*Chat{},
		Mods:                      []string{},
		Paused:                    false,
		Running:                   false,
		RestartDelaySeconds:       DefaultRestartDelaySeconds,
		ShutdownDelaySeconds:      2,
		WelcomeDelaySeconds:       1,
		BepInExInitTimeoutSeconds: 10,
	}

	s.EnsureLogger(paths)

	if data != "" {
		json.Unmarshal([]byte(data), s)
		if strings.TrimSpace(s.WorldID) == "" {
			s.WorldID = s.World
		}
		// Ensure a sensible default for language if missing in stored data
		if strings.TrimSpace(s.Language) == "" {
			s.Language = "English"
		}
		if !strings.Contains(data, "restart_delay_seconds") || s.RestartDelaySeconds < 0 {
			s.RestartDelaySeconds = DefaultRestartDelaySeconds
		}
		// Derive/repair SCON port from game port. Since SCON port isn't user-configurable yet,
		// treat it as a derived value and fix stale values after port changes.
		if s.Port > 0 {
			if s.SCONPort == 0 || s.SCONPort != s.Port+1 {
				s.SCONPort = s.Port + 1
			}
		}
		// Backfill defaults for newly introduced fields if absent
		if s.MaxAutoSaves <= 0 {
			s.MaxAutoSaves = 5
		}
		if s.MaxQuickSaves <= 0 {
			s.MaxQuickSaves = 5
		}
		if s.DisconnectTimeout <= 0 {
			s.DisconnectTimeout = 10000
		}
		// Backfill default for ShutdownDelaySeconds if absent (legacy default 2s)
		if s.ShutdownDelaySeconds < 0 {
			s.ShutdownDelaySeconds = 2
		}
		// Backfill WelcomeMessage to empty if missing
		s.WelcomeMessage = strings.TrimSpace(s.WelcomeMessage)
	} else {
		s.Name = fmt.Sprintf("Stationeers Server %d", serverID)
		s.World = "Mars2"
		s.WorldID = s.World
		// Default language to English for new servers
		s.Language = "English"
		s.Difficulty = "Normal"
		s.Port = 26017
		s.SaveInterval = 300
		s.AuthSecret = ""
		s.Password = ""
		s.MaxClients = 10
		s.Visible = true
		s.Beta = false
		s.AutoStart = false
		s.AutoUpdate = false
		s.AutoSave = true
		s.AutoPause = true
		s.MaxAutoSaves = 5
		s.MaxQuickSaves = 5
		s.DeleteSkeletonOnDecay = false
		s.UseSteamP2P = false
		s.DisconnectTimeout = 10000
		s.PlayerSaves = false
		s.SCONPort = s.Port + 1
		s.SCONPort = s.Port + 1
		s.WelcomeMessage = ""
	}

	s.Logger.Write("Server initialized.")

	if s.AutoUpdate {
		if err := s.Deploy(); err != nil && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("AutoUpdate deploy failed: %v", err))
		}
	}

	if s.AutoStart {
		s.Start()
	}

	return s
}

func (s *Server) ClientCount() int {
	count := 0
	for _, client := range s.Clients {
		if client != nil && client.IsOnline() {
			count++
		}
	}
	return count
}

func (s *Server) IsRunning() bool {
	if s == nil {
		return false
	}

	if s.Proc != nil {
		// Proc.ProcessState remains nil while the process is alive. Prefer that signal.
		if s.Proc.ProcessState == nil {
			s.Running = true
			return true
		}

		// Once the process has exited, ensure our in-memory flags reflect that reality.
		if s.Proc.ProcessState.Exited() {
			s.Running = false
			s.Starting = false
			s.Paused = false
			return false
		}
	}

	if s.Running || s.Starting || s.Paused {
		s.Running = false
		s.Starting = false
		s.Paused = false
	}

	return false
}

func (s *Server) SetProgressReporter(fn func(stage string, processed, total int64)) {
	s.progressReporter = fn
}

func (s *Server) reportProgress(stage string, processed, total int64) {
	if s.progressReporter != nil {
		s.progressReporter(stage, processed, total)
	}
}

// Deploy copies the release/beta game files (and BepInEx/LaunchPad assets)
// into this server's game directory. Progress is reported via the
// progressReporter callback when set.
func (s *Server) Deploy() error {
	if s.Paths == nil {
		if s.Logger != nil {
			s.Logger.Write("Skipping deploy because server paths are not configured")
		}
		return errors.New("server paths are not configured")
	}
	s.Paths.DeployServer(s.ID, s.Logger)

	var src string
	if s.Beta {
		src = s.Paths.BetaDir()
	} else {
		src = s.Paths.ReleaseDir()
	}
	dst := s.Paths.ServerGameDir(s.ID)

	s.Logger.Write(fmt.Sprintf("Deploying server files from %s to %s", src, dst))
	totalFiles, err := countFiles(src)
	if err != nil {
		s.Logger.Write(fmt.Sprintf("Failed to enumerate source files: %v", err))
		return err
	}

	additionalFiles := s.countFilesIfExists(s.Paths.BepInExDir()) + s.countFilesIfExists(s.Paths.LaunchPadDir()) + s.countFilesIfExists(s.Paths.SCONDir())
	if additionalFiles > 0 {
		totalFiles += additionalFiles
	}

	s.reportProgress("Preparing files", 0, totalFiles)

	tracker := newCopyTracker(s, totalFiles)
	if err := s.copyDir(src, dst, tracker); err != nil {
		s.Logger.Write(fmt.Sprintf("Deploy encountered errors: %v", err))
		s.reportProgress("Failed", tracker.processed, tracker.total)
		return err
	}

	if err := s.deployBepInExAssets(dst, tracker); err != nil {
		s.Logger.Write(fmt.Sprintf("Failed to deploy BepInEx assets: %v", err))
		s.reportProgress("Failed", tracker.processed, tracker.total)
		return err
	}

	s.reportProgress("Completed", tracker.processed, tracker.total)

	return nil

	// TODO: add mods
}

func (s *Server) countFilesIfExists(path string) int64 {
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to access %s: %v", path, err))
		}
		return 0
	}
	if !info.IsDir() {
		return 0
	}
	count, err := countFiles(path)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to count files in %s: %v", path, err))
		}
		return 0
	}
	return count
}

func (s *Server) deployBepInExAssets(dst string, tracker *copyTracker) error {
	bepDir := s.Paths.BepInExDir()
	info, err := os.Stat(bepDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("BepInEx is not installed (expected files at %s)", bepDir)
		}
		return fmt.Errorf("failed to access BepInEx directory %s: %w", bepDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("BepInEx path is not a directory: %s", bepDir)
	}

	if s.Logger != nil {
		s.Logger.Write("Copying BepInEx files into server game directory")
	}
	if err := s.copyDir(bepDir, dst, tracker); err != nil {
		return fmt.Errorf("copying BepInEx content: %w", err)
	}

	launchDir := s.Paths.LaunchPadDir()
	launchInfo, err := os.Stat(launchDir)
	if err != nil {
		if os.IsNotExist(err) {
			if s.Logger != nil {
				s.Logger.Write("LaunchPad directory not found; skipping plugin copy")
			}
			return nil
		}
		return fmt.Errorf("failed to access LaunchPad directory %s: %w", launchDir, err)
	}
	if !launchInfo.IsDir() {
		if s.Logger != nil {
			s.Logger.Write("LaunchPad path is not a directory; skipping plugin copy")
		}
		return nil
	}

	pluginsDst := filepath.Join(dst, "BepInEx", "plugins")
	if err := os.MkdirAll(pluginsDst, os.ModePerm); err != nil {
		return fmt.Errorf("creating plugins directory: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Write("Copying LaunchPad files into BepInEx/plugins/StationeersLaunchPad")
	}
	launchpadDst := filepath.Join(pluginsDst, "StationeersLaunchPad")
	if err := os.MkdirAll(launchpadDst, os.ModePerm); err != nil {
		return fmt.Errorf("creating StationeersLaunchPad directory: %w", err)
	}
	if err := s.copyDir(launchDir, launchpadDst, tracker); err != nil {
		return fmt.Errorf("copying LaunchPad content: %w", err)
	}

	// Copy SCON into BepInEx/plugins if present
	sconDir := s.Paths.SCONDir()
	if sconInfo, err := os.Stat(sconDir); err == nil && sconInfo.IsDir() {
		if s.Logger != nil {
			s.Logger.Write("Copying SCON files into BepInEx/plugins")
		}
		if err := s.copyDir(sconDir, pluginsDst, tracker); err != nil {
			return fmt.Errorf("copying SCON content: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to access SCON directory %s: %w", sconDir, err)
	}

	// After copying BepInEx files, run the installer to finalize setup.
	if err := s.installBepInEx(dst); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("BepInEx installation step failed: %v", err))
		}
		// Non-fatal; BepInEx may still work on first server start
	}

	return nil
}

// installBepInEx performs the platform-specific BepInEx installation step.
// On Windows, it does a quick server start/stop to let BepInEx generate its config files.
// On Linux, BepInEx is initialized on every server start via run_bepinex.sh wrapper.
func (s *Server) installBepInEx(gameDir string) error {
	if runtime.GOOS == "windows" {
		if s.Logger != nil {
			s.Logger.Write("Installing BepInEx via initial server run (Windows)")
		}

		// Determine the executable name
		executableName := "rocketstation_DedicatedServer.exe"
		executablePath := filepath.Join(gameDir, executableName)

		if _, err := os.Stat(executablePath); os.IsNotExist(err) {
			return fmt.Errorf("server executable not found at %s", executablePath)
		}

		// Build minimal start command to initialize BepInEx (using default world/settings)
		// We just need the process to run briefly so BepInEx creates its config structure.
		cmd := exec.Command(executablePath, "-batchmode", "-nographics", "-quit")
		cmd.Dir = gameDir

		if s.Logger != nil {
			s.Logger.Write("Starting server briefly to initialize BepInEx config")
		}

		// Run with a timeout in case it doesn't quit cleanly (configurable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start server for BepInEx init: %w", err)
		}

		// Wait for the process to exit or kill after a short timeout
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		timeout := time.Duration(s.BepInExInitTimeoutSeconds)
		if timeout <= 0 {
			timeout = 10
		}
		select {
		case err := <-done:
			// Process exited on its own
			if err != nil && s.Logger != nil {
				s.Logger.Write(fmt.Sprintf("BepInEx init run exited with status: %v", err))
			}
		case <-time.After(timeout * time.Second):
			// Timeout: kill the process
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done // Wait for Wait() to return
		}

		// Validate that BepInEx initialized by checking for a known directory
		bepinexConfigDir := filepath.Join(gameDir, "BepInEx", "config")
		if _, err := os.Stat(bepinexConfigDir); err == nil {
			if s.Logger != nil {
				s.Logger.Write("BepInEx initialization run completed successfully (config detected)")
			}
		} else {
			if s.Logger != nil {
				s.Logger.Write("Warning: BepInEx config directory not found after init run; plugins may not load until first full start")
			}
		}
		return nil
	}

	// Other platforms (macOS, etc.) not yet implemented
	if s.Logger != nil {
		s.Logger.Write(fmt.Sprintf("BepInEx installation not implemented for platform: %s", runtime.GOOS))
	}
	return nil
}

// ToJSON marshals the server into JSON for diagnostics.
// ToJSON removed as it wasn't referenced; prefer explicit logging/diagnostic formatting at call sites.

// Stop terminates the server process (if running), marks clients disconnected,
// flushes logs, and resets in-memory chat when appropriate.
// Stop performs an immediate (blocking) shutdown with any configured delay, without cancellation support.
// Used by internal restart flows and legacy code paths where a synchronous stop is acceptable.
func (s *Server) Stop() {
	if !s.Running || s.Proc == nil {
		// Nothing to stop; mark state and return
		s.Running = false
		s.Stopping = false
		s.Starting = false
		return
	}
	delay := s.shutdownDelayDuration()
	if delay <= 0 {
		// No delay: perform immediate shutdown
		s.performFinalShutdown()
		return
	}
	// Blocking delayed shutdown (legacy path). Sleeps cannot be canceled.
	s.Stopping = true
	s.StoppingEnds = time.Now().Add(delay)
	s.sendShutdownNotices(delay)
	s.performFinalShutdown()
}

// StopAsync schedules a delayed shutdown and returns immediately. If a delay is configured (>0),
// players receive staged notices and the process is terminated after the countdown. A cancellation
// channel allows interruption via CancelStop(). The provided broadcast callback (optional) is invoked
// after state changes (initial schedule, cancellation, final termination) so the UI can refresh promptly.
func (s *Server) StopAsync(broadcast func(*Server)) {
	if s == nil || !s.Running || s.Proc == nil {
		// Fallback to immediate stop logic
		s.Stop()
		if broadcast != nil {
			broadcast(s)
		}
		return
	}
	delay := s.shutdownDelayDuration()
	if delay <= 0 {
		// Immediate path
		s.performFinalShutdown()
		if broadcast != nil {
			broadcast(s)
		}
		return
	}
	if s.Stopping {
		// Already scheduled; nothing to do
		if broadcast != nil {
			broadcast(s)
		}
		return
	}
	s.Stopping = true
	s.StoppingEnds = time.Now().Add(delay)
	// Create a fresh cancellation channel
	s.StoppingCancel = make(chan struct{})
	s.sendInitialShutdownNotice(delay)
	if broadcast != nil {
		broadcast(s)
	}
	go func(cancel <-chan struct{}, srv *Server, d time.Duration, bc func(*Server)) {
		// Countdown logic with 1s ticks to allow responsive cancellation
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		tenSecondNoticeSent := false
		for {
			remaining := time.Until(srv.StoppingEnds)
			if remaining <= 0 {
				break
			}
			if remaining <= 10*time.Second && !tenSecondNoticeSent {
				// Send 10s notice once
				if remaining > 0 {
					srv.sendTenSecondNotice()
				}
				tenSecondNoticeSent = true
			}
			select {
			case <-cancel:
				// Cancellation requested; finalize without stopping process
				if srv.Logger != nil {
					srv.Logger.Write("Shutdown canceled before completion")
				}
				srv.sendCancellationNotice()
				srv.Stopping = false
				srv.StoppingEnds = time.Time{}
				if bc != nil {
					bc(srv)
				}
				return
			case <-ticker.C:
				// continue loop
			}
		}
		// Final notice + QUIT
		srv.sendFinalNotice()
		srv.performFinalShutdown()
		if bc != nil {
			bc(srv)
		}
	}(s.StoppingCancel, s, delay, broadcast)
}

// CancelStop interrupts a pending asynchronous shutdown (if any). Returns true if cancellation occurred.
func (s *Server) CancelStop() bool {
	if s == nil || !s.Stopping {
		return false
	}
	if s.StoppingCancel != nil {
		// Safe close: recover if already closed
		select {
		case <-s.StoppingCancel:
			// already closed
		default:
			close(s.StoppingCancel)
		}
		s.StoppingCancel = nil
		return true
	}
	return false
}

// Helper: send formatted timeframe + initial notice
func (s *Server) sendInitialShutdownNotice(delay time.Duration) {
	s.sendChat("Server is shutting down in " + s.formatTimeframe(delay))
}

func (s *Server) sendTenSecondNotice() { s.sendChat("Server is shutting down in 10 seconds") }
func (s *Server) sendFinalNotice()     { s.sendChat("Server shutting down now") }
func (s *Server) sendCancellationNotice() {
	s.sendChat("Shutdown canceled; server will remain running")
}

func (s *Server) sendChat(msg string) {
	if s == nil || !s.Running || s.Proc == nil {
		return
	}
	expanded := s.RenderChatMessage(msg, nil)
	if err := s.SendCommand("chat", expanded); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to send chat notice '%s': %v", expanded, err))
		}
	}
}

// sendShutdownNotices is used by the legacy blocking Stop() path.
func (s *Server) sendShutdownNotices(delay time.Duration) {
	s.sendInitialShutdownNotice(delay)
	if delay > 10*time.Second {
		time.Sleep(delay - 10*time.Second)
		s.sendTenSecondNotice()
		time.Sleep(10 * time.Second)
	} else {
		time.Sleep(delay)
	}
	s.sendFinalNotice()
}

// performFinalShutdown issues QUIT and finalizes state.
func (s *Server) performFinalShutdown() {
	if s == nil || !s.Running || s.Proc == nil {
		s.Running = false
		s.Stopping = false
		s.Starting = false
		return
	}
	if err := s.SendCommand("console", "QUIT"); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to send QUIT command: %v", err))
		}
	}
	time.Sleep(3 * time.Second)
	s.Running = false
	s.Stopping = false
	s.Starting = false
	now := time.Now()
	s.markAllClientsDisconnected(now)
	s.rewritePlayersLog()
	if s.Proc == nil {
		s.resetChat()
	}
	if s.Proc != nil {
		s.Proc.Process.Kill()
	}
}

// formatTimeframe turns a duration into a player-friendly phrase.
func (s *Server) formatTimeframe(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 60 {
		if secs == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", secs)
	}
	m := secs / 60
	sRem := secs % 60
	if sRem == 0 {
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	if m == 1 {
		if sRem == 1 {
			return "1 minute 1 second"
		}
		return fmt.Sprintf("1 minute %d seconds", sRem)
	}
	if sRem == 1 {
		return fmt.Sprintf("%d minutes 1 second", m)
	}
	return fmt.Sprintf("%d minutes %d seconds", m, sRem)
}

// RenderChatMessage expands supported tokens in outbound SAY/chat messages.
// Supported tokens:
//
//	{player}      - connecting player's name (when provided in ctx)
//	{lastplayer}  - most recent connected player's name (history fallback)
//	{server}      - server name
//	{world}       - current world id/name
//	{time}        - current local time (HH:MM)
//	{date}        - current local date (YYYY-MM-DD)
//
// The ctx map may include { "player": "Name" } during connect events.
func (s *Server) RenderChatMessage(msg string, ctx map[string]string) string {
	if s == nil {
		return msg
	}
	out := msg
	// Helper to replace a token if present
	replace := func(token, value string) {
		out = strings.ReplaceAll(out, "{"+token+"}", value)
	}
	// Player tokens
	var playerName string
	if ctx != nil {
		playerName = strings.TrimSpace(ctx["player"])
	}
	if playerName != "" {
		replace("player", playerName)
	} else if strings.Contains(out, "{player}") {
		replace("player", s.LastConnectedPlayerName())
	}
	if strings.Contains(out, "{lastplayer}") {
		replace("lastplayer", s.LastConnectedPlayerName())
	}
	// Server/world tokens
	replace("server", s.Name)
	w := s.WorldID
	if strings.TrimSpace(w) == "" {
		w = s.World
	}
	replace("world", w)
	// Time/date tokens
	now := time.Now()
	replace("time", now.Format("15:04"))
	replace("date", now.Format("2006-01-02"))
	// Additional tokens
	replace("player_count", fmt.Sprintf("%d", len(s.LiveClients())))
	replace("max_players", fmt.Sprintf("%d", s.MaxClients))
	if s.Port > 0 {
		replace("port", fmt.Sprintf("%d", s.Port))
	}
	replace("difficulty", s.Difficulty)
	replace("language", s.Language)
	replace("beta", func() string {
		if s.Beta {
			return "beta"
		}
		return "release"
	}())
	return out
}

func (s *Server) markAllClientsDisconnected(t time.Time) {
	for _, client := range s.Clients {
		if client != nil && client.DisconnectDatetime == nil {
			disconnected := t
			client.DisconnectDatetime = &disconnected
		}
	}
}

// Restart stops the server, waits for a configured delay, and starts it again.
func (s *Server) Restart() {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()

	if s.Logger != nil {
		s.Logger.Write("Restart requested; stopping server")
	}

	s.Stop()
	s.waitForShutdown(15 * time.Second)

	delay := s.restartDelayDuration()
	if delay > 0 {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Waiting %s before restarting server", delay))
		}
		time.Sleep(delay)
	}

	if s.Logger != nil {
		s.Logger.Write("Restart delay elapsed; starting server")
	}

	s.Start()
}

func (s *Server) restartDelayDuration() time.Duration {
	if s.RestartDelaySeconds > 0 {
		return time.Duration(s.RestartDelaySeconds) * time.Second
	}
	if s.RestartDelaySeconds == 0 {
		return 0
	}
	return time.Duration(DefaultRestartDelaySeconds) * time.Second
}

func (s *Server) shutdownDelayDuration() time.Duration {
	if s == nil {
		return 0
	}
	if s.ShutdownDelaySeconds > 0 {
		return time.Duration(s.ShutdownDelaySeconds) * time.Second
	}
	if s.ShutdownDelaySeconds == 0 {
		return 0
	}
	// Negative values fallback to legacy default of 2s
	return 2 * time.Second
}

func (s *Server) waitForShutdown(timeout time.Duration) {
	if timeout <= 0 {
		s.simpleShutdownWait()
		return
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if s.Proc == nil || (s.Proc.ProcessState != nil && s.Proc.ProcessState.Exited()) {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			if s.Logger != nil {
				s.Logger.Write("Timed out waiting for server to shut down before restart")
			}
			return
		}
	}
}

func (s *Server) simpleShutdownWait() {
	for s.Proc != nil && s.Proc.ProcessState == nil {
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Server) Start() {
	if s.Logger != nil {
		s.Logger.Write("Start requested")
	}

	if s.Paths == nil {
		if s.Logger != nil {
			s.Logger.Write("Cannot start: server paths are not configured")
		}
		return
	}

	if s.Running {
		if s.Logger != nil {
			s.Logger.Write("Start skipped: process is already running")
		}
		return
	}

	if s.Proc != nil {
		if s.Proc.ProcessState == nil {
			if s.Logger != nil {
				s.Logger.Write("Start skipped: process is already running")
			}
			return
		}
		// Previous process already finished, clear the handle so we can start fresh.
		s.Proc = nil
	}

	// Clear previous error state on a new start attempt
	s.LastError = ""
	s.LastErrorAt = nil

	// Stubbed save purge hook: if core parameters changed previously, we would purge saves here.
	// For now, just log intent and proceed without deleting anything.
	if s.PendingSavePurge && s.Logger != nil {
		s.Logger.Write("Pending save purge flagged due to core parameter change (stub: no deletion performed)")
	}

	var executableName string
	if runtime.GOOS == "windows" {
		executableName = "rocketstation_DedicatedServer.exe"
	} else {
		executableName = "rocketstation_DedicatedServer.x86_64"
	}
	executablePath := filepath.Join(s.Paths.ServerGameDir(s.ID), executableName)
	if s.Logger != nil {
		s.Logger.Write(fmt.Sprintf("Resolved executable path: %s", executablePath))
	}

	if s.AutoUpdate || !fileExists(executablePath) {
		if s.Logger != nil {
			s.Logger.Write("AutoUpdate triggered deploy before start")
		}
		if err := s.Deploy(); err != nil && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Deploy before start failed: %v", err))
		}
	}

	worldIdentifier := strings.TrimSpace(s.WorldID)
	if worldIdentifier == "" {
		worldIdentifier = s.World
	}

	s.Logger.Write(fmt.Sprintf("World: %s  -  WorldId: %s", s.World, s.WorldID))

	args := []string{
		"-FILE", "start", s.Name, worldIdentifier, s.Difficulty, s.StartCondition, s.StartLocation,
		"-logFile", s.Paths.ServerOutputFile(s.ID),
		"-SETTINGSPATH", s.Paths.ServerSettingsFile(s.ID),
		"-SETTINGS",
		"ServerVisible", strconv.FormatBool(s.Visible),
		"GamePort", strconv.Itoa(s.Port),
		"ServerName", s.Name,
		"ServerPassword", s.Password,
		"ServerAuthSecret", s.AuthSecret,
		"ServerMaxPlayers", strconv.Itoa(s.MaxClients),
		"AutoSave", strconv.FormatBool(s.AutoSave),
		"SaveInterval", strconv.Itoa(s.SaveInterval),
		"SavePath", s.Paths.ServerDir(s.ID),
		"AutoPauseServer", strconv.FormatBool(s.AutoPause),
		"StartLocalHost", "true",
		"LocalIpAddress", "0.0.0.0",
		// Extended settings
		"MaxAutoSaves", strconv.Itoa(max(1, s.MaxAutoSaves)),
		"MaxQuickSaves", strconv.Itoa(max(1, s.MaxQuickSaves)),
		"DeleteSkeletonOnDecay", strconv.FormatBool(s.DeleteSkeletonOnDecay),
		"UseSteamP2P", strconv.FormatBool(s.UseSteamP2P),
		"DisconnectTimeout", strconv.Itoa(func(v int) int {
			if v <= 0 {
				return 10000
			}
			return v
		}(s.DisconnectTimeout)),
	}
	s.Logger.Write(fmt.Sprintf("Starting server %d with command line: %v %v", s.ID, executablePath, args))

	var cmd *exec.Cmd
	if runtime.GOOS == "linux" {
		// On Linux, use run_bepinex.sh as wrapper
		scriptPath := filepath.Join(s.Paths.ServerGameDir(s.ID), "run_bepinex.sh")
		// Warn if BepInEx directory exists but wrapper is missing
		if _, err := os.Stat(scriptPath); err != nil {
			bepDir := filepath.Join(s.Paths.ServerGameDir(s.ID), "BepInEx")
			if _, berr := os.Stat(bepDir); berr == nil && s.Logger != nil {
				s.Logger.Write("Warning: BepInEx directory present but run_bepinex.sh wrapper missing; plugins may not load")
			}
		}
		// Make the script executable
		if err := os.Chmod(scriptPath, 0o755); err != nil && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Warning: failed to make run_bepinex.sh executable: %v", err))
		}
		// run_bepinex.sh expects: ./run_bepinex.sh <executable> [args...]
		scriptArgs := append([]string{executablePath}, args...)
		cmd = exec.Command(scriptPath, scriptArgs...)
		if s.Logger != nil {
			s.Logger.Write("Using run_bepinex.sh wrapper for BepInEx support")
		}
	} else if runtime.GOOS == "windows" {
		// On Windows, run executable directly
		cmd = exec.Command(executablePath, args...)
	} else {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Unsupported platform: %s. Server start aborted.", runtime.GOOS))
		}
		return
	}
	cmd.Dir = s.Paths.ServerGameDir(s.ID)

	// Optionally detach process group (Unix uses Setpgid; Windows uses CREATE_NEW_PROCESS_GROUP).
	if s.Detached {
		if s.Logger != nil {
			s.Logger.Write("Applying detached process group for server process")
		}
		setDetachedProcessGroup(cmd)
	}

	if s.Logger != nil {
		s.Logger.Write("Starting server process")
	}
	// stdin fallback removed: Stationeers does not accept stdin commands reliably
	if err := cmd.Start(); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to start server process: %v", err))
		}
		return
	}
	if s.Logger != nil {
		s.Logger.Write("Server process started successfully")
	}

	s.Proc = cmd
	s.resetChat()
	stopChan := make(chan bool)
	s.Thrd = stopChan
	s.Starting = true
	s.Running = true

	var tailWG sync.WaitGroup
	tailWG.Add(1)
	go func(stop <-chan bool) {
		defer tailWG.Done()
		s.tailServerLog(s.Paths.ServerOutputFile(s.ID), stop)
	}(stopChan)

	go func(stop chan bool) {
		if err := cmd.Wait(); err != nil && s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Server process exited with error: %v", err))
		}
		s.Running = false
		s.Starting = false
		s.Proc = nil
		// stdin fallback removed
		close(stop)
		if s.Thrd == stop {
			s.Thrd = nil
		}
		tailWG.Wait()
		s.markAllClientsDisconnected(time.Now())
		s.rewritePlayersLog()
		s.resetChat()
		if s.Logger != nil {
			s.Logger.Write("Server process ended")
		}
	}(stopChan)
	now := time.Now()
	s.ServerStarted = &now
}

func (s *Server) tailServerLog(path string, stop <-chan bool) {
	const pollInterval = 250 * time.Millisecond

	waitForStop := func(d time.Duration) bool {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-stop:
			return true
		case <-timer.C:
			return false
		}
	}

	var file *os.File
	var reader *bufio.Reader
	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	for {
		if file == nil {
			var err error
			file, err = os.Open(path)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) && s.Logger != nil {
					s.Logger.Write(fmt.Sprintf("Failed to open server log: %v", err))
				}
				if waitForStop(pollInterval) {
					return
				}
				continue
			}
			// Start reading from the beginning to catch early startup errors written before the tailer attached.
			reader = bufio.NewReader(file)
		}

		select {
		case <-stop:
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if len(line) > 0 {
				s.processLine(line)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				if currOffset, offsetErr := file.Seek(0, io.SeekCurrent); offsetErr == nil {
					if info, statErr := os.Stat(path); statErr == nil && info.Size() < currOffset {
						file.Close()
						file = nil
						reader = nil
						continue
					}
				}
				if waitForStop(pollInterval) {
					return
				}
				continue
			}

			if s.Logger != nil {
				s.Logger.Write(fmt.Sprintf("Error reading server log: %v", err))
			}
			file.Close()
			file = nil
			reader = nil
			if waitForStop(pollInterval) {
				return
			}
		}
	}
}

func (s *Server) processLine(line string) {
	s.LastLogLine = line

	// Allow multiple handlers to react to a single line. This avoids
	// early-greedy matches (e.g., a loose chat pattern) from masking
	// more specific detectors like fatal startup errors.
	for _, handler := range logLineHandlers {
		if matches := handler.match(line); matches != nil {
			handler.handle(s, line, matches)
		}
	}
}

// detectSCONPortFromLog attempts to parse the SCON HTTP port from the BepInEx LogOutput.log
// within the server's game directory. Returns 0 if not found.
func (s *Server) detectSCONPortFromLog() int {
	if s == nil || s.Paths == nil {
		return 0
	}
	logPath := filepath.Join(s.Paths.ServerGameDir(s.ID), "BepInEx", "LogOutput.log")
	f, err := os.Open(logPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	// Read up to last 128KB for efficiency on large logs
	const maxRead = 128 * 1024
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	size := info.Size()
	var start int64 = 0
	if size > maxRead {
		start = size - maxRead
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		// Best effort; if ReadAt fails, fallback to 0
		return 0
	}
	text := string(buf)
	// Check common SCON log patterns in reverse priority of specificity
	// 1) HTTP Listener started on http://localhost:27017/
	if i := strings.LastIndex(text, "HTTP Listener started on http://"); i >= 0 {
		// Extract substring after this marker and parse port
		sub := text[i:]
		if p := extractPortFromURLLike(sub); p > 0 {
			return p
		}
	}
	// 2) SCON server started on localhost:27017
	if i := strings.LastIndex(text, "SCON server started on "); i >= 0 {
		sub := text[i:]
		if p := extractPortFromHostPort(sub); p > 0 {
			return p
		}
	}
	// 3) Auto-binding SCON to dedicated server port+1: 27017
	if i := strings.LastIndex(text, "port+1:"); i >= 0 {
		sub := text[i:]
		if p := extractFirstPortDigits(sub); p > 0 {
			return p
		}
	}
	return 0
}

// extractPortFromURLLike parses a URL-like segment and returns the trailing port number.
func extractPortFromURLLike(s string) int {
	// Find last ':' and parse consecutive digits
	idx := strings.LastIndex(s, ":")
	if idx < 0 || idx+1 >= len(s) {
		return 0
	}
	// Read digits until non-digit
	j := idx + 1
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == idx+1 {
		return 0
	}
	n, _ := strconv.Atoi(s[idx+1 : j])
	if n >= 1 && n <= 65535 {
		return n
	}
	return 0
}

// extractPortFromHostPort finds patterns like "localhost:27017" or "127.0.0.1:27017".
func extractPortFromHostPort(s string) int { return extractPortFromURLLike(s) }

// extractFirstPortDigits returns the first 2-5 digit number in the string within port range.
func extractFirstPortDigits(s string) int {
	// Simple scan for a 2-5 digit sequence, then validate range
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' && (j-i) < 5 {
			j++
		}
		if j-i >= 2 {
			if n, _ := strconv.Atoi(s[i:j]); n >= 1 && n <= 65535 {
				return n
			}
		}
		i = j
	}
	return 0
}

// CurrentSCONPort returns the detected SCON HTTP port, preferring BepInEx logs when available.
// Falls back to the conventional game port + 1, or 8081 if unknown.
func (s *Server) CurrentSCONPort() int {
	if s == nil {
		return 8081
	}
	if p := s.detectSCONPortFromLog(); p > 0 {
		// Keep a cached copy for visibility
		s.SCONPort = p
		return p
	}
	if s.Port > 0 {
		return s.Port + 1
	}
	if s.SCONPort > 0 {
		return s.SCONPort
	}
	return 8081
}

// SendRaw sends a single console command via SCON HTTP API.
// Returns an error if the server is not running or SCON API is unavailable.
func (s *Server) SendRaw(line string) error {
	if s == nil {
		return fmt.Errorf("server unavailable")
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		if s.Logger != nil {
			s.Logger.Write("Command send aborted: empty command")
		}
		return fmt.Errorf("empty command")
	}
	if !s.IsRunning() || s.Proc == nil {
		if s.Logger != nil {
			s.Logger.Write("Command send failed: server is not running")
		}
		return fmt.Errorf("server is not running")
	}

	// Use SCON HTTP API only; stdin fallback is not supported
	// Prefer dynamic detection from BepInEx logs; fallback to game port + 1
	rconPort := s.CurrentSCONPort()

	// Ensure single line only
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	s.Logger.Write(fmt.Sprintf("Sending: %s", trimmed))

	// Create HTTP request to SCON API
	type CommandRequest struct {
		Command string `json:"command"`
	}
	reqBody, err := json.Marshal(CommandRequest{Command: trimmed})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Command marshal failed: %v", err))
		}
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/command", rconPort)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("SCON HTTP POST failed: %v", err))
		}
		return fmt.Errorf("failed to send command to SCON API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Log truncated body to avoid huge logs
		bodyStr := string(body)
		if len(bodyStr) > 1024 {
			bodyStr = bodyStr[:1024] + "…"
		}
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("SCON API error %d: %s", resp.StatusCode, bodyStr))
		}
		return fmt.Errorf("SCON API returned status %d: %s", resp.StatusCode, bodyStr)
	}

	return nil
}

// SendCommand provides a small layer over SendRaw for different command kinds.
// kind may be "console" (default) or "chat". For chat, we prefix with "say "
// which is commonly used by dedicated servers to broadcast messages.
func (s *Server) SendCommand(kind, payload string) error {
	k := strings.ToLower(strings.TrimSpace(kind))
	msg := strings.TrimSpace(payload)
	if msg == "" {
		return fmt.Errorf("no payload provided")
	}
	switch k {
	case "", "console":
		return s.SendRaw(msg)
	case "chat":
		// Stationeers console command is 'SAY' (case-sensitive in some contexts)
		// Use uppercase and pass the message directly.
		expanded := s.expandChatTokens(msg)
		return s.SendRaw("SAY " + expanded)
	default:
		// Unknown type, treat as console
		return s.SendRaw(msg)
	}
}

func (s *Server) parseTime(line string) time.Time {
	tStr := strings.Split(line, " ")[0]
	tStr = strings.TrimSuffix(tStr, ":")
	parsed, _ := time.Parse("15:04:05", tStr)
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, now.Location())
}

func (s *Server) copyDir(src, dst string, tracker *copyTracker) error {
	if err := os.MkdirAll(dst, os.ModePerm); err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to create directory %s: %v", dst, err))
		}
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Write(fmt.Sprintf("Failed to read directory %s: %v", src, err))
		}
		return err
	}

	var errs []error
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := s.copyDir(srcPath, dstPath, tracker); err != nil {
				errs = append(errs, err)
			}
			continue
		}

		copyNeeded, diffErr := filesDiffer(srcPath, dstPath)
		if diffErr != nil {
			if s.Logger != nil {
				s.Logger.Write(fmt.Sprintf("Failed to compare %s and %s: %v", srcPath, dstPath, diffErr))
			}
			copyNeeded = true
		}
		if copyNeeded {
			if err := copyFile(srcPath, dstPath); err != nil {
				if s.Logger != nil {
					s.Logger.Write(fmt.Sprintf("Failed to copy %s to %s: %v", srcPath, dstPath, err))
				}
				errs = append(errs, err)
			}
		}
		if tracker != nil {
			tracker.increment("Copying files")
		}
	}

	return errors.Join(errs...)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	mode := info.Mode()
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Normalize permissions on Unix-like systems: preserve exec bit if present; otherwise 0644
	if runtime.GOOS != "windows" {
		finalMode := mode
		if mode.IsRegular() {
			if (mode&0o111) != 0 || strings.HasSuffix(strings.ToLower(dst), ".sh") {
				finalMode = 0o755
			} else {
				finalMode = 0o644
			}
		}
		return os.Chmod(dst, finalMode)
	}
	return os.Chmod(dst, mode)
}

func filesDiffer(src, dst string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return true, err
	}
	dstInfo, err := os.Stat(dst)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if srcInfo.Size() != dstInfo.Size() {
		return true, nil
	}

	srcHash, err := fileHash(src)
	if err != nil {
		return true, err
	}
	dstHash, err := fileHash(dst)
	if err != nil {
		return true, err
	}

	return !bytes.Equal(srcHash, dstHash), nil
}

type copyTracker struct {
	server    *Server
	total     int64
	processed int64
}

func newCopyTracker(server *Server, total int64) *copyTracker {
	return &copyTracker{
		server: server,
		total:  total,
	}
}

func (ct *copyTracker) increment(stage string) {
	if ct == nil || ct.server == nil {
		return
	}
	ct.processed++
	ct.server.reportProgress(stage, ct.processed, ct.total)
}

func countFiles(root string) (int64, error) {
	var count int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			count++
		}
		return nil
	})
	return count, err
}

func fileHash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
