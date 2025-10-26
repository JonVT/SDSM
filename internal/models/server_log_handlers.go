package models

import (
	"regexp"
	"strings"
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
					return strings.TrimSpace(raw)
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
	}
)
