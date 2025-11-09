package handlers

import (
	"fmt"
	"sdsm/internal/models"
	"strings"
)

// ApplyCoreChangeEffects sets PendingSavePurge if core start parameters changed
// and redeploys when beta channel flips. Returns (coreChanged, redeployed, error).
func (h *ManagerHandlers) ApplyCoreChangeEffects(s *models.Server, origWorld, origStartLoc, origStartCond string, originalBeta bool) (bool, bool, error) {
	if s == nil || h == nil || h.manager == nil {
		return false, false, fmt.Errorf("invalid context")
	}
	coreChanged := false
	if stringsTrim(origWorld) != stringsTrim(s.World) || stringsTrim(origStartLoc) != stringsTrim(s.StartLocation) || stringsTrim(origStartCond) != stringsTrim(s.StartCondition) {
		coreChanged = true
		s.PendingSavePurge = true
		if s.Logger != nil {
			s.Logger.Write("Core start parameters changed; pending save purge flagged (stub, no deletion yet)")
		}
	}
	redeployed := false
	if s.Beta != originalBeta {
		h.manager.Log.Write(fmt.Sprintf("Server %s (ID: %d) game version changed; redeploying...", s.Name, s.ID))
		if err := s.Deploy(); err != nil {
			return coreChanged, false, err
		}
		redeployed = true
	}
	return coreChanged, redeployed, nil
}

func stringsTrim(s string) string { return strings.TrimSpace(s) }
