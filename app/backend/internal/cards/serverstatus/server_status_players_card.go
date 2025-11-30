package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/models"
)

const serverStatusPlayersTemplate = "cards/server_status_players.html"

type serverStatusPlayersCard struct{}

func init() {
	cards.Register(serverStatusPlayersCard{})
}

func (serverStatusPlayersCard) ID() string {
	return "server-status-players"
}

func (serverStatusPlayersCard) Template() string {
	return serverStatusPlayersTemplate
}

func (serverStatusPlayersCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusPlayersCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusPlayersCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}

	data := gin.H{
		"server": req.Server,
	}

	if req.Payload != nil {
		if role, ok := req.Payload["role"].(string); ok {
			data["role"] = role
		}
	}

	if ds := req.Datasets; ds != nil {
		if roster := ds.PlayerRoster(); roster != nil {
			data["liveClients"] = roster.LiveClients
			data["historyClients"] = roster.HistoryClients
			data["banned"] = roster.BannedEntries
			data["banned_ids"] = roster.BannedIDs
		}
	}

	if req.Payload != nil {
		if _, ok := data["liveClients"]; !ok {
			if val, exists := req.Payload["liveClients"]; exists {
				data["liveClients"] = val
			}
		}
		if _, ok := data["historyClients"]; !ok {
			if val, exists := req.Payload["historyClients"]; exists {
				data["historyClients"] = val
			}
		}
		if _, ok := data["banned"]; !ok {
			if val, exists := req.Payload["banned"]; exists {
				data["banned"] = val
			}
		}
		if _, ok := data["banned_ids"]; !ok {
			if val, exists := req.Payload["banned_ids"]; exists {
				switch typed := val.(type) {
				case []string:
					data["banned_ids"] = typed
				case []interface{}:
					extracted := make([]string, 0, len(typed))
					for _, entry := range typed {
						if str, ok := entry.(string); ok {
							extracted = append(extracted, str)
						}
					}
					data["banned_ids"] = extracted
				}
			}
		}
	}

	if _, ok := data["liveClients"]; !ok {
		data["liveClients"] = []*models.Client{}
	}
	if _, ok := data["historyClients"]; !ok {
		data["historyClients"] = []*models.Client{}
	}
	if _, ok := data["banned"]; !ok {
		data["banned"] = []models.BannedEntry{}
	}
	if _, ok := data["banned_ids"]; !ok {
		data["banned_ids"] = []string{}
	}

	return data, nil
}
