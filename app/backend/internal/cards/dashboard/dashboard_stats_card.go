package dashboard

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardStatsTemplate = "cards/dashboard_stats.html"

type dashboardStatsCard struct{}

func init() {
	cards.Register(dashboardStatsCard{})
}

func (dashboardStatsCard) ID() string {
	return "dashboard-stats"
}

func (dashboardStatsCard) Template() string {
	return dashboardStatsTemplate
}

func (dashboardStatsCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardStatsCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardStatsCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}

	if req.Payload != nil {
		if statsCtx, ok := req.Payload["statsCtx"].(gin.H); ok {
			for k, v := range statsCtx {
				data[k] = v
			}
		} else if statsCtx, ok := req.Payload["statsCtx"].(map[string]interface{}); ok {
			for k, v := range statsCtx {
				data[k] = v
			}
		}
	}

	setIfMissing := func(key string) {
		if _, exists := data[key]; exists {
			return
		}
		if req.Payload == nil {
			return
		}
		if value, ok := req.Payload[key]; ok {
			data[key] = value
		}
	}

	setIfMissing("totalServers")
	setIfMissing("activeServers")
	setIfMissing("totalPlayers")
	setIfMissing("startableServers")

	return data, nil
}
