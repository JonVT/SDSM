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
// ProfilePOST removed: legacy HTML form password change has been replaced by /api/profile/password.

// APIProfileChangePassword updates the current user's password via JSON API.
// Request JSON: { "current_password": string, "new_password": string, "confirm_password": string }
// Responses:
//
//	200 OK on success {"status":"ok"}
//	400 Bad Request on validation failure {"error": string}
//	401 Unauthorized if current password is incorrect {"error": string}
//	500 Internal Server Error if hashing/saving fails {"error": string}
func (h *ProfileHandlers) APIProfileChangePassword(c *gin.Context) {
	username := c.GetString("username")
	type reqBody struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
		Confirm string `json:"confirm_password"`
	}
	var req reqBody
	if err := c.BindJSON(&req); err != nil {
		ToastError(c, "Invalid Request", "Malformed JSON payload")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	newPass := strings.TrimSpace(req.New)
	confirm := strings.TrimSpace(req.Confirm)
	if newPass == "" || len(newPass) < 8 {
		ToastError(c, "Weak Password", "New password must be at least 8 characters.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "new password must be at least 8 characters"})
		return
	}
	if newPass != confirm {
		ToastError(c, "Mismatch", "Passwords do not match.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "passwords do not match"})
		return
	}
	// Verify current password
	u, ok := h.users.Get(username)
	if !ok || !h.authService.CheckPassword(req.Current, u.PasswordHash) {
		ToastError(c, "Incorrect Password", "Current password is incorrect.")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "current password incorrect"})
		return
	}
	// Hash and persist
	hash, err := h.authService.HashPassword(newPass)
	if err != nil {
		ToastError(c, "Update Failed", "Failed to update password.")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "hash failure"})
		return
	}
	if err := h.users.SetPassword(username, hash); err != nil {
		ToastError(c, "Save Failed", "Failed to save new password.")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "save failure"})
		return
	}
	ToastSuccess(c, "Password Updated", "Your password has been changed.")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
