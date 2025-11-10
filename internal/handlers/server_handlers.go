package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *ManagerHandlers) ServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Invalid server ID",
		})
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Server not found",
		})
		return
	}

	if role != "admin" {
		if user, ok := username.(string); ok {
			if h.userStore == nil || !h.userStore.CanAccess(user, s.ID) {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "You do not have access to this server."})
				return
			}
		}
	}

	h.renderServerPage(c, http.StatusOK, s, username, "")
}

// ServerPOST removed: legacy multi-action form handler replaced by distinct /api/servers/:id/* endpoints.

func (h *ManagerHandlers) ServerProgressGET(c *gin.Context) {
	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	progress := h.manager.ServerProgressSnapshot(serverID)
	c.JSON(http.StatusOK, progress)
}

func (h *ManagerHandlers) NewServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	h.renderNewServerForm(c, http.StatusOK, username, nil)
}

// NewServerPOST removed: server creation now performed via /api/servers and /api/servers/create-from-save.
