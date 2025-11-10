package handlers

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "sdsm/internal/manager"
    "sdsm/internal/middleware"
    "sdsm/internal/utils"

    "github.com/gin-gonic/gin"
)

func setupTestUserStore(t *testing.T) *manager.UserStore {
    t.Helper()
    dir := t.TempDir()
    paths := utils.NewPaths(dir)
    // Ensure parent dirs exist so Save works
    os.MkdirAll(filepath.Dir(paths.UsersFile()), 0o755)
    store := manager.NewUserStore(paths)
    if err := store.Load(); err != nil {
        t.Fatalf("load user store: %v", err)
    }
    return store
}

func buildProfileAPIRouter(t *testing.T, store *manager.UserStore, user, pass string) (*gin.Engine, string) {
    t.Helper()
    gin.SetMode(gin.TestMode)
    auth := middleware.NewAuthService()
    // Create test user
    hash, err := auth.HashPassword(pass)
    if err != nil {
        t.Fatalf("hash: %v", err)
    }
    if _, err := store.CreateUser(user, hash, manager.RoleUser); err != nil {
        t.Fatalf("create user: %v", err)
    }
    token, err := auth.GenerateToken(user)
    if err != nil {
        t.Fatalf("token: %v", err)
    }

    h := NewProfileHandlers(store, auth)
    r := gin.New()
    r.Use(middleware.SecurityHeaders()) // allow API POST
    api := r.Group("/api")
    api.Use(auth.RequireAPIAuth())
    api.POST("/profile/password", h.APIProfileChangePassword)
    return r, token
}

func TestAPIProfilePassword_Success(t *testing.T) {
    store := setupTestUserStore(t)
    r, token := buildProfileAPIRouter(t, store, "alice", "oldpass123")

    body := map[string]string{
        "current_password": "oldpass123",
        "new_password":     "newpass123",
        "confirm_password": "newpass123",
    }
    b, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/api/profile/password", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
    }
}

func TestAPIProfilePassword_WrongCurrent(t *testing.T) {
    store := setupTestUserStore(t)
    r, token := buildProfileAPIRouter(t, store, "bob", "oldpass123")

    body := map[string]string{
        "current_password": "nope",
        "new_password":     "newpass123",
        "confirm_password": "newpass123",
    }
    b, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/api/profile/password", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusUnauthorized {
        t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
    }
}

func TestAPIProfilePassword_Mismatch(t *testing.T) {
    store := setupTestUserStore(t)
    r, token := buildProfileAPIRouter(t, store, "carol", "oldpass123")

    body := map[string]string{
        "current_password": "oldpass123",
        "new_password":     "newpass123",
        "confirm_password": "different",
    }
    b, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/api/profile/password", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
    }
}

func TestAPIProfilePassword_Weak(t *testing.T) {
    store := setupTestUserStore(t)
    r, token := buildProfileAPIRouter(t, store, "dave", "oldpass123")

    body := map[string]string{
        "current_password": "oldpass123",
        "new_password":     "short",
        "confirm_password": "short",
    }
    b, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/api/profile/password", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
    }
}
