package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
)

const serverStatusSavesTemplate = "cards/server_status_saves.html"

type serverStatusSavesCard struct{}

func init() {
	cards.Register(serverStatusSavesCard{})
}

func (serverStatusSavesCard) ID() string {
	return "server-status-saves"
}

func (serverStatusSavesCard) Template() string {
	return serverStatusSavesTemplate
}

func (serverStatusSavesCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusSavesCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusSavesCard) Capabilities() cards.CardCapabilities {
	return cards.CardCapabilities{
		AllowedRoles: []string{
			string(manager.RoleAdmin),
			string(manager.RoleOperator),
		},
	}
}

func (serverStatusSavesCard) FetchData(req *cards.Request) (gin.H, error) {
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
	return data, nil
}
