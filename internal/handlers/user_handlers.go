package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"
	"sdsm/internal/utils"

	"github.com/gin-gonic/gin"
)

type UserHandlers struct {
	users       *manager.UserStore
	authService *middleware.AuthService
	logger      *utils.Logger
}

// NewUserHandlers constructs handlers with optional logger (nil-safe).
func NewUserHandlers(store *manager.UserStore, auth *middleware.AuthService, logger *utils.Logger) *UserHandlers {
	return &UserHandlers{users: store, authService: auth, logger: logger}
}

func (h *UserHandlers) UsersGET(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		if h.logger != nil {
			uname := strings.TrimSpace(c.GetString("username"))
			h.logger.Write(fmt.Sprintf("UsersGET: forbidden for user '%s' (role=%s)", uname, c.GetString("role")))
		}
		c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required."})
		return
	}
	// Best-effort refresh from disk in case users.json was edited externally
	_ = h.users.Load()
	// Snapshot of users
	list := h.users.Users()
	// Fallback: if store is empty but current authenticated user is 'admin', synthesize an admin record.
	if len(list) == 0 {
		uname := strings.TrimSpace(c.GetString("username"))
		if strings.EqualFold(uname, "admin") && h.users.AdminCount() == 0 {
			list = append(list, manager.User{Username: "admin", Role: manager.RoleAdmin, CreatedAt: time.Now()})
			if h.logger != nil {
				h.logger.Write("UsersGET: synthetic admin fallback applied (empty store)")
			}
		}
	}
	if h.logger != nil {
		usernames := make([]string, 0, len(list))
		for _, u := range list {
			usernames = append(usernames, u.Username+":"+string(u.Role))
		}
		h.logger.Write(fmt.Sprintf("UsersGET: returning %d user(s): %s", len(list), strings.Join(usernames, ",")))
	}
	c.HTML(http.StatusOK, "users.html", gin.H{
		"users":    list,
		"username": c.GetString("username"),
		"now":      time.Now(),
	})
}

// UsersPOST removed: legacy HTML form user management replaced by /api/users endpoints.

// --- JSON API for user management (admin only) ---

// APIUsersList returns users, optionally filtered by ?q=
func (h *UserHandlers) APIUsersList(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		if h.logger != nil {
			uname := strings.TrimSpace(c.GetString("username"))
			h.logger.Write(fmt.Sprintf("APIUsersList: forbidden for user '%s' (role=%s)", uname, c.GetString("role")))
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	// Best-effort refresh from disk to reflect any external changes
	_ = h.users.Load()
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	users := h.users.Users()
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		if q != "" {
			if !strings.Contains(strings.ToLower(u.Username), q) && !strings.Contains(strings.ToLower(string(u.Role)), q) {
				continue
			}
		}
		out = append(out, gin.H{
			"username":   u.Username,
			"role":       u.Role,
			"created_at": u.CreatedAt,
		})
	}
	// Fallback synthetic admin if store empty but authenticated user is 'admin'
	if len(out) == 0 {
		uname := strings.TrimSpace(c.GetString("username"))
		if strings.EqualFold(uname, "admin") && h.users.AdminCount() == 0 {
			out = append(out, gin.H{
				"username":   "admin",
				"role":       manager.RoleAdmin,
				"created_at": time.Now(),
			})
			if h.logger != nil {
				h.logger.Write("APIUsersList: synthetic admin fallback applied (empty store)")
			}
		}
	}
	if h.logger != nil {
		usernames := make([]string, 0, len(out))
		for _, obj := range out {
			if name, ok := obj["username"].(string); ok {
				if role, ok2 := obj["role"].(manager.Role); ok2 {
					usernames = append(usernames, name+":"+string(role))
				} else if roleStr, ok3 := obj["role"].(string); ok3 {
					usernames = append(usernames, name+":"+roleStr)
				}
			}
		}
		if q != "" {
			h.logger.Write(fmt.Sprintf("APIUsersList: query='%s' matched %d user(s): %s", q, len(out), strings.Join(usernames, ",")))
		} else {
			h.logger.Write(fmt.Sprintf("APIUsersList: returning %d user(s): %s", len(out), strings.Join(usernames, ",")))
		}
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type apiCreateUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *UserHandlers) APIUsersCreate(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	var req apiCreateUserReq
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	username := middleware.SanitizeString(strings.TrimSpace(req.Username))
	password := req.Password
	if len(username) < 3 || len(password) < 8 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "username >=3 and password >=8 required"})
		return
	}
	roleStr := strings.ToLower(strings.TrimSpace(req.Role))
	role := manager.RoleOperator
	switch roleStr {
	case string(manager.RoleAdmin):
		role = manager.RoleAdmin
	case string(manager.RoleOperator), "":
		role = manager.RoleOperator
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	hash, err := h.authService.HashPassword(password)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "hash failure"})
		return
	}
	if _, err := h.users.CreateUser(username, hash, role); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "ok"})
}

type apiSetRoleReq struct {
	Role string `json:"role"`
}

func (h *UserHandlers) APIUsersSetRole(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	var req apiSetRoleReq
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	roleStr := strings.ToLower(strings.TrimSpace(req.Role))
	if roleStr != string(manager.RoleAdmin) && roleStr != string(manager.RoleOperator) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	// Prevent demoting the last admin
	if roleStr != string(manager.RoleAdmin) {
		if u, ok := h.users.Get(username); ok && u.Role == manager.RoleAdmin {
			admins := 0
			for _, usr := range h.users.Users() {
				if usr.Role == manager.RoleAdmin {
					admins++
				}
			}
			if admins <= 1 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "at least one admin required"})
				return
			}
		}
	}
	if err := h.users.SetRole(username, manager.Role(roleStr)); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// API: get/set operator server assignments
func (h *UserHandlers) APIUsersGetAssignments(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	all, list, err := h.users.GetAssignments(username)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"all": all, "servers": list})
}

type apiSetAssignmentsReq struct {
	All     bool  `json:"all"`
	Servers []int `json:"servers"`
}

func (h *UserHandlers) APIUsersSetAssignments(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	var req apiSetAssignmentsReq
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	if err := h.users.SetAssignments(username, req.All, req.Servers); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *UserHandlers) APIUsersDelete(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	if u, ok := h.users.Get(username); ok && u.Role == manager.RoleAdmin {
		admins := 0
		for _, usr := range h.users.Users() {
			if usr.Role == manager.RoleAdmin {
				admins++
			}
		}
		if admins <= 1 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "cannot delete last admin"})
			return
		}
	}
	if err := h.users.Delete(username); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type apiResetPasswordReq struct {
	Password string `json:"password"`
}

func (h *UserHandlers) APIUsersResetPassword(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	var req apiResetPasswordReq
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	if len(req.Password) < 8 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "password must be >= 8 chars"})
		return
	}
	hash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "hash failure"})
		return
	}
	if err := h.users.SetPassword(username, hash); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
