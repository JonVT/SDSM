package models

import (
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
	// Example line:
	//   file: [No such world name: Europa. Valid worlds: Europa3, Lunar, Mars2, ...]
	// Be lenient about casing and trailing bracket.
	noSuchWorldRegex = regexp.MustCompile(`(?i)no\s+such\s+world\s+name:\s*['"]?([^\]'"\.]+)['"]?\.?\s+valid\s+worlds:\s*(.*)`)
	// Example lines (variants observed):
	//   file: [No such world name: 'Europa3'. Valid worlds: Europa3, Lunar, Mars2, ...
	// Be lenient about quotes, trailing bracket, and any trailing characters on the line.

	logLineHandlers = []logLineHandler{
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
				client := &Client{
					SteamID:         steamID,
					Name:            name,
					ConnectDatetime: t,
				}
				if s.recordClientSession(client) {
					s.appendPlayerLog(client)
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
