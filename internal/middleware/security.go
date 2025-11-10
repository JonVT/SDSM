package middleware

import (
	"net/http"
	"os"
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
func SecurityHeaders() gin.HandlerFunc {
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
		// By default, permit only same-origin embedding. When SDSM_ALLOW_IFRAME=true is set,
		// relax to allow any parent (useful for preview environments). Prefer CSP frame-ancestors
		// and set X-Frame-Options to SAMEORIGIN (not DENY) so our own iframe works.
		allowIFrame := strings.EqualFold(os.Getenv("SDSM_ALLOW_IFRAME"), "true")
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
