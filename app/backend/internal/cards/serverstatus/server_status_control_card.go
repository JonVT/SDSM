package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
)

const serverStatusControlTemplate = "cards/server_status_control.html"

type serverStatusControlCard struct{}

func init() {
	cards.Register(serverStatusControlCard{})
}

func (serverStatusControlCard) ID() string {
	return "server-status-control"
}

func (serverStatusControlCard) Template() string {
	return serverStatusControlTemplate
}

func (serverStatusControlCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusControlCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusControlCard) Capabilities() cards.CardCapabilities {
	return cards.CardCapabilities{
		AllowedRoles: []string{
			string(manager.RoleAdmin),
			string(manager.RoleOperator),
		},
	}
}

func (serverStatusControlCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}
	return gin.H{
		"server": req.Server,
	}, nil
}
