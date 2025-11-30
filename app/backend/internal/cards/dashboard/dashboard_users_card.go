package dashboard

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const dashboardUsersTemplate = "cards/dashboard_users.html"

type dashboardUsersCard struct{}

func init() {
	cards.Register(dashboardUsersCard{})
}

func (dashboardUsersCard) ID() string {
	return "dashboard-users"
}

func (dashboardUsersCard) Template() string {
	return dashboardUsersTemplate
}

func (dashboardUsersCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenDashboard}
}

func (dashboardUsersCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (dashboardUsersCard) FetchData(req *cards.Request) (gin.H, error) {
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
	data["role"] = strings.ToLower(strings.TrimSpace(role))

	if req.Payload != nil {
		switch stats := req.Payload["userStats"].(type) {
		case gin.H:
			mergeUserStats(data, stats)
		case map[string]interface{}:
			mergeUserStats(data, stats)
		}
	}

	if _, ok := data["total"]; !ok {
		data["total"] = 0
	}
	if _, ok := data["admins"]; !ok {
		data["admins"] = 0
	}
	if _, ok := data["operators"]; !ok {
		data["operators"] = 0
	}
	if _, ok := data["empty"]; !ok {
		data["empty"] = data["total"] == 0
	}

	return data, nil
}

func mergeUserStats(dst gin.H, src map[string]interface{}) {
	for key, value := range src {
		switch key {
		case "total", "admins", "operators":
			dst[key] = extractInt(value)
		case "empty":
			dst[key] = toBool(value)
		default:
			dst[key] = value
		}
	}
}

func extractInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 0
}

func toBool(val interface{}) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return b
		}
	}
	return false
}
