package handlers

import "sdsm/internal/models"

// BroadcastStatusAndStats emits status for a server (if provided) and global stats.
func (h *ManagerHandlers) BroadcastStatusAndStats(s *models.Server) {
	if h == nil {
		return
	}
	if s != nil {
		h.broadcastServerStatus(s)
	}
	h.broadcastStats()
}
