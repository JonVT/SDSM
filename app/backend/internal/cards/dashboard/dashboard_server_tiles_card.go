package dashboard

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardServerTilesTemplate = "cards/dashboard_server_tiles.html"

type dashboardServerTilesCard struct{}

func init() {
	cards.Register(dashboardServerTilesCard{})
}

func (dashboardServerTilesCard) ID() string {
	return "dashboard-server-tiles"
}

func (dashboardServerTilesCard) Template() string {
	return dashboardServerTilesTemplate
}

func (dashboardServerTilesCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardServerTilesCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardServerTilesCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}

	role := ""
	if req.Payload != nil {
		if r, ok := req.Payload["role"].(string); ok {
			role = r
		}
	}
	servers := extractServersFromPayload(req)
	active := 0
	startable := 0
	for _, s := range servers {
		if s == nil {
			continue
		}
		if s.IsRunning() {
			active++
		}
		if !s.IsRunning() || s.Stopping {
			startable++
		}
	}
	context := gin.H{
		"servers": servers,
		"role":    role,
	}

	data["role"] = role
	data["servers"] = servers
	data["context"] = context
	data["active"] = active
	data["startable"] = startable

	return data, nil
}
