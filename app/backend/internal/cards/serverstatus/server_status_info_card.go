package serverstatus

import (
	"errors"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

const serverStatusInfoTemplate = "cards/server_status_info.html"

type serverStatusInfoCard struct{}

func init() {
	cards.Register(serverStatusInfoCard{})
}

func (serverStatusInfoCard) ID() string {
	return "server-status-info"
}

func (serverStatusInfoCard) Template() string {
	return serverStatusInfoTemplate
}

func (serverStatusInfoCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusInfoCard) Slot() cards.Slot {
	return cards.SlotPrimary
}

func (serverStatusInfoCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}

	data := gin.H{
		"server": req.Server,
	}
	if ds := req.Datasets; ds != nil {
		if summary := ds.ServerSummary(); summary != nil {
			data["language_options"] = summary.LanguageOptions
			data["worldInfo"] = summary.WorldInfo
			if summary.StartLocationInfo != nil {
				data["start_location_info"] = summary.StartLocationInfo
			}
			if summary.StartConditionInfo != nil {
				data["start_condition_info"] = summary.StartConditionInfo
			}
		}
	} else if req.Payload != nil {
		if value, ok := req.Payload["language_options"]; ok {
			data["language_options"] = value
		}
		if value, ok := req.Payload["worldInfo"]; ok {
			data["worldInfo"] = value
		}
		if value, ok := req.Payload["start_location_info"]; ok {
			data["start_location_info"] = value
		}
		if value, ok := req.Payload["start_condition_info"]; ok {
			data["start_condition_info"] = value
		}
	}

	return data, nil
}
