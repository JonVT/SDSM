package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
)

const serverStatusDiscordTemplate = "cards/server_status_discord.html"

type serverStatusDiscordCard struct{}

func init() {
	cards.Register(serverStatusDiscordCard{})
}

func (serverStatusDiscordCard) ID() string {
	return "server-status-discord"
}

func (serverStatusDiscordCard) Template() string {
	return serverStatusDiscordTemplate
}

func (serverStatusDiscordCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusDiscordCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusDiscordCard) Capabilities() cards.CardCapabilities {
	return cards.CardCapabilities{
		AllowedRoles: []string{string(manager.RoleAdmin)},
	}
}

func (serverStatusDiscordCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}

	data := gin.H{
		"server": req.Server,
	}

	if req.Payload != nil {
		for _, key := range []string{"role"} {
			if val, ok := req.Payload[key]; ok {
				data[key] = val
			}
		}
	}

	return data, nil
}
