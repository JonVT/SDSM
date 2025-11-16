package handlers

import (
	"net/http"
	"strings"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"

	"github.com/gin-gonic/gin"
)

type AuthHandlers struct {
	authService *middleware.AuthService
	manager     *manager.Manager
	users       *manager.UserStore
}

type LoginRequest struct {
	Username string `json:"username" binding:"required" validate:"required,min=3,max=50"`
	Password string `json:"password" binding:"required" validate:"required,min=6"`
}

func NewAuthHandlers(authService *middleware.AuthService, mgr *manager.Manager, store *manager.UserStore) *AuthHandlers {
	h := &AuthHandlers{
		authService: authService,
		manager:     mgr,
		users:       store,
	}
	// Ensure latest on boot (ignore error if missing)
	_ = h.users.Load()
	return h
}

// Environment-based defaults removed; configuration is exclusively via sdsm.config.

func (h *AuthHandlers) LoginGET(c *gin.Context) {
	// If no users exist, redirect to admin setup
	if h.users.IsEmpty() {
		c.Redirect(http.StatusFound, "/admin/setup")
		return
	}
	// Check if already authenticated
	if token, _ := c.Cookie(middleware.CookieName); token != "" {
		if claims, err := h.authService.ValidateToken(token); err == nil {
			// Determine default landing page based on role
			target := "/dashboard"
			if claims != nil && strings.TrimSpace(claims.Username) != "" {
				if u, ok := h.users.Get(claims.Username); ok && u.Role == manager.RoleAdmin {
					target = "/manager"
				}
			}
			c.Redirect(http.StatusFound, target)
			return
		}
	}

	redirect := c.Query("redirect")
	c.HTML(http.StatusOK, "login.html", gin.H{
		"redirect": redirect,
		"error":    c.Query("error"),
	})
}

func (h *AuthHandlers) LoginPOST(c *gin.Context) {
	// If no users exist, direct to admin setup
	if h.users.IsEmpty() {
		c.Redirect(http.StatusFound, "/admin/setup")
		return
	}
	username := c.PostForm("username")
	password := c.PostForm("password")
	redirect := c.PostForm("redirect")

	if username == "" || password == "" {
		c.HTML(http.StatusBadRequest, "login.html", gin.H{
			"error":    "Username and password are required",
			"redirect": redirect,
		})
		return
	}

	// Validate credentials against user store
	u, exists := h.users.Get(username)
	if !exists || !h.authService.CheckPassword(password, u.PasswordHash) {
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{
			"error":    "Invalid username or password",
			"redirect": redirect,
		})
		return
	}

	// Generate JWT token
	token, err := h.authService.GenerateToken(username)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "login.html", gin.H{
			"error":    "Failed to generate authentication token",
			"redirect": redirect,
		})
		return
	}

	// Set auth cookie with proper SameSite/Secure flags (supports iframe/preview)
	middleware.SetAuthCookie(c, token)

	if redirect == "" || redirect == "/login" || redirect == "/setup" {
		if u.Role == manager.RoleAdmin {
			redirect = "/manager"
		} else {
			redirect = "/dashboard"
		}
	}

	// Redirect to requested page or manager dashboard
	c.Redirect(http.StatusFound, redirect)
}

func (h *AuthHandlers) Logout(c *gin.Context) {
	middleware.ClearAuthCookie(c)
	c.Redirect(http.StatusFound, "/login")
}

// APILogin handles JSON-based authentication requests.
func (h *AuthHandlers) APILogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
		})
		return
	}

	username := middleware.SanitizeString(req.Username)
	password := req.Password

	// Validate credentials
	u, exists := h.users.Get(username)
	if !exists || !h.authService.CheckPassword(password, u.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid username or password",
		})
		return
	}

	// Generate JWT token
	token, err := h.authService.GenerateToken(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate authentication token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  username,
	})
}

// Admin setup pages for first run
func (h *AuthHandlers) AdminSetupGET(c *gin.Context) {
	// If already initialized, go to login
	if !h.users.IsEmpty() {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	c.HTML(http.StatusOK, "admin_setup.html", gin.H{})
}

func (h *AuthHandlers) AdminSetupPOST(c *gin.Context) {
	if !h.users.IsEmpty() {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	password := strings.TrimSpace(c.PostForm("password"))
	confirm := strings.TrimSpace(c.PostForm("confirm"))
	if password == "" || len(password) < 8 {
		c.HTML(http.StatusBadRequest, "admin_setup.html", gin.H{"error": "Password must be at least 8 characters."})
		return
	}
	if password != confirm {
		c.HTML(http.StatusBadRequest, "admin_setup.html", gin.H{"error": "Passwords do not match."})
		return
	}
	hash, err := h.authService.HashPassword(password)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "admin_setup.html", gin.H{"error": "Failed to set password."})
		return
	}
	if _, err := h.users.CreateUser("admin", hash, manager.RoleAdmin); err != nil {
		c.HTML(http.StatusInternalServerError, "admin_setup.html", gin.H{"error": "Failed to create admin user."})
		return
	}
	// Optional viewer auto-creation via environment has been removed.
	// Redirect to login
	c.Redirect(http.StatusFound, "/login")
}

