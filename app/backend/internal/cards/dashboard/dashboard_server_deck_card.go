package dashboard

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardServerDeckTemplate = "cards/dashboard_server_deck.html"

type dashboardServerDeckCard struct{}

func init() {
	cards.Register(dashboardServerDeckCard{})
}

func (dashboardServerDeckCard) ID() string {
	return "dashboard-server-deck"
}

func (dashboardServerDeckCard) Template() string {
	return dashboardServerDeckTemplate
}

func (dashboardServerDeckCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardServerDeckCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardServerDeckCard) FetchData(req *cards.Request) (gin.H, error) {
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
	data["active"] = active
	data["startable"] = startable
	data["context"] = context
	if req.Payload != nil {
		if deckRaw, ok := req.Payload["serverDeck"].(map[string]interface{}); ok {
			for k, v := range deckRaw {
				data[k] = v
			}
		}
	}
	return data, nil
}
