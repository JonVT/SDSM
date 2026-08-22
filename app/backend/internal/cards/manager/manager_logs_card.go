package manager

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const managerLogsTemplate = "cards/manager_logs.html"

type managerLogsCard struct{}

func init() {
	cards.Register(managerLogsCard{})
}

func (managerLogsCard) ID() string {
	return "manager-logs"
}

func (managerLogsCard) Template() string {
	return managerLogsTemplate
}

func (managerLogsCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenManager}
}

func (managerLogsCard) Slot() cards.Slot {
	return cards.SlotFooter
}

func (managerLogsCard) FetchData(req *cards.Request) (gin.H, error) {
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
