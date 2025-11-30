package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltmpl "html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	_ "sdsm/app/backend/internal/cards/dashboard"
	_ "sdsm/app/backend/internal/cards/manager"
	_ "sdsm/app/backend/internal/cards/serverstatus"
	_ "sdsm/app/backend/internal/cards/users"
	"sdsm/app/backend/internal/handlers"
	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/middleware"
	"sdsm/app/backend/internal/models"
	"sdsm/app/backend/internal/utils"
	"sdsm/app/backend/internal/version"
	ui "sdsm/app/frontend"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Build and version metadata now live in sdsm/app/backend/internal/version

type App struct {
	manager     *manager.Manager
	authService *middleware.AuthService
	wsHub       *middleware.Hub
	rateLimiter *middleware.RateLimiter
	userStore   *manager.UserStore
	tlsEnabled  bool
	tlsCertPath string
	tlsKeyPath  string
	ginLogFile  *os.File
}

var app *App

var templateRegistry *htmltmpl.Template

func renderCardTemplate(name string, data interface{}) htmltmpl.HTML {
	name = strings.TrimSpace(name)
	if name == "" || templateRegistry == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := templateRegistry.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("renderCard: failed to render %s: %v", name, err)
		return ""
	}
	return htmltmpl.HTML(buf.String())
}

func logStuff(msg string) {
	if app.manager != nil && app.manager.Log != nil {
		app.manager.Log.Write(msg)
	} else {
		// Fallback: write to default SDSM log instead of stdout
		utils.NewLogger("").Write(msg)
	}
}

func clearLogFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logStuff(fmt.Sprintf("Failed to ensure log directory for %s: %v", path, err))
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		logStuff(fmt.Sprintf("Failed to clear log file %s: %v", path, err))
		return
	}
	_ = file.Close()
}

// managerLogWriter adapts Manager.Log to io.Writer for frameworks like Gin.
type managerLogWriter struct{ mgr *manager.Manager }

func (w managerLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	logStuff(msg)
	return len(p), nil
}

// TLS settings are now read from sdsm.config (manager fields).

