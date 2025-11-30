package users

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const usersManagementTemplate = "cards/users_management.html"

type usersManagementCard struct{}

func init() {
	cards.Register(usersManagementCard{})
}

func (usersManagementCard) ID() string {
	return "users-management"
}

func (usersManagementCard) Template() string {
	return usersManagementTemplate
}

func (usersManagementCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenUsers}
}

func (usersManagementCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (usersManagementCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}
	if req.Payload != nil {
		if users, ok := req.Payload["users"]; ok {
			data["users"] = users
		}
		if serverOptions, ok := req.Payload["serverOptions"]; ok {
			data["serverOptions"] = serverOptions
		}
	}
	return data, nil
}
