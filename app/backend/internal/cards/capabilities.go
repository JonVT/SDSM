package cards

import (
	"strings"
)

// CapabilityAwareCard can declare additional requirements before a card renders.
type CapabilityAwareCard interface {
	Capabilities() CardCapabilities
}

// CardCapabilities captures optional guard rails evaluated before rendering.
type CardCapabilities struct {
	// RequirePlayerSaves hides the card unless the active server has PlayerSaves enabled.
	RequirePlayerSaves bool
	// RequireServerRunning hides the card unless the server process is running.
	RequireServerRunning bool
	// AllowedRoles restricts rendering to a list of user roles (case-insensitive).
	// When empty, all authenticated roles may view the card.
	AllowedRoles []string
}

// Allows reports whether the provided request satisfies the card capabilities.
func (caps CardCapabilities) Allows(req *Request) bool {
	if isZeroCapabilities(caps) {
		return true
	}
	if caps.RequirePlayerSaves {
		if req == nil || req.Server == nil || !req.Server.PlayerSaves {
			return false
		}
	}
	if caps.RequireServerRunning {
		if req == nil || req.Server == nil || !req.Server.IsRunning() {
			return false
		}
	}
	if len(caps.AllowedRoles) > 0 {
		role := normalizeRole(roleFromRequest(req))
		if role == "" {
			return false
		}
		allowed := false
		for _, candidate := range caps.AllowedRoles {
			if normalizeRole(candidate) == role {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func isZeroCapabilities(caps CardCapabilities) bool {
	return !caps.RequirePlayerSaves && !caps.RequireServerRunning && len(caps.AllowedRoles) == 0
}

func roleFromRequest(req *Request) string {
	if req == nil {
		return ""
	}
	if req.Context != nil {
		if role := strings.TrimSpace(req.Context.GetString("role")); role != "" {
			return role
		}
	}
	if req.Payload != nil {
		if role, ok := req.Payload["role"].(string); ok {
			if trimmed := strings.TrimSpace(role); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func normalizeRole(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(role))
}

func cardEnabledForRequest(card Card, req *Request) bool {
	if req != nil && req.Server != nil {
		if !req.Server.IsCardEnabled(card.ID()) {
			return false
		}
	}
	capabilityCard, ok := card.(CapabilityAwareCard)
	if !ok {
		return true
	}
	return capabilityCard.Capabilities().Allows(req)
}
