package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"sdsm/app/backend/internal/cards"
)

func (h *ManagerHandlers) ServerGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		h.renderError(c, http.StatusNotFound, "Invalid server ID")
		return
	}

	s := h.manager.ServerByID(serverID)
	if s == nil {
		h.renderError(c, http.StatusNotFound, "Server not found")
		return
	}

	if role != "admin" {
		if user, ok := username.(string); ok {
			if h.userStore == nil || !h.userStore.CanAccess(user, s.ID) {
				h.renderError(c, http.StatusForbidden, "You do not have access to this server.")
				return
			}
		}
	}

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		h.renderServerPage(c, http.StatusOK, s, username, "")
		return
	}

	// Otherwise, render the full frame with the server status payload embedded.
	serverPayload := h.serverStatusPayload(c, s, username, "")
	frameData := gin.H{
		"username":          username,
		"role":              role,
		"servers":           h.manager.Servers,
		"buildTime":         h.manager.BuildTime(),
		"active":            h.manager.IsActive(),
		"page":              "server_status",
		"title":             s.Name,
		"currentServerPath": serverPayload["currentServerPath"],
	}
	for key, value := range serverPayload {
		frameData[key] = value
	}

	c.HTML(http.StatusOK, "frame.html", frameData)
}

// ServerCardGET renders a single card partial for HTMX refresh requests.
func (h *ManagerHandlers) ServerCardGET(c *gin.Context) {
	username, _ := c.Get("username")
	role := c.GetString("role")

	serverID, err := strconv.Atoi(c.Param("server_id"))
	if err != nil {
		c.String(http.StatusNotFound, "invalid server id")
		return
	}
	if h.manager == nil {
		c.String(http.StatusServiceUnavailable, "manager unavailable")
		return
	}
	s := h.manager.ServerByID(serverID)
	if s == nil {
		c.String(http.StatusNotFound, "server not found")
		return
	}

	if role != "admin" {
		if user, ok := username.(string); ok {
			if h.userStore == nil || !h.userStore.CanAccess(user, s.ID) {
				c.String(http.StatusForbidden, "access denied")
				return
			}
		}
	}

	cardID := strings.TrimSpace(c.Param("card_id"))
	if cardID == "" {
		c.String(http.StatusBadRequest, "card id required")
		return
	}
	screenParam := strings.TrimSpace(c.DefaultQuery("screen", string(cards.ScreenServerStatus)))
	screen := cards.Screen(screenParam)

	builder := h.cardRequestBuilder
	if builder == nil {
		builder = h.buildServerStatusCardRequest
	}
	cardReq, payload := builder(c, s, username, "")
	if payload == nil {
		c.String(http.StatusInternalServerError, "card context unavailable")
		return
	}
	renderable, ok := cards.BuildRenderableByID(screen, cardID, cardReq)
	if !ok {
		c.String(http.StatusNotFound, "card not found")
		return
	}

	c.HTML(http.StatusOK, renderable.Template, renderable.Data)
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
	role, _ := c.Get("role")

	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		h.renderNewServerForm(c, http.StatusOK, username, nil)
		return
	}

	// Otherwise, render the full frame with the form payload so the page mirrors the HTMX version
	formPayload := h.buildNewServerFormPayload(username, nil)
	frameData := gin.H{
		"username":  username,
		"role":      role,
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "server_new",
		"title":     "Create New Server",
	}
	for k, v := range formPayload {
		frameData[k] = v
	}

	c.HTML(http.StatusOK, "frame.html", frameData)
}

// NewServerPOST removed: server creation now performed via /api/servers and /api/servers/create-from-save.
