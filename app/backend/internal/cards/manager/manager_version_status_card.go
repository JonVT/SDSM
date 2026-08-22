package manager

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const managerVersionStatusTemplate = "cards/manager_version_status.html"

type managerVersionStatusCard struct{}

func init() {
	cards.Register(managerVersionStatusCard{})
}

func (managerVersionStatusCard) ID() string {
	return "manager-version-status"
}

func (managerVersionStatusCard) Template() string {
	return managerVersionStatusTemplate
}

func (managerVersionStatusCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenManager}
}

func (managerVersionStatusCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (managerVersionStatusCard) FetchData(req *cards.Request) (gin.H, error) {
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
