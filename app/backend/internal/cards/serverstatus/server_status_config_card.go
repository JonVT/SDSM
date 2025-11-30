package serverstatus

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
)

const serverStatusConfigTemplate = "cards/server_status_config.html"

type serverStatusConfigCard struct{}

func init() {
	cards.Register(serverStatusConfigCard{})
}

func (serverStatusConfigCard) ID() string {
	return "server-status-config"
}

func (serverStatusConfigCard) Template() string {
	return serverStatusConfigTemplate
}

func (serverStatusConfigCard) Screens() []cards.Screen {
	return []cards.Screen{cards.ScreenServerStatus}
}

func (serverStatusConfigCard) Slot() cards.Slot {
	return cards.SlotGrid
}

func (serverStatusConfigCard) Capabilities() cards.CardCapabilities {
	return cards.CardCapabilities{
		AllowedRoles: []string{string(manager.RoleAdmin)},
	}
}

func (serverStatusConfigCard) FetchData(req *cards.Request) (gin.H, error) {
	if req == nil || req.Server == nil {
		return nil, errors.New("server context required")
	}

	data := gin.H{
		"server": req.Server,
	}

	if ds := req.Datasets; ds != nil {
		if summary := ds.ServerSummary(); summary != nil {
			data["resolved_world_id"] = summary.ResolvedWorldID
		}
		if cfg := ds.ServerConfig(); cfg != nil {
			data["worlds"] = cfg.Worlds
			data["worldData"] = cfg.WorldData
			data["release_difficulties"] = cfg.ReleaseDifficulties
			data["beta_difficulties"] = cfg.BetaDifficulties
			data["release_languages"] = cfg.ReleaseLanguages
			data["beta_languages"] = cfg.BetaLanguages
			data["world_ids"] = cfg.WorldIDs
		}
	}

	if req.Payload != nil {
		for _, key := range []string{
			"worlds",
			"worldData",
			"release_difficulties",
			"beta_difficulties",
			"resolved_world_id",
			"release_languages",
			"beta_languages",
			"world_ids",
		} {
			if _, exists := data[key]; !exists {
				if val, ok := req.Payload[key]; ok {
					data[key] = val
				}
			}
		}
	}

	if _, ok := data["resolved_world_id"]; !ok {
		id := strings.TrimSpace(req.Server.WorldID)
		if id == "" {
			id = strings.TrimSpace(req.Server.World)
		}
		data["resolved_world_id"] = id
	}

	if _, ok := data["worlds"]; !ok {
		data["worlds"] = map[string][]gin.H{
			"release": {},
			"beta":    {},
		}
	}
	if _, ok := data["worldData"]; !ok {
		data["worldData"] = map[string]map[string]gin.H{
			"release": {},
			"beta":    {},
		}
	}
	if _, ok := data["release_difficulties"]; !ok {
		data["release_difficulties"] = []string{}
	}
	if _, ok := data["beta_difficulties"]; !ok {
		data["beta_difficulties"] = []string{}
	}

	return data, nil
}
