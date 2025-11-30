package manager

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const managerConfigurationTemplate = "cards/manager_configuration.html"

type managerConfigurationCard struct{}

func init() {
	cards.Register(managerConfigurationCard{})
}

func (managerConfigurationCard) ID() string {
	return "manager-configuration"
}

func (managerConfigurationCard) Template() string {
	return managerConfigurationTemplate
}

func (managerConfigurationCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenManager}
}

func (managerConfigurationCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (managerConfigurationCard) FetchData(req *cards.Request) (gin.H, error) {
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
