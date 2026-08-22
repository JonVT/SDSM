package dashboard

import (
	"strings"

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
	starting := 0
	paused := 0
	stopped := 0
	errors := 0
	storming := 0
	for _, s := range servers {
		if s == nil {
			continue
		}
		if s.IsRunning() {
			active++
			if s.Paused {
				paused++
			}
		} else {
			stopped++
		}
		if !s.IsRunning() || s.Stopping {
			startable++
		}
		if s.Starting {
			starting++
		}
		if strings.TrimSpace(s.LastError) != "" {
			errors++
		}
		if s.Storming {
			storming++
		}
	}
	filters := gin.H{
		"all":      len(servers),
		"running":  active,
		"starting": starting,
		"paused":   paused,
		"stopped":  stopped,
		"errors":   errors,
		"storming": storming,
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
	data["filters"] = filters

	return data, nil
}
