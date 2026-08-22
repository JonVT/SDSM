package users

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const usersOverviewTemplate = "cards/users_overview.html"

type usersOverviewCard struct{}

func init() {
	cards.Register(usersOverviewCard{})
}

func (usersOverviewCard) ID() string {
	return "users-overview"
}

func (usersOverviewCard) Template() string {
	return usersOverviewTemplate
}

func (usersOverviewCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenUsers}
}

func (usersOverviewCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (usersOverviewCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}

	stats := gin.H{
		"total":     0,
		"admins":    0,
		"operators": 0,
		"empty":     true,
	}
	if req.Payload != nil {
		if raw, ok := req.Payload["userStats"]; ok {
			switch src := raw.(type) {
			case gin.H:
				mergeUserStats(stats, map[string]interface{}(src))
			case map[string]interface{}:
				mergeUserStats(stats, src)
			}
		}
		if now, ok := req.Payload["now"].(time.Time); ok {
			data["updatedAt"] = now
		}
		if username, ok := req.Payload["username"].(string); ok {
			data["username"] = strings.TrimSpace(username)
		}
	}

	data["stats"] = stats
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
