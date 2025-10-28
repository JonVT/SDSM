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

type Client struct {
	SteamID            string     `json:"steam_id"`
	Name               string     `json:"name"`
	ConnectDatetime    time.Time  `json:"connect_datetime"`
	DisconnectDatetime *time.Time `json:"disconnect_datetime,omitempty"`
	IsAdmin            bool       `json:"is_admin"`
}

func (c *Client) IsOnline() bool {
	return c != nil && c.DisconnectDatetime == nil
}

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

type Chat struct {
	Datetime time.Time `json:"datetime"`
	Name     string    `json:"name"`
	Message  string    `json:"message"`
}

type ServerConfig struct {
	Name                string
	World               string
	WorldID             string
	StartLocation       string
	StartCondition      string
	Difficulty          string
	Port                int
	Password            string
	AuthSecret          string
	MaxClients          int
	SaveInterval        int
	Visible             bool
	Beta                bool
	AutoStart           bool
	AutoUpdate          bool
	AutoSave            bool
	AutoPause           bool
	Mods                []string
	RestartDelaySeconds int
}

type Server struct {
	ID                  int           `json:"id"`
	Proc                *exec.Cmd     `json:"-"`
	Thrd                chan bool     `json:"-"`
	Logger              *utils.Logger `json:"-"`
	Paths               *utils.Paths  `json:"-"`
	Name                string        `json:"name"`
	World               string        `json:"world"`
	WorldID             string        `json:"world_id"`
	StartLocation       string        `json:"start_location"`
	StartCondition      string        `json:"start_condition"`
	Difficulty          string        `json:"difficulty"`
	Port                int           `json:"port"`
	SaveInterval        int           `json:"save_interval"`
	AuthSecret          string        `json:"auth_secret"`
	Password            string        `json:"password"`
	MaxClients          int           `json:"max_clients"`
	Visible             bool          `json:"visible"`
	Beta                bool          `json:"beta"`
	AutoStart           bool          `json:"auto_start"`
	AutoUpdate          bool          `json:"auto_update"`
	AutoSave            bool          `json:"auto_save"`
	AutoPause           bool          `json:"auto_pause"`
	Mods                []string      `json:"mods"`
	RestartDelaySeconds int           `json:"restart_delay_seconds"`
	ServerStarted       *time.Time    `json:"server_started,omitempty"`
	ServerSaved         *time.Time    `json:"server_saved,omitempty"`
	Clients             []*Client     `json:"-"`
	Chat                []*Chat       `json:"-"`
	Storming            bool          `json:"-"`
	Paused              bool          `json:"-"`
	Starting            bool          `json:"-"`
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
	// stdin is a handle to the running server process' standard input when available.
	// It is used to send console commands. Guard all writes with stdinMu.
	stdin   io.WriteCloser `json:"-"`
	stdinMu sync.Mutex     `json:"-"`
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
func (s *Server) ReadBlacklistIDs() []string {
	path := s.blacklistPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
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
func (s *Server) BannedEntries() []BannedEntry {
	ids := s.ReadBlacklistIDs()
	if len(ids) == 0 {
		return nil
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

func (s *Server) LiveClients() []*Client {
	if len(s.Clients) == 0 {
		return nil
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

	if cfg.RestartDelaySeconds >= 0 {
		s.RestartDelaySeconds = cfg.RestartDelaySeconds
	} else {
		s.RestartDelaySeconds = DefaultRestartDelaySeconds
	}

	s.EnsureLogger(paths)

	return s
}

func NewServer(serverID int, paths *utils.Paths, data string) *Server {
	s := &Server{
		ID:                  serverID,
		Paths:               paths,
		Clients:             []*Client{},
		Chat:                []*Chat{},
		Mods:                []string{},
		Paused:              false,
		Running:             false,
		RestartDelaySeconds: DefaultRestartDelaySeconds,
	}

	s.EnsureLogger(paths)

	if data != "" {
		json.Unmarshal([]byte(data), s)
		if strings.TrimSpace(s.WorldID) == "" {
			s.WorldID = s.World
		}
		if !strings.Contains(data, "restart_delay_seconds") || s.RestartDelaySeconds < 0 {
			s.RestartDelaySeconds = DefaultRestartDelaySeconds
		}
	} else {
		s.Name = fmt.Sprintf("Stationeers Server %d", serverID)
		s.World = "Mars2"
		s.WorldID = s.World
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

	s.Logger.Write("Deploying server files...")
	totalFiles, err := countFiles(src)
	if err != nil {
		s.Logger.Write(fmt.Sprintf("Failed to enumerate source files: %v", err))
		return err
	}

	additionalFiles := s.countFilesIfExists(s.Paths.BepInExDir()) + s.countFilesIfExists(s.Paths.LaunchPadDir())
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
		s.Logger.Write("Copying LaunchPad files into BepInEx/plugins")
	}
	if err := s.copyDir(launchDir, pluginsDst, tracker); err != nil {
		return fmt.Errorf("copying LaunchPad content: %w", err)
	}

	return nil
}

func (s *Server) ToJSON() string {
	data, _ := json.Marshal(s)
	return string(data)
}

func (s *Server) Stop() {
	s.Running = false
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

func (s *Server) markAllClientsDisconnected(t time.Time) {
	for _, client := range s.Clients {
		if client != nil && client.DisconnectDatetime == nil {
			disconnected := t
			client.DisconnectDatetime = &disconnected
		}
	}
}

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
		"-file",
		"start",
		s.Name,
		worldIdentifier,
		s.Difficulty,
		s.StartCondition,
		s.StartLocation,
		"-logFile", s.Paths.ServerOutputFile(s.ID),
		"-settings",
		"ServerVisible", strconv.FormatBool(s.Visible),
		"GamePort", strconv.Itoa(s.Port),
		"ServerName", s.Name,
		"ServerPassword", s.Password,
		"ServerAuthSecret", s.AuthSecret,
		"ServerMaxPlayers", strconv.Itoa(s.MaxClients),
		"AutoSave", strconv.FormatBool(s.AutoSave),
		"SaveInterval", strconv.Itoa(s.SaveInterval),
		"AutoPauseServer", strconv.FormatBool(s.AutoPause),
		"StartLocalHost", "true",
		"LocalIpAddress", "0.0.0.0",
	}
	s.Logger.Write(fmt.Sprintf("Starting server %d with command line: %v %v", s.ID, executablePath, args))

	cmd := exec.Command(executablePath, args...)
	cmd.Dir = s.Paths.ServerGameDir(s.ID)

	if s.Logger != nil {
		s.Logger.Write("Starting server process")
	}
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
	// Attempt to capture stdin for sending console commands
	if stdin, err := cmd.StdinPipe(); err == nil {
		s.stdinMu.Lock()
		s.stdin = stdin
		s.stdinMu.Unlock()
	} else if s.Logger != nil {
		s.Logger.Write(fmt.Sprintf("Failed to acquire stdin for server process: %v", err))
	}
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
		// Close stdin if present and clear it
		s.stdinMu.Lock()
		if s.stdin != nil {
			_ = s.stdin.Close()
			s.stdin = nil
		}
		s.stdinMu.Unlock()
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

// SendRaw writes a single console line to the running server's stdin.
// It returns an error if the server is not running or stdin is unavailable.
func (s *Server) SendRaw(line string) error {
	if s == nil {
		return fmt.Errorf("server unavailable")
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}
	if !s.IsRunning() || s.Proc == nil {
		return fmt.Errorf("server is not running")
	}
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.stdin == nil {
		return fmt.Errorf("stdin not available")
	}
	// Ensure single line only
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	if _, err := io.WriteString(s.stdin, trimmed+"\n"); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
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
		// Prefix with 'say' which many servers use; adjust if Stationeers expects a different verb
		return s.SendRaw("say " + msg)
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

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return os.Chmod(dst, info.Mode())
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