func main() {
	// Always run Gin in release mode; debugging is controlled elsewhere via logs.
	gin.SetMode(gin.ReleaseMode)

	// Parse CLI flags: --config/-c <path>, --background
	var configPath string
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch arg {
		case "--config", "-c":
			if i+1 < len(os.Args) {
				configPath = strings.TrimSpace(os.Args[i+1])
				i++
			}
		}
	}

	mgr := manager.NewManagerWithConfig(configPath)

	// On Windows with tray enabled, spawn a detached background instance so the
	// launching console returns immediately. Use env guard to prevent infinite spawning.
	if runtime.GOOS == "windows" && mgr != nil && mgr.TrayEnabled {
		if spawnDetachedIfNeeded(mgr.TrayEnabled) {
			// Parent exits; background child continues
			return
		}
	}

	// Initialize application
	app = &App{
		manager:     mgr,
		authService: middleware.NewAuthServiceWithSecret(strings.TrimSpace(mgr.JWTSecret)),
		wsHub:       middleware.NewHub(mgr.Log),
		rateLimiter: middleware.NewRateLimiter(rate.Every(time.Minute/100), 10),
		userStore:   manager.NewUserStore(mgr.Paths),
	}

	// Configure cookie settings from manager config
	middleware.SetCookieOptions(middleware.CookieOptions{ForceSecure: mgr != nil && mgr.CookieForceSecure, SameSite: strings.TrimSpace(mgr.CookieSameSite)})

	// If we are the detached/background process, hide the console window if present
	if runtime.GOOS == "windows" && mgr != nil && mgr.TrayEnabled {
		hideConsoleWindow()
	}

	// Apply TLS settings from config; resolve relative paths against the configured root
	app.tlsEnabled = app.manager.TLSEnabled
	app.tlsCertPath = app.manager.TLSCertPath
	app.tlsKeyPath = app.manager.TLSKeyPath
	resolveTLSPath := func(p string) string {
		p = strings.TrimSpace(p)
		if p == "" {
			return p
		}
		if filepath.IsAbs(p) {
			return p
		}
		if app.manager != nil && app.manager.Paths != nil {
			return filepath.Join(app.manager.Paths.RootPath, p)
		}
		return p
	}
	app.tlsCertPath = resolveTLSPath(app.tlsCertPath)
	app.tlsKeyPath = resolveTLSPath(app.tlsKeyPath)

	// Ensure config directories exist and load users
	if app.manager.Paths != nil {
		_ = os.MkdirAll(app.manager.Paths.ConfigDir(), 0o755)
	}
	_ = app.userStore.Load()
	// Log diagnostics to help when UI shows admin setup unexpectedly
	if app.userStore.IsEmpty() {
		cfg := strings.TrimSpace(app.manager.ConfigFile)
		root := ""
		if app.manager.Paths != nil {
			root = strings.TrimSpace(app.manager.Paths.RootPath)
		}
		path := app.userStore.Path()
		exists := "missing"
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			exists = "present"
		}
		logStuff(fmt.Sprintf("User store appears empty. Config=%s | root_path=%s | users.json=%s (%s)", cfg, root, path, exists))
		logStuff("If you previously had accounts, copy your old users.json into the current root_path/config/users.json, or start SDSM with --config pointing to your original sdsm.config.")
	}

	if !app.manager.IsActive() {
		logStuff("Manager failed to initialize")
		os.Exit(1)
	}

	app.manager.StartTelemetryMonitor()

	if app.manager.Paths != nil {
		clearLogFile(filepath.Join(app.manager.Paths.LogsDir(), "GIN.log"))
	}

	// Start WebSocket hub
	go app.wsHub.Run()

	// Route Gin logs to dedicated GIN.log file (fallback to manager log on error)
	if app.manager != nil && app.manager.Paths != nil {
		if err := os.MkdirAll(app.manager.Paths.LogsDir(), 0o755); err != nil {
			logStuff(fmt.Sprintf("Failed to ensure logs directory: %v", err))
		} else {
			ginLogPath := filepath.Join(app.manager.Paths.LogsDir(), "GIN.log")
			file, err := os.OpenFile(ginLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				logStuff(fmt.Sprintf("Failed to open Gin log file: %v", err))
			} else {
				app.ginLogFile = file
				gin.DefaultWriter = file
				gin.DefaultErrorWriter = file
			}
		}
	}
	if app.ginLogFile == nil && app.manager != nil && app.manager.Log != nil {
		gin.DefaultWriter = managerLogWriter{mgr: app.manager}
		gin.DefaultErrorWriter = managerLogWriter{mgr: app.manager}
	}
	if app.ginLogFile != nil {
		defer app.ginLogFile.Close()
	}
	r := setupRouter()

	// Set up graceful shutdown
	srv := &http.Server{
		Addr:           ":" + strconv.Itoa(app.manager.Port),
		Handler:        r,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	// Route standard library HTTP server errors (including TLS handshake errors)
	// into sdsm.log instead of stderr/stdout.
	srv.ErrorLog = log.New(managerLogWriter{mgr: app.manager}, "", 0)

	// Prepare to start server; actual start may differ if tray must run on main thread (Windows requirement for systray)
	certMissing := strings.TrimSpace(app.tlsCertPath) == ""
	keyMissing := strings.TrimSpace(app.tlsKeyPath) == ""
	useTLS := app.tlsEnabled && !certMissing && !keyMissing
	if app.tlsEnabled && !useTLS {
		logStuff("TLS enabled but certificate or key missing; falling back to HTTP")
		app.tlsEnabled = false
		if app.manager != nil {
			app.manager.TLSEnabled = false
			app.manager.Save()
		}
	}

	startServer := func() {
		if useTLS {
			logStuff(fmt.Sprintf("Starting HTTPS server on port %d", app.manager.Port))
			if err := srv.ListenAndServeTLS(app.tlsCertPath, app.tlsKeyPath); err != nil && err != http.ErrServerClosed {
				logStuff(fmt.Sprintf("HTTPS server failed to start: %v", err))
				os.Exit(1)
			}
		} else {
			logStuff(fmt.Sprintf("Starting HTTP server on port %d", app.manager.Port))
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logStuff(fmt.Sprintf("Server failed to start: %v", err))
				os.Exit(1)
			}
		}
	}

	// Windows tray integration (configurable)
	// For non-Windows platforms or when tray disabled, use nil channel so select ignores tray exit.
	var trayDone chan struct{}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	if app.manager.TrayEnabled && runtime.GOOS == "windows" {
		trayDone = make(chan struct{})
		go startServer() // run server in background
		// Start manager TCP port forwarding if enabled
		app.manager.StartManagerPortForwarding()
		go func() { // forward OS signals to tray exit
			<-quit
			logStuff("Shutdown signal received")
			trayQuit()
		}()
		// run tray on main thread (blocks until tray exit)
		startTray(app, srv, trayDone)
		logStuff("Tray exit requested")
	} else {
		trayDone = nil
		go startServer()
		// Start manager TCP port forwarding if enabled
		app.manager.StartManagerPortForwarding()
		// Block on OS signal only (trayDone nil ignored)
		<-quit
		logStuff("Shutdown signal received")
	}

	// Gracefully stop HTTP server (allow in-flight requests up to 5s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := srv.Shutdown(ctx); err != nil {
		logStuff(fmt.Sprintf("HTTP server shutdown error: %v", err))
	}
	cancel()

	// Stop manager TCP port forwarding and remove mapping
	if app != nil && app.manager != nil {
		app.manager.StopManagerPortForwarding()
	}

	// Exit manager, stopping servers unless detached mode keeps them running
	if app.manager != nil {
		app.manager.StopTelemetryMonitor()
		stopServers := !app.manager.DetachedServers
		app.manager.ExitDetached(stopServers)
	}

	logStuff("Server exited")
}

