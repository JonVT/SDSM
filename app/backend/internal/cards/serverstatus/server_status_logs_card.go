package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const serverStatusLogsTemplate = "cards/server_status_logs.html"

type serverStatusLogsCard struct{}

func init() {
	cards.Register(serverStatusLogsCard{})
}

func (serverStatusLogsCard) ID() string {
	return "server-status-logs"
}

func (serverStatusLogsCard) Template() string {
	return serverStatusLogsTemplate
}

func (serverStatusLogsCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusLogsCard) Slot() cards.Slot {
	return cards.SlotFooter
}

func (serverStatusLogsCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}

	data := gin.H{
		"server": req.Server,
	}

	if req.Payload != nil {
		if role, ok := req.Payload["role"].(string); ok {
			data["role"] = role
		}
	}

	return data, nil
}
