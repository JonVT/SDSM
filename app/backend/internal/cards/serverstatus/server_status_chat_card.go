package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const serverStatusChatTemplate = "cards/server_status_chat.html"

type serverStatusChatCard struct{}

func init() {
	cards.Register(serverStatusChatCard{})
}

func (serverStatusChatCard) ID() string {
	return "server-status-chat"
}

func (serverStatusChatCard) Template() string {
	return serverStatusChatTemplate
}

func (serverStatusChatCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusChatCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusChatCard) FetchData(req *cards.Request) (gin.H, error) {
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

	if _, ok := data["server"]; !ok {
		data["server"] = req.Server
	}

	return data, nil
}
