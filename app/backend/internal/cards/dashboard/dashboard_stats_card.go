package dashboard

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

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
	setIfMissing("pendingUpdates")
	setIfMissing("componentsHealthy")
	setIfMissing("componentsTotal")
	setIfMissing("systemHealthPercent")
	setIfMissing("systemHealthLabel")
	setIfMissing("cpuPercent")
	setIfMissing("cpuPercentLabel")
	setIfMissing("memoryPercent")
	setIfMissing("memoryPercentLabel")
	setIfMissing("memoryDetail")
	setIfMissing("diskPercent")
	setIfMissing("diskPercentLabel")
	setIfMissing("diskDetail")
	setIfMissing("telemetrySample")
	setIfMissing("telemetrySampleTime")
	setIfMissing("telemetrySampleISO")
	setIfMissing("telemetryStatus")
	setIfMissing("telemetryStatusClass")
	setIfMissing("telemetryStatusDetail")
	setIfMissing("telemetryIsStale")
	setIfMissing("telemetrySampleAge")
	setIfMissing("componentAlerts")
	setIfMissing("componentAlertsLabel")
	setIfMissing("loadAverageLabel")
	setIfMissing("uptimeLabel")
	setIfMissing("lastUpdated")
	setIfMissing("lastUpdatedISO")

	data["systemHealthPercent"] = normalizePercent(data["systemHealthPercent"])

	return data, nil
}

func normalizePercent(value interface{}) float64 {
	percent := toFloat(value)
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0
	}
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func toFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
	case string:
		trimmed := strings.TrimSpace(strings.TrimSuffix(v, "%"))
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return f
		}
	case nil:
		return 0
	}
	return 0
}
