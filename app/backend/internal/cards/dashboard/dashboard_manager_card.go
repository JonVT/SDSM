package dashboard

import (
	"time"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardManagerTemplate = "cards/dashboard_manager.html"

type dashboardManagerCard struct{}

func init() {
	cards.Register(dashboardManagerCard{})
}

func (dashboardManagerCard) ID() string {
	return "dashboard-manager"
}

func (dashboardManagerCard) Template() string {
	return dashboardManagerTemplate
}

func (dashboardManagerCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardManagerCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardManagerCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil || req.Payload == nil {
		return defaultManagerCardData(data), nil
	}
	if ctx, ok := req.Payload["managerCard"].(gin.H); ok {
		for k, v := range ctx {
			data[k] = v
		}
	} else if ctx, ok := req.Payload["managerCard"].(map[string]interface{}); ok {
		for k, v := range ctx {
			data[k] = v
		}
	}
	return defaultManagerCardData(data), nil
}

func defaultManagerCardData(data gin.H) gin.H {
	if _, ok := data["port"]; !ok {
		data["port"] = 0
	}
	if _, ok := data["port_label"]; !ok {
		data["port_label"] = "—"
	}
	if _, ok := data["root_path"]; !ok {
		data["root_path"] = ""
	}
	if _, ok := data["root_label"]; !ok {
		data["root_label"] = "Not configured"
	}
	if _, ok := data["components_total"]; !ok {
		data["components_total"] = 0
	}
	if _, ok := data["components_uptodate"]; !ok {
		data["components_uptodate"] = 0
	}
	if _, ok := data["components_outdated"]; !ok {
		data["components_outdated"] = 0
	}
	if _, ok := data["pill_class"]; !ok {
		data["pill_class"] = "is-warning"
	}
	if _, ok := data["status_label"]; !ok {
		data["status_label"] = "Status Unknown"
	}
	if _, ok := data["status_meta"]; !ok {
		data["status_meta"] = ""
	}
	if _, ok := data["status_meta_hidden"]; !ok {
		data["status_meta_hidden"] = true
	}
	if _, ok := data["timestamp"]; !ok {
		data["timestamp"] = time.Now()
	}
	if _, ok := data["timestamp_iso"]; !ok {
		data["timestamp_iso"] = time.Now().UTC().Format(time.RFC3339)
	}
	return data
}
