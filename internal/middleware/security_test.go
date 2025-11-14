package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPostGuardBlocksNonAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	// A generic handler that would succeed if reached
	r.POST("/foo", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodPost, "/foo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for non-API POST, got %d", w.Code)
	}
}

func TestPostGuardAllowsAPIAndAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	// API path should be allowed
	r.POST("/api/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	// Allowlist path should be allowed to reach handler
	r.POST("/login", func(c *gin.Context) { c.JSON(http.StatusTeapot, gin.H{"ok": true}) })

	// API POST
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected API POST to succeed (200), got %d", w.Code)
	}

	// Allowlisted POST
	req2 := httptest.NewRequest(http.MethodPost, "/login", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTeapot { // confirmed our handler ran
		t.Fatalf("expected allowlisted POST to reach handler (418), got %d", w2.Code)
	}
}
