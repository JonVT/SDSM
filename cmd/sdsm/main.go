package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"text/template"
	"time"

	"sdsm/internal/handlers"
	"sdsm/internal/manager"
	"sdsm/internal/middleware"

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
}

var app *App

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

	// Initialize application
	app = &App{
		manager:     manager.NewManager(),
		authService: middleware.NewAuthService(),
		wsHub:       middleware.NewHub(),
		rateLimiter: middleware.NewRateLimiter(rate.Every(time.Minute/100), 10),
		tlsEnabled:  envBool(envUseTLS),
		tlsCertPath: os.Getenv(envTLSCert),
		tlsKeyPath:  os.Getenv(envTLSKey),
	}

	if !app.manager.IsActive() {
		log.Fatal("Manager failed to initialize")
	}

	// Start WebSocket hub
	go app.wsHub.Run()

	// Set up Gin with security middleware
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
			log.Fatalf("%s is enabled but %s or %s not provided", envUseTLS, envTLSCert, envTLSKey)
		}
		go func() {
			log.Printf("Starting HTTPS server on port %d", app.manager.Port)
			if err := srv.ListenAndServeTLS(app.tlsCertPath, app.tlsKeyPath); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTPS server failed to start: %v", err)
			}
		}()
	} else {
		go func() {
			log.Printf("Starting server on port %d", app.manager.Port)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server failed to start: %v", err)
			}
		}()
	}

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Shutdown manager first
	app.manager.Shutdown()

	// Stop rate limiter cleanup goroutine
	app.rateLimiter.Stop()

	// Give server 5 seconds to finish handling requests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
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

	// Load templates
	r.SetFuncMap(template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"has": func(slice []string, item string) bool {
			for _, s := range slice {
				if s == item {
					return true
				}
			}
			return false
		},
	})
	r.LoadHTMLGlob("templates/*")
	r.Static("/static", "./static")
	r.StaticFile("/sdsm.png", "./sdsm.png")

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

	// API routes (require token authentication)
	api := r.Group("/api")
	api.Use(app.authService.RequireAPIAuth())
	{
		api.POST("/login", authHandlers.APILogin)
		api.GET("/stats", managerHandlers.APIStats)
		api.GET("/servers", managerHandlers.APIServers)
		api.GET("/manager/status", managerHandlers.APIManagerStatus)
		api.GET("/servers/:server_id/status", managerHandlers.APIServerStatus)
		api.GET("/servers/:server_id/log", managerHandlers.APIServerLog)
		api.POST("/servers/:server_id/start", managerHandlers.APIServerStart)
		api.POST("/servers/:server_id/stop", managerHandlers.APIServerStop)
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
