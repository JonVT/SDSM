package manager

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const managerControlTemplate = "cards/manager_control.html"

type managerControlCard struct{}

func init() {
	cards.Register(managerControlCard{})
}

func (managerControlCard) ID() string {
	return "manager-control"
}

func (managerControlCard) Template() string {
	return managerControlTemplate
}

func (managerControlCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenManager}
}

func (managerControlCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (managerControlCard) FetchData(req *cards.Request) (gin.H, error) {
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
