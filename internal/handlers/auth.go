package handlers

import (
	"fmt"
	"net/http"
	"os"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"

	"github.com/gin-gonic/gin"
)

type AuthHandlers struct {
	authService *middleware.AuthService
	manager     *manager.Manager
}

type LoginRequest struct {
	Username string `json:"username" binding:"required" validate:"required,min=3,max=50"`
	Password string `json:"password" binding:"required" validate:"required,min=6"`
}

// User credentials (in production, this should be from a database)
var userCredentials = map[string]string{
	"admin": "", // This will be set with hashed password
	"user1": "", // Add more users here
	"user2": "", // Add more users here
}

func NewAuthHandlers(authService *middleware.AuthService, mgr *manager.Manager) *AuthHandlers {
	h := &AuthHandlers{
		authService: authService,
		manager:     mgr,
	}

	// Initialize user passwords
	// Priority: Environment variables > Default passwords
	userPasswords := map[string]string{
		"admin": getEnvOrDefault("SDSM_ADMIN_PASSWORD", "admin123"),
		"user1": getEnvOrDefault("SDSM_USER1_PASSWORD", "password1"),
		"user2": getEnvOrDefault("SDSM_USER2_PASSWORD", "password2"),
	}

	for username, password := range userPasswords {
		hashedPassword, err := authService.HashPassword(password)
		if err != nil {
			// In production, handle this error properly
			panic(fmt.Sprintf("Failed to hash password for user %s: %v", username, err))
		}
		userCredentials[username] = hashedPassword
	}

	return h
}

// Helper function to get environment variable or default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func (h *AuthHandlers) LoginGET(c *gin.Context) {
	// Check if already authenticated
	if token, _ := c.Cookie(middleware.CookieName); token != "" {
		if _, err := h.authService.ValidateToken(token); err == nil {
			c.Redirect(http.StatusFound, "/manager")
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

	// Validate credentials
	hashedPassword, exists := userCredentials[username]
	if !exists || !h.authService.CheckPassword(password, hashedPassword) {
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
		redirect = "/manager"
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
	hashedPassword, exists := userCredentials[username]
	if !exists || !h.authService.CheckPassword(password, hashedPassword) {
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
