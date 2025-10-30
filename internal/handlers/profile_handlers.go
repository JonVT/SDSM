package handlers

import (
	"net/http"
	"strings"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"

	"github.com/gin-gonic/gin"
)

// ProfileHandlers provides endpoints for self-service account actions
type ProfileHandlers struct {
	users       *manager.UserStore
	authService *middleware.AuthService
}

func NewProfileHandlers(store *manager.UserStore, auth *middleware.AuthService) *ProfileHandlers {
	return &ProfileHandlers{users: store, authService: auth}
}

// ProfileGET renders the profile page for the current user
func (h *ProfileHandlers) ProfileGET(c *gin.Context) {
	username := c.GetString("username")
	role := c.GetString("role")
	c.HTML(http.StatusOK, "profile.html", gin.H{
		"username": username,
		"role":     role,
	})
}

// ProfilePOST handles updating the current user's password
func (h *ProfileHandlers) ProfilePOST(c *gin.Context) {
	username := c.GetString("username")
	role := c.GetString("role")

	current := c.PostForm("current_password")
	newPass := strings.TrimSpace(c.PostForm("new_password"))
	confirm := strings.TrimSpace(c.PostForm("confirm_password"))

	// Basic validation
	if newPass == "" || len(newPass) < 8 {
		c.HTML(http.StatusBadRequest, "profile.html", gin.H{
			"username": username,
			"role":     role,
			"error":    "New password must be at least 8 characters.",
		})
		return
	}
	if newPass != confirm {
		c.HTML(http.StatusBadRequest, "profile.html", gin.H{
			"username": username,
			"role":     role,
			"error":    "Passwords do not match.",
		})
		return
	}

	// Verify current password
	u, ok := h.users.Get(username)
	if !ok || !h.authService.CheckPassword(current, u.PasswordHash) {
		c.HTML(http.StatusUnauthorized, "profile.html", gin.H{
			"username": username,
			"role":     role,
			"error":    "Current password is incorrect.",
		})
		return
	}

	// Hash and persist
	hash, err := h.authService.HashPassword(newPass)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "profile.html", gin.H{
			"username": username,
			"role":     role,
			"error":    "Failed to update password.",
		})
		return
	}
	if err := h.users.SetPassword(username, hash); err != nil {
		c.HTML(http.StatusInternalServerError, "profile.html", gin.H{
			"username": username,
			"role":     role,
			"error":    "Failed to save new password.",
		})
		return
	}

	// Success - keep the same session; show confirmation
	c.HTML(http.StatusOK, "profile.html", gin.H{
		"username": username,
		"role":     role,
		"success":  "Password updated successfully.",
	})
}