func setupRouter() *gin.Engine {
	r := gin.New()

	// Add recovery middleware
	r.Use(gin.Recovery())

	// Add filtered logging middleware to reduce noise in GIN.log
	r.Use(middleware.RequestLoggerWithOptions(app != nil && app.manager != nil && app.manager.VerboseHTTP))

	// Security middleware
	allowIFrame := false
	if app != nil && app.manager != nil {
		allowIFrame = app.manager.AllowIFrame
	}
	r.Use(middleware.SecurityHeadersWithOptions(allowIFrame))
	r.Use(middleware.CORS())

	// Rate limiting - 100 requests per minute per IP
	r.Use(app.rateLimiter.Middleware())

	// Initialize handlers first, so they are available for all route registrations.
	authHandlers := handlers.NewAuthHandlers(app.authService, app.manager, app.userStore)
	userHandlers := handlers.NewUserHandlers(app.userStore, app.authService, app.manager.Log, app.manager)
	profileHandlers := handlers.NewProfileHandlers(app.userStore, app.authService)
	managerHandlers := handlers.NewManagerHandlersWithHub(app.manager, app.userStore, app.wsHub)
	// Wire realtime broadcast for servers that are attached on startup (detached mode)
	if app != nil && app.manager != nil {
		app.manager.OnServerAttached = func(s *models.Server) {
			if s == nil {
				return
			}
			managerHandlers.BroadcastStatusAndStats(s)
		}
	}

	updateHandler := func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			acceptsJSON := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json")
			ajax := c.GetHeader("HX-Request") == "true" || strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")
			if acceptsJSON || ajax {
				handlers.ToastError(c, "Permission Denied", "Admin privileges required.")
				c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
			} else {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
			}
			return
		}
		managerHandlers.UpdatePOST(c)
	}

	// Load templates from embedded filesystem
	funcMap := htmltmpl.FuncMap{
		"add": func(a, b int) int { return a + b },
		"has": func(slice []string, item string) bool {
			for _, s := range slice {
				if s == item {
					return true
				}
			}
			return false
		},
		// dict builds a map for passing multiple values to partial templates.
		"dict": func(values ...interface{}) map[string]interface{} {
			m := make(map[string]interface{})
			for i := 0; i+1 < len(values); i += 2 {
				key, _ := values[i].(string)
				m[key] = values[i+1]
			}
			return m
		},
		// initials returns up to the first two runes of a string in uppercase for avatar badges
		"initials": func(s string) string {
			s = strings.TrimSpace(s)
			if s == "" {
				return "?"
			}
			rs := []rune(s)
			if len(rs) == 1 {
				return strings.ToUpper(string(rs))
			}
			return strings.ToUpper(string(rs[0])) + strings.ToUpper(string(rs[1]))
		},
		"appVersion": func() string { return version.Version },
		"buildTime":  func() string { return version.String() },
		"toJSON": func(value interface{}) htmltmpl.JS {
			data, err := json.Marshal(value)
			if err != nil {
				return htmltmpl.JS("null")
			}
			return htmltmpl.JS(string(data))
		},
		"renderCard": renderCardTemplate,
	}
	r.SetFuncMap(funcMap)

	// Load templates from embedded filesystem using glob patterns for each screen/card bucket.
	t, err := htmltmpl.New("").Funcs(funcMap).ParseFS(
		ui.Assets,
		"templates/screens/frame/*.html",
		"templates/screens/frame/partials/*.html",
		"templates/screens/dashboard/*.html",
		"templates/screens/manager/*.html",
		"templates/screens/serverstatus/*.html",
		"templates/screens/users/*.html",
		"templates/cards/dashboard/*.html",
		"templates/cards/manager/*.html",
		"templates/cards/serverstatus/*.html",
		"templates/cards/users/*.html",
	)
	if err != nil {
		log.Fatalf("FATAL: failed to parse templates: %v", err)
	}
	templateRegistry = t
	r.SetHTMLTemplate(t)

	// Static assets from embedded FS (no disk fallback)
	staticFS, err := fs.Sub(ui.Assets, "static")
	if err != nil {
		// Fail fast: embedded static must be present
		logStuff(fmt.Sprintf("FATAL: embedded static directory missing: %v", err))
		os.Exit(1)
	}
	// Validate embedded critical static assets exist
	for _, asset := range []string{"sdsm.png", "css/ui-theme.css", "css/modern.css", "js/common/app.js"} {
		if f, openErr := staticFS.Open(asset); openErr != nil {
			logStuff(fmt.Sprintf("FATAL: embedded static asset missing: %s (%v)", asset, openErr))
			os.Exit(1)
		} else {
			_ = f.Close()
		}
	}
	r.StaticFS("/static", http.FS(staticFS))
	// Serve icon from embedded FS
	r.GET("/sdsm.png", func(c *gin.Context) {
		c.FileFromFS("sdsm.png", http.FS(staticFS))
	})
	// sdsm icon served above from embedded FS

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Readiness probe: reports when the manager is active and critical components
	// are initialized. Returns 200 when ready; 503 with a reason otherwise.
	r.GET("/readyz", readyHandler)

	// Public version endpoint with build metadata
	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version": version.Version,
			"commit":  version.Commit,
			"date":    version.Date,
			"dirty":   version.Dirty,
			"display": version.String(),
		})
	})

	termsPageHandler := func(c *gin.Context) {
		c.HTML(http.StatusOK, "terms.html", gin.H{"title": "Terms of Use", "appVersion": version.Version, "buildTime": version.String()})
	}
	licensePageHandler := func(c *gin.Context) {
		c.HTML(http.StatusOK, "license.html", gin.H{"title": "License", "appVersion": version.Version, "buildTime": version.String()})
	}
	privacyPageHandler := func(c *gin.Context) {
		c.HTML(http.StatusOK, "privacy.html", gin.H{"title": "Privacy", "appVersion": version.Version, "buildTime": version.String()})
	}

	// Public policy pages
	r.GET("/terms", termsPageHandler)
	r.GET("/license", licensePageHandler)
	r.GET("/privacy", privacyPageHandler)

	// Authentication routes
	auth := r.Group("/")
	{
		auth.GET("/login", authHandlers.LoginGET)
		auth.POST("/login", authHandlers.LoginPOST)
		auth.GET("/logout", authHandlers.Logout)
	}

	// Root route: redirect to appropriate landing page
	r.GET("/", func(c *gin.Context) {
		// If first run (no users), send to admin setup
		if app.userStore.IsEmpty() {
			c.Redirect(http.StatusFound, "/admin/setup")
			return
		}
		// Try auth cookie; if valid, route to dashboard
		if token, _ := c.Cookie(middleware.CookieName); token != "" {
			if _, err := app.authService.ValidateToken(token); err == nil {
				c.Redirect(http.StatusFound, "/dashboard")
				return
			}
		}
		// Not authenticated -> login
		c.Redirect(http.StatusFound, "/login")
	})

	// First-run admin setup routes (public until initialized)
	r.GET("/admin/setup", authHandlers.AdminSetupGET)
	r.POST("/admin/setup", authHandlers.AdminSetupPOST)

	// API login (public - does not require token)
	r.POST("/api/login", authHandlers.APILogin)

	// API routes (require token authentication)
	api := r.Group("/api")
	api.Use(app.authService.RequireAPIAuth())
	// Attach role and perform admin safety net via shared middleware
	api.Use(middleware.EnsureRoleContext(app.userStore, app.manager.Log, "API"))
	{
		// Report a bug to configured SDSM Discord webhook (admin only)
		api.POST("/bug-report", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.BugReportPOST(c)
		})
		// Profile self-service password change (JSON only)
		api.POST("/profile/password", profileHandlers.APIProfileChangePassword)
		// Simple refresh endpoint used by UI header buttons to trigger htmx 'refresh' events.
		api.GET("/refresh", func(c *gin.Context) {
			// Return minimal JSON; htmx button uses hx-swap="none" so body is ignored.
			handlers.ToastSuccess(c, "Refreshed", "Data refreshed")
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		api.GET("/stats", managerHandlers.APIStats)
		api.GET("/servers", managerHandlers.APIServers)
		api.POST("/servers", func(c *gin.Context) {
			// Admin only create
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServersCreate(c)
		})
		// Create from Save (multipart .save upload)
		api.POST("/servers/create-from-save", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServersCreateFromSave(c)
		})
		// Analyze Save (multipart .save upload) - returns world and world file name
		api.POST("/servers/analyze-save", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServersAnalyzeSave(c)
		})
		api.GET("/manager/status", managerHandlers.APIManagerStatus)
		// Aggregated port forwarding metrics (admin)
		api.GET("/metrics/port-forward", managerHandlers.APIPortForwardMetrics)
		// Manager networking diagnostics
		api.GET("/manager/test-port", managerHandlers.APIManagerTestPort)
		api.GET("/servers/:server_id/status", managerHandlers.APIServerStatus)
		api.GET("/servers/:server_id/progress", managerHandlers.ServerProgressGET)
		api.GET("/servers/:server_id/saves", managerHandlers.APIServerSaves)
		api.DELETE("/servers/:server_id/saves", managerHandlers.APIServerSaveDelete)
		api.GET("/servers/:server_id/logs", managerHandlers.APIServerLogsList)
		api.GET("/servers/:server_id/log", managerHandlers.APIServerLog)
		api.GET("/servers/:server_id/log/tail", managerHandlers.APIServerLogTail)
		api.GET("/servers/:server_id/log/download", managerHandlers.APIServerLogDownload)
		api.GET("/servers/:server_id/world/saves", managerHandlers.APIServerWorldSaves)
		api.GET("/servers/:server_id/world/download", managerHandlers.APIServerWorldDownload)
		api.POST("/servers/:server_id/log/clear", managerHandlers.APIServerLogClear)
		api.POST("/servers/:server_id/start", managerHandlers.APIServerStart)
		api.POST("/servers/:server_id/stop", managerHandlers.APIServerStop)
		api.POST("/servers/:server_id/pause", managerHandlers.APIServerPause)
		api.POST("/servers/:server_id/resume", managerHandlers.APIServerResume)
		// legacy generic command removed; use explicit endpoints below
		api.POST("/servers/:server_id/chat", managerHandlers.APIServerChat)
		api.POST("/servers/:server_id/console", managerHandlers.APIServerConsole)
		api.GET("/servers/:server_id/scon/health", managerHandlers.APIServerSCONHealth)
		api.POST("/servers/:server_id/save", managerHandlers.APIServerSave)
		api.POST("/servers/:server_id/quicksave", managerHandlers.APIServerQuickSave)
		api.POST("/servers/:server_id/save-as", managerHandlers.APIServerSaveAs)
		api.POST("/servers/:server_id/load", managerHandlers.APIServerLoad)
		api.POST("/servers/:server_id/storm", managerHandlers.APIServerStorm)
		api.POST("/servers/:server_id/cleanup", managerHandlers.APIServerCleanup)
		api.POST("/servers/:server_id/kick", managerHandlers.APIServerKick)
		api.POST("/servers/:server_id/restart", managerHandlers.APIServerRestart)
		api.POST("/servers/:server_id/ban", managerHandlers.APIServerBan)
		api.POST("/servers/:server_id/unban", managerHandlers.APIServerUnban)
		api.POST("/servers/:server_id/player-saves/exclude", managerHandlers.APIServerPlayerSaveExclude)
		api.POST("/servers/:server_id/player-saves/delete-all", managerHandlers.APIServerPlayerSaveDeleteAll)
		api.POST("/servers/:server_id/settings", managerHandlers.APIServerUpdateSettings)
		api.POST("/servers/:server_id/rename", managerHandlers.APIServerRename)
		api.GET("/servers/:server_id/settings/attach-defaults", managerHandlers.APIServerAttachDefaults)
		api.POST("/servers/:server_id/language", managerHandlers.APIServerSetLanguage)
		api.POST("/servers/:server_id/update-server", managerHandlers.APIServerUpdateServerFiles)
		api.POST("/servers/:server_id/reinstall", managerHandlers.APIServerReinstall)
		// Admin-only delete via API
		api.POST("/servers/:server_id/delete", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServerDelete(c)
		})
		// Start all servers (admin)
		api.POST("/servers/start-all", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServersStartAll(c)
		})
		// Stop all servers (admin)
		api.POST("/servers/stop-all", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.APIServersStopAll(c)
		})
		api.GET("/start-locations", managerHandlers.APIGetStartLocations)
		api.GET("/start-conditions", managerHandlers.APIGetStartConditions)
		// Async manager versions (latest/deployed) for faster initial manager page load
		api.GET("/manager/versions", managerHandlers.ManagerVersionsGET)
		// Global manager logs APIs
		api.GET("/manager/logs", managerHandlers.APIManagerLogsList)
		api.GET("/manager/log/tail", managerHandlers.APIManagerLogTail)
		api.POST("/manager/log/clear", managerHandlers.APIManagerLogClear)
		api.GET("/manager/log/download", managerHandlers.APIManagerLogDownload)
		api.GET("/paths/browse", managerHandlers.APIPathBrowser)
		api.POST("/manager/update", updateHandler)

		// Admin-only user management API
		api.GET("/users", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersList(c)
		})
		api.POST("/users", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersCreate(c)
		})
		api.PATCH("/users/:username/role", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersSetRole(c)
		})
		api.POST("/users/:username/reset-password", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersResetPassword(c)
		})
		api.DELETE("/users/:username", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersDelete(c)
		})
		api.GET("/users/:username/assignments", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersGetAssignments(c)
		})
		api.POST("/users/:username/assignments", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			userHandlers.APIUsersSetAssignments(c)
		})
	}

	managerPageHandler := func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
			return
		}
		managerHandlers.ManagerGET(c)
	}
	usersPageHandler := func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
			return
		}
		userHandlers.UsersGET(c)
	}
	newServerPageHandler := func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required."})
			return
		}
		managerHandlers.NewServerGET(c)
	}

	// Protected web routes
	protected := r.Group("/")
	protected.Use(app.authService.RequireAuth())
	// Attach user role to context for downstream checks via shared middleware
	protected.Use(middleware.EnsureRoleContext(app.userStore, app.manager.Log, "UI"))
	{
		// Setup pages (admin only)
		protected.GET("/setup", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
				return
			}
			managerHandlers.SetupGET(c)
		})
		protected.POST("/setup/skip", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
				return
			}
			managerHandlers.SetupSkipPOST(c)
		})
		protected.POST("/setup/install", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
				return
			}
			managerHandlers.SetupInstallPOST(c)
		})
		protected.GET("/setup/status", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.SetupStatusGET(c)
		})
		protected.GET("/setup/progress", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.SetupProgressGET(c)
		})
		protected.POST("/setup/update", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
				return
			}
			managerHandlers.SetupUpdatePOST(c)
		})
		protected.GET("/dashboard", managerHandlers.Dashboard)
		protected.GET("/frame", managerHandlers.Frame)
		// Help pages
		protected.GET("/help/tokens", managerHandlers.TokensHelpGET)
		protected.GET("/help/commands", managerHandlers.CommandsHelpGET)
		protected.GET("/manager", managerPageHandler)
		protected.GET("/manager/cards/:card_id", managerHandlers.ManagerCardGET)
		protected.GET("/dashboard/cards/:card_id", managerHandlers.DashboardCardGET)
		// Profile page for self-service password change
		protected.GET("/profile", profileHandlers.ProfileGET)
		// Debug endpoint to verify identity and role
		protected.GET("/whoami", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"username": c.GetString("username"),
				"role":     c.GetString("role"),
			})
		})
		// Admin-only user management
		protected.GET("/users", usersPageHandler)
		protected.GET("/users/cards/:card_id", userHandlers.UsersCardGET)
		// Users POST removed; UI uses /api endpoints.
		protected.POST("/update", updateHandler)
		protected.POST("/manager/update", updateHandler)
		// Graceful shutdown with optional server stop based on detached mode
		protected.POST("/shutdown", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required.", "username": c.GetString("username"), "role": c.GetString("role")})
				return
			}
			stop := strings.TrimSpace(c.PostForm("stop_servers")) == "1"
			go app.manager.ExitDetached(stop)
			handlers.ToastWarn(c, "Shutdown Initiated", "SDSM is shutting down...")
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		protected.GET("/update/progress", managerHandlers.UpdateProgressGET)
		protected.GET("/updating", managerHandlers.UpdateStream)
		protected.GET("/logs/updates", managerHandlers.UpdateLogGET)
		protected.GET("/logs/sdsm", managerHandlers.ManagerLogGET)
		// Admin-only: server creation
		protected.GET("/server/new", newServerPageHandler)
		// /server/new POST removed; creation flows moved to /api/servers
		protected.GET("/server/:server_id/status.json", managerHandlers.APIServerStatus)
		protected.GET("/server/:server_id/clients", managerHandlers.ServerClientsGET)
		protected.GET("/server/:server_id/log", managerHandlers.APIServerLog)
		protected.GET("/server/:server_id", managerHandlers.ServerGET)
		protected.GET("/server/:server_id/cards/:card_id", managerHandlers.ServerCardGET)
		// /server/:server_id POST removed; actions use /api/servers/:id/*
		protected.GET("/server/:server_id/progress", managerHandlers.ServerProgressGET)
		protected.GET("/server/:server_id/world-image", managerHandlers.ServerWorldImage)
	}

	pagesAPI := r.Group("/api/pages")
	pagesAPI.Use(app.authService.RequireAuth())
	pagesAPI.Use(middleware.EnsureRoleContext(app.userStore, app.manager.Log, "UI-Pages"))
	{
		pagesAPI.GET("/dashboard", managerHandlers.Dashboard)
		pagesAPI.GET("/manager", managerPageHandler)
		pagesAPI.GET("/users", usersPageHandler)
		pagesAPI.GET("/profile", profileHandlers.ProfileGET)
		pagesAPI.GET("/help/tokens", managerHandlers.TokensHelpGET)
		pagesAPI.GET("/help/commands", managerHandlers.CommandsHelpGET)
		pagesAPI.GET("/server/new", newServerPageHandler)
		pagesAPI.GET("/server/:server_id", managerHandlers.ServerGET)
		pagesAPI.GET("/license", licensePageHandler)
		pagesAPI.GET("/privacy", privacyPageHandler)
		pagesAPI.GET("/terms", termsPageHandler)
	}

	// WebSocket endpoint
	r.GET("/ws", app.wsHub.HandleWebSocket())

	return r
}

// startTray provides a Windows system tray icon allowing quick access and exit.
// On non-Windows platforms it returns immediately.
// startTray is implemented in platform-specific files.

// readyHandler is registered at /readyz. Split into a function to avoid bizarre
// issues where inlined handlers might be optimized away in rare build setups.
func readyHandler(c *gin.Context) {
	ready := app != nil && app.manager != nil && app.manager.IsActive()
	missing := []string{}
	if ready {
		if len(app.manager.MissingComponents) > 0 {
			ready = false
			missing = append(missing, app.manager.MissingComponents...)
		}
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{
		"ready":   ready,
		"missing": missing,
	})
}
