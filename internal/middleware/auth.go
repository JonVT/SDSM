package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	JWTSecret   = "your-secret-key-change-in-production" // Should be from environment
	TokenExpiry = 24 * time.Hour
	CookieName  = "auth_token"
)

type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type AuthService struct {
	secret      []byte
	mu          sync.Mutex
	apiFailures map[string]*apiFailure
}

type apiFailure struct {
	count        int
	lastAttempt  time.Time
	lockoutUntil time.Time
}

func NewAuthService() *AuthService {
	return &AuthService{
		secret:      []byte(JWTSecret),
		apiFailures: make(map[string]*apiFailure),
	}
}

func (a *AuthService) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func (a *AuthService) CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func (a *AuthService) GenerateToken(username string) (string, error) {
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(TokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

func (a *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// Helper to detect if current request is effectively HTTPS (behind proxy or direct)
func requestIsSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	return false
}

func forceSecureCookies() bool {
	return strings.EqualFold(os.Getenv("SDSM_COOKIE_FORCE_SECURE"), "true")
}

func cookieShouldBeSecure(c *gin.Context) bool {
	if forceSecureCookies() {
		return true
	}
	return requestIsSecure(c)
}

// Resolve SameSite setting based on env; defaults to SameSiteNone for iframe compatibility
func resolveSameSite() http.SameSite {
	switch strings.ToLower(os.Getenv("SDSM_COOKIE_SAMESITE")) {
	case "lax":
		return http.SameSiteLaxMode
	case "strict":
		return http.SameSiteStrictMode
	case "default":
		return http.SameSiteDefaultMode
	default:
		// default to None to support embedding in preview if served over HTTPS
		return http.SameSiteNoneMode
	}
}

// Sets the auth cookie with appropriate flags for iframe environments
func SetAuthCookie(c *gin.Context, token string) {
	sameSite := resolveSameSite()
	secure := cookieShouldBeSecure(c)
	// SameSite=None requires Secure=true; fallback to Lax if connection isn't secure
	if sameSite == http.SameSiteNoneMode && !secure {
		sameSite = http.SameSiteLaxMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   int(TokenExpiry.Seconds()),
	})
}

// Clears the auth cookie using the same attributes
func ClearAuthCookie(c *gin.Context) {
	sameSite := resolveSameSite()
	secure := cookieShouldBeSecure(c)
	if sameSite == http.SameSiteNoneMode && !secure {
		sameSite = http.SameSiteLaxMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   -1,
	})
}

// Middleware to require authentication
func (a *AuthService) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try to get token from header first
		tokenString := c.GetHeader("Authorization")
		if tokenString != "" {
			// Remove "Bearer " prefix if present
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		} else {
			// Fall back to cookie
			tokenString, _ = c.Cookie(CookieName)
		}

		if tokenString == "" {
			c.Redirect(http.StatusFound, "/login?redirect="+c.Request.URL.Path)
			c.Abort()
			return
		}

		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			c.Redirect(http.StatusFound, "/login?redirect="+c.Request.URL.Path)
			c.Abort()
			return
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

// API Authentication middleware (returns JSON instead of redirect)
func (a *AuthService) RequireAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := a.apiFailureKey(c)
		if retryAfter, locked := a.checkAPILockout(key); locked {
			c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many unauthorized attempts",
				"retry_after": int(retryAfter.Seconds()),
			})
			return
		}

		// Prefer Authorization header but fall back to cookie for browser requests
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			if cookieToken, err := c.Cookie(CookieName); err == nil {
				tokenString = cookieToken
			}
		}

		if tokenString == "" {
			retryAfter, locked := a.recordAPIFailure(key)
			if locked {
				c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":       "Too many unauthorized attempts",
					"retry_after": int(retryAfter.Seconds()),
				})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header or cookie required"})
			return
		}

		tokenString = strings.TrimPrefix(tokenString, "Bearer ")

		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			retryAfter, locked := a.recordAPIFailure(key)
			if locked {
				c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":       "Too many unauthorized attempts",
					"retry_after": int(retryAfter.Seconds()),
				})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		a.clearAPIFailures(key)
		c.Set("username", claims.Username)
		c.Next()
	}
}

func (a *AuthService) apiFailureKey(c *gin.Context) string {
	return c.ClientIP()
}

func (a *AuthService) checkAPILockout(key string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	rec, ok := a.apiFailures[key]
	if !ok {
		return 0, false
	}
	now := time.Now()
	if rec.lockoutUntil.After(now) {
		return rec.lockoutUntil.Sub(now), true
	}
	return 0, false
}

func (a *AuthService) recordAPIFailure(key string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	rec, ok := a.apiFailures[key]
	if !ok {
		rec = &apiFailure{}
		a.apiFailures[key] = rec
	}

	if rec.lockoutUntil.After(now) {
		return rec.lockoutUntil.Sub(now), true
	}

	if now.Sub(rec.lastAttempt) > 5*time.Minute {
		rec.count = 0
	}

	rec.lastAttempt = now
	rec.count++

	if rec.count >= 3 {
		lockout := time.Duration(rec.count) * 15 * time.Second
		if lockout > 2*time.Minute {
			lockout = 2 * time.Minute
		}
		rec.lockoutUntil = now.Add(lockout)
		rec.count = 0
		return lockout, true
	}

	return 0, false
}

func (a *AuthService) clearAPIFailures(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.apiFailures, key)
}
