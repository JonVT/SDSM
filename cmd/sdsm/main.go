package main

import (
	"context"
	"fmt"
	htmltmpl "html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sdsm/internal/handlers"
	"sdsm/internal/manager"
	"sdsm/internal/middleware"
	"sdsm/ui"
	"strconv"
	"strings"
	"syscall"
	"time"

	"io/fs"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type App struct {
	manager     *manager.Manager
	authService *middleware.AuthService
	wsHub       *middleware.Hub
	rateLimiter *middleware.RateLimiter
	tlsEnabled  bool
	tlsCertPath string
	tlsKeyPath  string
	ginLogFile  *os.File
}

var app *App

func logStuff(msg string) {
	if app.manager != nil && app.manager.Log != nil {
		app.manager.Log.Write(msg)
	} else {
		log.Println(msg)
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

const (
	envUseTLS  = "SDSM_USE_TLS"
	envTLSCert = "SDSM_TLS_CERT"
	envTLSKey  = "SDSM_TLS_KEY"
)

func envBool(key string) bool {
	val := os.Getenv(key)
	if val == "" {
		return false
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return parsed
}

func main() {
	// Set Gin mode
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	mgr := manager.NewManager()

	// Initialize application
	app = &App{
		manager:     mgr,
		authService: middleware.NewAuthService(),
		wsHub:       middleware.NewHub(mgr.Log),
		rateLimiter: middleware.NewRateLimiter(rate.Every(time.Minute/100), 10),
		tlsEnabled:  envBool(envUseTLS),
		tlsCertPath: os.Getenv(envTLSCert),
		tlsKeyPath:  os.Getenv(envTLSKey),
	}

	if !app.manager.IsActive() {
		logStuff("Manager failed to initialize")
		os.Exit(1)
	}

	if app.manager.Paths != nil {
		clearLogFile(filepath.Join(app.manager.Paths.LogsDir(), "GIN.log"))
		clearLogFile(app.manager.Paths.UpdateLogFile())
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

	// Start server in a goroutine
	if app.tlsEnabled {
		if app.tlsCertPath == "" || app.tlsKeyPath == "" {
			logStuff(fmt.Sprintf("%s is enabled but %s or %s not provided", envUseTLS, envTLSCert, envTLSKey))
			os.Exit(1)
		}
		go func() {
			logStuff(fmt.Sprintf("Starting HTTPS server on port %d", app.manager.Port))
			if err := srv.ListenAndServeTLS(app.tlsCertPath, app.tlsKeyPath); err != nil && err != http.ErrServerClosed {
				logStuff(fmt.Sprintf("HTTPS server failed to start: %v", err))
				os.Exit(1)
			}
		}()
	} else {
		go func() {
			if app.tlsEnabled {
				fmt.Printf("Starting server at https://localhost:%d\n", app.manager.Port)
			} else {
				fmt.Printf("Starting server at http://localhost:%d\n", app.manager.Port)
			}
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logStuff(fmt.Sprintf("Server failed to start: %v", err))
				os.Exit(1)
			}
		}()
	}

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logStuff("Shutting down server...")

	// Shutdown manager first
	app.manager.Shutdown()

	// Stop rate limiter cleanup goroutine
	app.rateLimiter.Stop()

	// Give server 5 seconds to finish handling requests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logStuff(fmt.Sprintf("Server forced to shutdown: %v", err))
		os.Exit(1)
	}

	logStuff("Server exited")
}

func setupRouter() *gin.Engine {
	r := gin.New()

	// Add recovery middleware
	r.Use(gin.Recovery())

	// Add custom logging middleware
	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format(time.RFC1123),
			param.Method,
			param.Path,
			param.Request.Proto,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	}))

	// Security middleware
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.CORS())

	// Rate limiting - 100 requests per minute per IP
	r.Use(app.rateLimiter.Middleware())

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
	}
	r.SetFuncMap(funcMap)
	// Build a combined template set from embedded assets so both root and subdir files are available by name
	t := htmltmpl.New("").Funcs(funcMap)
	// Parse templates from embedded FS
	t = htmltmpl.Must(t.ParseFS(ui.Assets, "templates/*.html"))
	t = htmltmpl.Must(t.ParseFS(ui.Assets, "templates/*/*.html"))
	// Validate presence of critical templates
	requiredTemplates := []string{"login.html", "manager.html", "frame.html", "dashboard.html", "setup.html", "error.html"}
	for _, name := range requiredTemplates {
		if t.Lookup(name) == nil {
			logStuff(fmt.Sprintf("FATAL: embedded template missing: %s", name))
			os.Exit(1)
		}
	}
	r.SetHTMLTemplate(t)
	// Static assets from embedded FS (no disk fallback)
	staticFS, err := fs.Sub(ui.Assets, "static")
	if err != nil {
		// Fail fast: embedded static must be present
		logStuff(fmt.Sprintf("FATAL: embedded static directory missing: %v", err))
		os.Exit(1)
	}
	// Validate embedded critical static assets exist
	for _, asset := range []string{"sdsm.png", "ui-theme.css", "modern.css"} {
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

	// Initialize handlers
	authHandlers := handlers.NewAuthHandlers(app.authService, app.manager)
	managerHandlers := handlers.NewManagerHandlers(app.manager)

	// Public routes
	r.GET("/", func(c *gin.Context) {
		token, err := c.Cookie(middleware.CookieName)
		if err == nil {
			if _, validateErr := app.authService.ValidateToken(token); validateErr == nil {
				c.Redirect(http.StatusFound, "/manager")
				return
			}
		}
		c.Redirect(http.StatusFound, "/login")
	})

	// Setup routes (no auth required for initial setup)
	setup := r.Group("/setup")
	{
		setup.GET("", managerHandlers.SetupGET)
		setup.GET("/status", managerHandlers.SetupStatusGET)
		setup.POST("/skip", managerHandlers.SetupSkipPOST)
		setup.POST("/install", managerHandlers.SetupInstallPOST)
		setup.POST("/update", managerHandlers.SetupUpdatePOST)
	}

	// Authentication routes
	auth := r.Group("/")
	{
		auth.GET("/login", authHandlers.LoginGET)
		auth.POST("/login", authHandlers.LoginPOST)
		auth.GET("/logout", authHandlers.Logout)
	}

	// API login (public - does not require token)
	r.POST("/api/login", authHandlers.APILogin)

	// API routes (require token authentication)
	api := r.Group("/api")
	api.Use(app.authService.RequireAPIAuth())
	{
		api.GET("/stats", managerHandlers.APIStats)
		api.GET("/servers", managerHandlers.APIServers)
		api.GET("/manager/status", managerHandlers.APIManagerStatus)
		api.GET("/servers/:server_id/status", managerHandlers.APIServerStatus)
		api.GET("/servers/:server_id/log", managerHandlers.APIServerLog)
		api.POST("/servers/:server_id/start", managerHandlers.APIServerStart)
		api.POST("/servers/:server_id/stop", managerHandlers.APIServerStop)
		api.POST("/servers/:server_id/delete", managerHandlers.APIServerDelete)
		api.GET("/start-locations", managerHandlers.APIGetStartLocations)
		api.GET("/start-conditions", managerHandlers.APIGetStartConditions)
	}

	// Protected web routes
	protected := r.Group("/")
	protected.Use(app.authService.RequireAuth())
	protected.Use(func(c *gin.Context) {
		c.Next()
	})
	{
		protected.GET("/dashboard", managerHandlers.Dashboard)
		protected.GET("/frame", managerHandlers.Frame)
		protected.GET("/manager", managerHandlers.ManagerGET)
		protected.POST("/update", managerHandlers.UpdatePOST)
		protected.GET("/update/progress", managerHandlers.UpdateProgressGET)
		protected.GET("/updating", managerHandlers.UpdateStream)
		protected.GET("/logs/updates", managerHandlers.UpdateLogGET)
		protected.GET("/logs/sdsm", managerHandlers.ManagerLogGET)
		protected.GET("/server/new", managerHandlers.NewServerGET)
		protected.POST("/server/new", managerHandlers.NewServerPOST)
		protected.GET("/server/:server_id/status.json", managerHandlers.APIServerStatus)
		protected.GET("/server/:server_id/log", managerHandlers.APIServerLog)
		protected.GET("/server/:server_id", managerHandlers.ServerGET)
		protected.POST("/server/:server_id", managerHandlers.ServerPOST)
		protected.GET("/server/:server_id/progress", managerHandlers.ServerProgressGET)
		protected.GET("/server/:server_id/world-image", managerHandlers.ServerWorldImage)
	}

	// WebSocket endpoint
	r.GET("/ws", app.wsHub.HandleWebSocket())

	return r
}
