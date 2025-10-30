package handlers

import (
	"net/http"
	"strings"
	"time"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"

	"github.com/gin-gonic/gin"
)

type UserHandlers struct {
	users       *manager.UserStore
	authService *middleware.AuthService
}

func NewUserHandlers(store *manager.UserStore, auth *middleware.AuthService) *UserHandlers {
	return &UserHandlers{users: store, authService: auth}
}

func (h *UserHandlers) UsersGET(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required."})
		return
	}
	// Snapshot of users
	list := h.users.Users()
	c.HTML(http.StatusOK, "users.html", gin.H{
		"users":    list,
		"username": c.GetString("username"),
		"now":      time.Now(),
	})
}

func (h *UserHandlers) UsersPOST(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required."})
		return
	}

	// Determine action
	if c.PostForm("create") != "" {
		username := middleware.SanitizeString(c.PostForm("new_username"))
		roleStr := strings.ToLower(strings.TrimSpace(c.PostForm("new_role")))
		password := c.PostForm("new_password")
		if len(username) < 3 || len(password) < 8 {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{
				"error": "Username must be at least 3 characters and password at least 8 characters.",
				"users": h.users.Users(),
			})
			return
		}
		role := manager.RoleUser
		if roleStr == string(manager.RoleAdmin) {
			role = manager.RoleAdmin
		}
		hash, err := h.authService.HashPassword(password)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "users.html", gin.H{"error": "Failed to hash password.", "users": h.users.Users()})
			return
		}
		if _, err := h.users.CreateUser(username, hash, role); err != nil {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": err.Error(), "users": h.users.Users()})
			return
		}
		c.Redirect(http.StatusFound, "/users")
		return
	}

	if del := strings.TrimSpace(c.PostForm("delete")); del != "" {
		// Prevent deleting the only admin
		if u, ok := h.users.Get(del); ok && u.Role == manager.RoleAdmin {
			admins := 0
			for _, usr := range h.users.Users() {
				if usr.Role == manager.RoleAdmin {
					admins++
				}
			}
			if admins <= 1 {
				c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": "Cannot delete the last admin.", "users": h.users.Users()})
				return
			}
		}
		if err := h.users.Delete(del); err != nil {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": err.Error(), "users": h.users.Users()})
			return
		}
		c.Redirect(http.StatusFound, "/users")
		return
	}

	if target := strings.TrimSpace(c.PostForm("set_role_user")); target != "" {
		roleStr := strings.ToLower(strings.TrimSpace(c.PostForm("set_role_value")))
		if roleStr != string(manager.RoleAdmin) && roleStr != string(manager.RoleUser) {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": "Invalid role.", "users": h.users.Users()})
			return
		}
		// Prevent demoting the last admin
		if roleStr == string(manager.RoleUser) {
			if u, ok := h.users.Get(target); ok && u.Role == manager.RoleAdmin {
				admins := 0
				for _, usr := range h.users.Users() {
					if usr.Role == manager.RoleAdmin {
						admins++
					}
				}
				if admins <= 1 {
					c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": "At least one admin is required.", "users": h.users.Users()})
					return
				}
			}
		}
		if err := h.users.SetRole(target, manager.Role(roleStr)); err != nil {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": err.Error(), "users": h.users.Users()})
			return
		}
		c.Redirect(http.StatusFound, "/users")
		return
	}

	if target := strings.TrimSpace(c.PostForm("reset_password_user")); target != "" {
		newPass := c.PostForm("reset_password_value")
		if len(newPass) < 8 {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": "Password must be at least 8 characters.", "users": h.users.Users()})
			return
		}
		hash, err := h.authService.HashPassword(newPass)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "users.html", gin.H{"error": "Failed to hash password.", "users": h.users.Users()})
			return
		}
		if err := h.users.SetPassword(target, hash); err != nil {
			c.HTML(http.StatusBadRequest, "users.html", gin.H{"error": err.Error(), "users": h.users.Users()})
			return
		}
		c.Redirect(http.StatusFound, "/users")
		return
	}

	// Default: redirect back
	c.Redirect(http.StatusFound, "/users")
}
