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
				s.Clients = append(s.Clients, client)
				s.appendPlayerLog(client)
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
				if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" {
					s.World = strings.TrimSpace(matches[1])
					return
				}
				fields := strings.Fields(line)
				if len(fields) > 2 {
					s.World = fields[2]
				}
			},
		},
		{
			match: func(line string) []string {
				if strings.Count(line, ":") < 4 {
					return nil
				}
				parts := strings.Split(line, ":")
				if len(parts) < 5 {
					return nil
				}
				return parts
			},
			handle: func(s *Server, line string, parts []string) {
				if len(parts) < 5 {
					return
				}
				name := strings.TrimSpace(parts[3])
				if name == "" {
					return
				}
				for _, client := range s.Clients {
					if client.Name == name {
						t := s.parseTime(line)
						message := strings.Join(parts[4:], ":")
						s.Chat = append(s.Chat, &Chat{
							Datetime: t,
							Name:     name,
							Message:  strings.TrimSpace(message),
						})
						break
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
