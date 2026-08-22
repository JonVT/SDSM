package dashboard

import (
	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/models"
)

func extractServersFromPayload(req *cards.Request) []*models.Server {
	if req == nil || req.Payload == nil {
		return nil
	}
	if servers, ok := req.Payload["servers"].([]*models.Server); ok {
		return servers
	}
	if ctxRaw, ok := req.Payload["serverDeck"].(map[string]interface{}); ok {
		if ctx, ok := ctxRaw["context"].(map[string]interface{}); ok {
			if servers, ok := ctx["servers"].([]*models.Server); ok {
				return servers
			}
		}
	}
	return nil
}
