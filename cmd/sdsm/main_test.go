package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"sdsm/internal/manager"
	"sdsm/internal/middleware"
)

// initMinimalApp initializes the global app with a minimal manager and services for testing.
func initMinimalApp(t *testing.T) {
	t.Helper()
	mgr := manager.NewManager()
	if mgr == nil {
		t.Fatal("manager.NewManager returned nil")
	}
	app = &App{
		manager:     mgr,
		authService: middleware.NewAuthService(),
		wsHub:       middleware.NewHub(nil),
		rateLimiter: middleware.NewRateLimiter(rate.Every(time.Second), 100),
		userStore:   manager.NewUserStore(mgr.Paths),
	}
}

func TestPublicEndpoints(t *testing.T) {
	initMinimalApp(t)
	r := setupRouter()

	// /healthz
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz expected 200, got %d", w.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatalf("/healthz invalid JSON: %v", err)
	}
	if health["status"] != "ok" {
		t.Fatalf("/healthz expected status=ok, got %#v", health)
	}

	// /version
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/version", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/version expected 200, got %d", w.Code)
	}
	var ver map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ver); err != nil {
		t.Fatalf("/version invalid JSON: %v", err)
	}
	if _, ok := ver["version"]; !ok {
		t.Fatalf("/version missing 'version' field")
	}
}

func TestReadyz(t *testing.T) {
	initMinimalApp(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/readyz", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz expected 200 or 503 depending on environment, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("/readyz invalid JSON: %v", err)
	}
	if _, ok := body["ready"]; !ok {
		t.Fatalf("/readyz missing 'ready' field")
	}
}
