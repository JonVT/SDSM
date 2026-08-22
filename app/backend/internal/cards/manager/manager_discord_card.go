package manager

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const managerDiscordTemplate = "cards/manager_discord.html"

type managerDiscordCard struct{}

func init() {
	cards.Register(managerDiscordCard{})
}

func (managerDiscordCard) ID() string {
	return "manager-discord"
}

func (managerDiscordCard) Template() string {
	return managerDiscordTemplate
}

func (managerDiscordCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenManager}
}

func (managerDiscordCard) Slot() cards.Slot {
	return cards.SlotFooter
}

func (managerDiscordCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}
	if req.Payload != nil {
		for k, v := range req.Payload {
			data[k] = v
		}
	}
	if req.Manager != nil {
		data["manager"] = req.Manager
	}
	return data, nil
}
