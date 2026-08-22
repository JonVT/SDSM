package users

import (
	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const usersAssignmentTemplate = "cards/users_assignment.html"

type usersAssignmentCard struct{}

func init() {
	cards.Register(usersAssignmentCard{})
}

func (usersAssignmentCard) ID() string {
	return "users-assignment"
}

func (usersAssignmentCard) Template() string {
	return usersAssignmentTemplate
}

func (usersAssignmentCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenUsers}
}

func (usersAssignmentCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (usersAssignmentCard) FetchData(req *cards.Request) (gin.H, error) {
	data := gin.H{}
	if req == nil {
		return data, nil
	}
	if req.Payload != nil {
		if assignments, ok := req.Payload["userAssignments"]; ok {
			data["assignments"] = assignments
		}
		if serverOptions, ok := req.Payload["serverOptions"]; ok {
			data["serverOptions"] = serverOptions
		}
	}
	return data, nil
}
