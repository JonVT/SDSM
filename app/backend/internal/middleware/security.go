package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Rate limiter middleware
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	stopCh   chan struct{}
}

func NewRateLimiter(rps rate.Limit, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rps,
		burst:    burst,
		stopCh:   make(chan struct{}),
	}
}

func (rl *RateLimiter) getLimiter(clientIP string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[clientIP]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-check pattern
		if limiter, exists := rl.limiters[clientIP]; exists {
			rl.mu.Unlock()
			return limiter
		}

		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.limiters[clientIP] = limiter
		rl.mu.Unlock()
	}

	return limiter
}

func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	// Clean up old limiters periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.mu.Lock()
				// Remove limiters that haven't been active
				for ip := range rl.limiters {
					delete(rl.limiters, ip)
				}
				rl.mu.Unlock()
			case <-rl.stopCh:
				return
			}
		}
	}()

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/static/") ||
			path == "/sdsm.png" ||
			path == "/favicon.ico" ||
			path == "/healthz" ||
			path == "/readyz" ||
			path == "/version" {
			c.Next()
			return
		}

		if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			c.Next()
			return
		}

		limiter := rl.getLimiter(c.ClientIP())
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// Security headers middleware
// SecurityHeadersWithOptions returns a middleware that sets security headers.
// If allowIFrame is true, frame-ancestors is relaxed to allow any parent; otherwise same-origin only.
func SecurityHeadersWithOptions(allowIFrame bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Enforce API-only mutation: block POSTs to non-API paths except explicit allowlist.
		// This is a security hardening layer; legacy HTML form endpoints have been disabled,
		// but this prevents accidental reintroduction or template drift.
		if c.Request.Method == http.MethodPost {
			path := c.Request.URL.Path
			// Allowlist: API endpoints, authentication, setup, update, shutdown
			allowed := false
			if strings.HasPrefix(path, "/api/") || path == "/api/login" {
				allowed = true
			} else {
				switch path {
				case "/login", "/admin/setup", "/setup/skip", "/setup/install", "/setup/update", "/shutdown", "/update", "/profile":
					// /profile POST currently returns 410 Gone but permit request to reach handler for backward compatibility message.
					allowed = true
				default:
					// Disallow any other POST to non-API path
				}
			}
			if !allowed {
				c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"error": "POST not allowed on non-API path", "path": path})
				return
			}
		}
		// Prevent XSS attacks
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")

		// HSTS header for HTTPS
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Allow embedding in iframe: our app uses a same-origin iframe for the content shell.
		// By default, permit only same-origin embedding. When allowIFrame is true, relax to allow any parent
		// (useful for preview environments). Prefer CSP frame-ancestors and set X-Frame-Options to SAMEORIGIN
		// (not DENY) so our own iframe works.
		if allowIFrame {
			// Allow any parent via CSP and omit X-Frame-Options (ALLOW-FROM is deprecated and unsupported by most browsers)
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' unpkg.com; style-src 'self' 'unsafe-inline' fonts.googleapis.com; font-src 'self' fonts.gstatic.com; img-src 'self' data: www.stationeers.net; frame-ancestors *;")
		} else {
			// Same-origin embedding only
			c.Header("X-Frame-Options", "SAMEORIGIN")
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' unpkg.com; style-src 'self' 'unsafe-inline' fonts.googleapis.com; font-src 'self' fonts.gstatic.com; img-src 'self' data: www.stationeers.net; frame-ancestors 'self';")
		}

		// Prevent MIME type sniffing
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		c.Next()
	}
}

// SecurityHeaders is the default middleware (same-origin iframe policy).
func SecurityHeaders() gin.HandlerFunc { return SecurityHeadersWithOptions(false) }

// RequestLogger provides a filtered request logging middleware reducing noise in GIN.log.
// It suppresses logs for health probes, readiness checks, version endpoint, static assets,
// favicon, websocket upgrades, and any successful (2xx/3xx) responses to lightweight endpoints.
// noisy=true forces full logging (similar to default gin logger).
func RequestLoggerWithOptions(noisy bool) gin.HandlerFunc {
	// Paths to suppress entirely unless verbose
	suppressedPrefixes := []string{"/static/"}
	suppressedExact := map[string]struct{}{
		"/healthz":     {},
		"/readyz":      {},
		"/version":     {},
		"/favicon.ico": {},
		"/sdsm.png":    {},
	}
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method
		// Process request first
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()

		if !noisy {
			// Skip websocket (will generally be upgraded and noisy if logged each poll)
			if strings.HasPrefix(strings.ToLower(c.GetHeader("Upgrade")), "websocket") {
				return
			}
			if _, ok := suppressedExact[path]; ok {
				return
			}
			for _, p := range suppressedPrefixes {
				if strings.HasPrefix(path, p) {
					return
				}
			}
			// Suppress successful GETs to lightweight endpoints
			if method == http.MethodGet && status < 400 {
				// Allow errors through for diagnostics
				return
			}
		}

		// Log line format (IP - time method path proto status latency ua err)
		clientIP := c.ClientIP()
		ua := c.Request.UserAgent()
		errMsg := c.Errors.ByType(gin.ErrorTypePrivate).String()
		line := fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\n", clientIP, time.Now().Format(time.RFC1123), method, path, c.Request.Proto, status, latency, ua, strings.TrimSpace(errMsg))
		// Use gin DefaultWriter (already redirected to GIN.log) – fallback safe.
		_, _ = gin.DefaultWriter.Write([]byte(line))
	}
}

// RequestLogger is the default logger with noise reduction.
func RequestLogger() gin.HandlerFunc { return RequestLoggerWithOptions(false) }

// CORS middleware
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		// Allow standard verbs + PATCH (role updates) without duplicate header assignment
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
