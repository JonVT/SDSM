package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type logLineHandler struct {
	match  func(string) []string
	handle func(*Server, string, []string)
}

var (
	clientReadyRegex      = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+Client\s+(.+?)\s+\(([^)]+)\)\s+is ready`)
	clientDisconnectRegex = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+Client\s+disconnected:\s*(\S+)\s+\|\s+(.+?)\s+connectTime:.*ClientId:\s*(\d+)`)
	difficultyRegex       = regexp.MustCompile(`Set difficulty to\s+(\S+)`)
	worldLoadedRegex      = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+loaded\s+(\S+)\s+things in`)
	adminCommandRegex     = regexp.MustCompile(`(?i)client\s+'(.+?)\s+\(([^)]+)\)'\s+ran\s+command`)
	chatMessageRegex      = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:\s+([^:]+?):\s*(.+)$`)
	// CLIENTS response parsing
	clientsHeaderRegex    = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+CLIENTS\b`)
	clientsCountRegex     = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+Clients:\s*(\d+)`)
	clientEntryRegex      = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+(\S+)\s*\|\s*(.+?)\s+\t?connectTime:.*?ClientId:\s*(\d+)`)
	hostClientRegex       = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}:?\s+Host\s+Client:`)
	// Example line:
	//   file: [No such world name: Europa. Valid worlds: Europa3, Lunar, Mars2, ...]
	// Be lenient about casing and trailing bracket.
	noSuchWorldRegex = regexp.MustCompile(`(?i)no\s+such\s+world\s+name:\s*['"]?([^\]'"\.]+)['"]?\.?\s+valid\s+worlds:\s*(.*)`)
	// Example lines (variants observed):
	//   file: [No such world name: 'Europa3'. Valid worlds: Europa3, Lunar, Mars2, ...
	// Be lenient about quotes, trailing bracket, and any trailing characters on the line.

	logLineHandlers = []logLineHandler{
		// Begin CLIENTS scan block
		{
			match: func(line string) []string { return clientsHeaderRegex.FindStringSubmatch(line) },
			handle: func(s *Server, line string, _ []string) {
				if s != nil { s.beginClientsScan() }
			},
		},
		// Clients count line (optional, mostly informational)
		{
			match: func(line string) []string { return clientsCountRegex.FindStringSubmatch(line) },
			handle: func(_ *Server, _ string, _ []string) { /* no-op */ },
		},
		// Individual client entry lines
		{
			match: func(line string) []string { return clientEntryRegex.FindStringSubmatch(line) },
			handle: func(s *Server, line string, m []string) {
				if s == nil || len(m) < 4 { return }
				t := s.parseTime(line)
				first := strings.TrimSpace(m[1]) // sometimes an internal id
				name := strings.TrimSpace(m[2])
				clientID := strings.TrimSpace(m[3]) // appears to be steam id when non-zero
				steam := clientID
				if steam == "0" || steam == "" {
					// fallback to first field if it looks like a steam64
					if len(first) >= 16 {
						steam = first
					} else {
						steam = ""
					}
				}
				s.noteClientsScan(steam, name, t)
			},
		},
		// End of CLIENTS scan when Host Client line appears
		{
			match: func(line string) []string { return hostClientRegex.FindStringSubmatch(line) },
			handle: func(s *Server, _ string, _ []string) { if s != nil { s.endClientsScan() } },
		},
		{
			match: func(line string) []string {
				if strings.Contains(line, "World Saved") || strings.Contains(line, "Saving - file created") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, line string, _ []string) {
				t := s.parseTime(line)
				s.ServerSaved = &t
			},
		},
		// Recognize completion line like: "17:13:57: Saved <ServerName>"
		{
			match: func(line string) []string {
				if strings.Contains(line, ": Saved ") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, line string, _ []string) {
				t := s.parseTime(line)
				s.ServerSaved = &t
				// Try to move the next pending manual save into playersave
				s.tryMoveNextPendingPlayerSave()
			},
		},
		{
			match: func(line string) []string {
				return clientReadyRegex.FindStringSubmatch(line)
			},
			handle: func(s *Server, line string, matches []string) {
				if len(matches) < 3 {
					return
				}
				t := s.parseTime(line)
				name := strings.TrimSpace(matches[1])
				steamID := strings.TrimSpace(matches[2])
				// Ignore duplicate "is ready" events for a player already marked online.
				// This prevents re-appending sessions or re-triggering welcome/save logic on spurious repeats.
				for _, live := range s.LiveClients() {
					if live == nil { continue }
					// Match by SteamID when available; fallback to case-insensitive name match.
					if steamID != "" && strings.EqualFold(live.SteamID, steamID) {
						return
					}
					if steamID == "" && name != "" && strings.EqualFold(live.Name, name) {
						return
					}
				}
				client := &Client{
					SteamID:         steamID,
					Name:            name,
					ConnectDatetime: t,
				}
				if s.recordClientSession(client) {
					s.appendPlayerLog(client)
				}

				// Player Saves automation: on player connect, if enabled and not excluded,
				// issue a FILE saveas <ddmmyy_hhmmss_steamid> (the game appends .save in manualsave),
				// then move the resulting file to playersave when we see the 'Saved' log line.
				if s != nil && s.PlayerSaves {
					// Skip if SteamID is excluded
					if steamID != "" && !s.HasPlayerSaveExclude(steamID) {
						// Build base name ddmmyy_hhmmss_steamid using the connect timestamp
						yy := t.Year() % 100
						mm := int(t.Month())
						stamp := fmt.Sprintf("%02d%02d%02d_%02d%02d%02d", t.Day(), mm, yy, t.Hour(), t.Minute(), t.Second())
						baseName := fmt.Sprintf("%s_%s", stamp, steamID)
						// The created file on disk will be baseName.save
						fileName := baseName + ".save"
						// Rate-limit per steamID and avoid duplicate queueing
						if s.shouldEnqueuePlayerSave(steamID, t) {
							// Queue the filename (with .save) to be moved after save completes
							s.queuePendingPlayerSave(fileName)
							// Only attempt when server is running. Pass base name without extension;
							// the game will append .save automatically. This avoids *.save.save.
							if s.IsRunning() {
								_ = s.SendCommand("console", "FILE saveas "+baseName)
							}
						}
					}
				}

				// Welcome / Welcome Back messages:
				// Determine if this is a first-time player (no prior session with same SteamID in history).
				if s != nil && s.IsRunning() {
					steam := strings.TrimSpace(steamID)
					isReturning := false
					if steam != "" {
						for _, existing := range s.Clients {
							if existing == nil { continue }
							if strings.EqualFold(existing.SteamID, steam) && existing.ConnectDatetime.Before(t) {
								isReturning = true
								break
							}
						}
					}
					// Choose appropriate message
					msg := ""
					if isReturning {
						msg = strings.TrimSpace(s.WelcomeBackMessage)
					} else {
						msg = strings.TrimSpace(s.WelcomeMessage)
					}
					if msg != "" {
						ctx := map[string]string{"player": name}
						delay := s.WelcomeDelaySeconds
						if delay < 0 { delay = 0 }
						go func(srv *Server, text string, c map[string]string, d int) {
							time.Sleep(time.Duration(d) * time.Second)
							_ = srv.SendCommand("chat", srv.RenderChatMessage(text, c))
						}(s, msg, ctx, delay)
					}
				}
			},
		},
		{
			match: func(line string) []string {
				return clientDisconnectRegex.FindStringSubmatch(line)
			},
			handle: func(s *Server, line string, matches []string) {
				if len(matches) < 4 {
					return
				}
				steamID := strings.TrimSpace(matches[3])
				name := strings.TrimSpace(matches[2])
				t := s.parseTime(line)
				for _, client := range s.Clients {
					if client.DisconnectDatetime != nil {
						continue
					}
					if client.SteamID == steamID || strings.EqualFold(client.Name, name) {
						client.DisconnectDatetime = &t
						s.rewritePlayersLog()
						break
					}
				}
			},
		},
		{
			match: func(line string) []string {
				return difficultyRegex.FindStringSubmatch(line)
			},
			handle: func(s *Server, _ string, matches []string) {
				if len(matches) < 2 {
					return
				}
				s.Difficulty = strings.TrimSpace(matches[1])
			},
		},
		{
			match: func(line string) []string {
				if m := worldLoadedRegex.FindStringSubmatch(line); m != nil {
					return m
				}
				if strings.Contains(line, "loaded") && strings.Contains(line, "things in") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, line string, matches []string) {
				extract := func(raw string) string {
					raw = strings.TrimSpace(raw)
					// Some logs wrap world IDs in quotes, e.g., 'Europa3'. Strip surrounding quotes/brackets and stray punctuation.
					raw = strings.Trim(raw, "'\"")
					raw = strings.Trim(raw, "[]()")
					raw = strings.TrimRight(raw, ".,;]")
					return raw
				}
				worldID := ""
				if len(matches) > 1 {
					worldID = extract(matches[1])
				}
				if worldID == "" {
					fields := strings.Fields(line)
					if len(fields) > 2 {
						worldID = extract(fields[2])
					}
				}
				if worldID == "" {
					return
				}
				s.WorldID = worldID
				if strings.TrimSpace(s.World) == "" {
					s.World = worldID
				}
			},
		},
		{
			match: func(line string) []string {
				return adminCommandRegex.FindStringSubmatch(line)
			},
			handle: func(s *Server, _ string, matches []string) {
				if len(matches) < 3 {
					return
				}
				name := strings.TrimSpace(matches[1])
				steamID := strings.TrimSpace(matches[2])
				s.markClientAdmin(name, steamID)
			},
		},
		{
			match: func(line string) []string {
				return chatMessageRegex.FindStringSubmatch(line)
			},
			handle: func(s *Server, line string, matches []string) {
				if len(matches) < 3 {
					return
				}
				name := strings.TrimSpace(matches[1])
				message := strings.TrimSpace(matches[2])
				if name == "" || message == "" {
					return
				}
				t := s.parseTime(line)

				// Always accept chat messages from "Server" without checking online clients
				if strings.EqualFold(name, "Server") {
					s.addChatMessage(name, t, message)
					return
				}

				for _, client := range s.LiveClients() {
					if client == nil {
						continue
					}
					if strings.EqualFold(client.Name, name) {
						s.addChatMessage(client.Name, t, message)
						return
					}
				}
			},
		},
		{
			match: func(line string) []string {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "server paused") || strings.Contains(lower, "game is paused") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, _ string, _ []string) {
				s.Paused = true
			},
		},
		{
			match: func(line string) []string {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "server resumed") || strings.Contains(lower, "game is resumed") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, _ string, _ []string) {
				s.Paused = false
			},
		},
		// Weather event start/stop
		{
			match: func(line string) []string {
				// Toggle storming on weather start
				if strings.Contains(strings.ToLower(line), "started weather event") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, _ string, _ []string) {
				s.Storming = true
			},
		},
		{
			match: func(line string) []string {
				// Toggle storming off on weather stop
				if strings.Contains(strings.ToLower(line), "stopped weather event") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, _ string, _ []string) {
				s.Storming = false
			},
		},
		{
			match: func(line string) []string {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "started server") || strings.Contains(lower, "rocketnet succesfully hosted") {
					return []string{}
				}
				return nil
			},
			handle: func(s *Server, _ string, _ []string) {
				s.Starting = false
				s.Running = true
			},
		},
		// Fatal startup error: invalid world name
		{
			match: func(line string) []string {
				// Quick pre-filter to avoid regex on every line
				if !strings.Contains(strings.ToLower(line), "no such world name") {
					return nil
				}
				if m := noSuchWorldRegex.FindStringSubmatch(line); m != nil {
					return m
				}
				// Fall back to a synthetic non-empty slice to trigger handler even if regex didn't match structure
				return []string{"no such world name"}
			},
			handle: func(s *Server, line string, matches []string) {
				// matches[0] = full match, [1] = bad world (optional), [2] = valid worlds (optional)
				var bad, valids string
				if len(matches) >= 2 {
					bad = strings.TrimSpace(matches[1])
				}
				if len(matches) >= 3 {
					valids = strings.TrimSpace(matches[2])
				}
				// Build a concise error message; if parsing failed, include the raw line
				msg := "Invalid world name detected"
				if bad != "" {
					msg += ": " + bad
				}
				if valids != "" {
					msg += ". Valid worlds: " + valids
				} else if bad == "" {
					msg += ": see log line — " + line
				}
				// Record and log the error
				now := time.Now()
				s.LastError = msg
				s.LastErrorAt = &now
				if s.Logger != nil {
					s.Logger.Write("Startup error: " + msg)
				}
				// Stop the server if it's starting/running
				if s.Running || s.Starting {
					s.Stop()
				}
			},
		},
	}
)
