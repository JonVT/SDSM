package dashboard

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardSystemHealthTemplate = "cards/dashboard_system_health.html"

type dashboardSystemHealthCard struct{}

func init() {
	cards.Register(dashboardSystemHealthCard{})
}

func (dashboardSystemHealthCard) ID() string {
	return "dashboard-system-health"
}

func (dashboardSystemHealthCard) Template() string {
	return dashboardSystemHealthTemplate
}

func (dashboardSystemHealthCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardSystemHealthCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardSystemHealthCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}
	if req.Payload != nil {
		if ctx, ok := req.Payload["healthCtx"].(gin.H); ok {
			for k, v := range ctx {
				data[k] = v
			}
		} else if ctx, ok := req.Payload["healthCtx"].(map[string]interface{}); ok {
			for k, v := range ctx {
				data[k] = v
			}
		}
		if telemetry, ok := req.Payload["systemTelemetry"]; ok {
			data["systemTelemetry"] = telemetry
		}
	}
	if _, ok := data["score"]; !ok {
		data["score"] = "Offline"
	}
	if _, ok := data["pill"]; !ok {
		if score, _ := data["score"].(string); score != "" {
			data["pill"] = score
		} else {
			data["pill"] = "Offline"
		}
	}
	if _, ok := data["telemetryStatus"]; !ok {
		data["telemetryStatus"] = "Awaiting data"
	}
	if _, ok := data["telemetryStatusClass"]; !ok {
		data["telemetryStatusClass"] = "is-muted"
	}
	if _, ok := data["telemetryStatusDetail"]; !ok {
		data["telemetryStatusDetail"] = "Telemetry has not reported yet"
	}
	if _, ok := data["componentAlerts"]; !ok {
		data["componentAlerts"] = []string{}
	}
	if _, ok := data["componentAlertCount"]; !ok {
		if alerts, ok := data["componentAlerts"].([]string); ok {
			data["componentAlertCount"] = len(alerts)
		} else {
			data["componentAlertCount"] = 0
		}
	}
	return data, nil
}
