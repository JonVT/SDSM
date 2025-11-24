// Package middleware provides authentication helpers and Gin middlewares
// for SDSM's UI and API endpoints.
package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	// JWTSecret is the default HMAC signing secret for auth tokens. Override via environment in production.
	JWTSecret = "your-secret-key-change-in-production"
	// TokenExpiry controls how long issued tokens remain valid.
	TokenExpiry = 24 * time.Hour
	// CookieName is the name of the auth cookie used by the UI and API.
	CookieName = "auth_token"
)

// Claims is the JWT claims payload used for SDSM sessions.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// AuthService generates and validates JWT tokens and provides auth middlewares.
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

// NewAuthService creates an AuthService using the default JWTSecret.
func NewAuthService() *AuthService { return NewAuthServiceWithSecret("") }

// NewAuthServiceWithSecret creates an AuthService using the provided secret or the default when empty.
func NewAuthServiceWithSecret(secret string) *AuthService {
	if strings.TrimSpace(secret) == "" {
		secret = JWTSecret
	}
	return &AuthService{
		secret:      []byte(secret),
		apiFailures: make(map[string]*apiFailure),
	}
}

// HashPassword returns a bcrypt hash for the provided password.
func (a *AuthService) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword verifies a plaintext password against a bcrypt hash.
func (a *AuthService) CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateToken creates a signed JWT for the given username.
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

// ValidateToken parses and validates a JWT, returning its claims on success.
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

// requestIsSecure reports whether the request is secure (direct TLS or X-Forwarded-Proto=https).
func requestIsSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	return false
}

// bearerTokenFromHeader extracts the JWT token from an Authorization header when it
// uses the Bearer scheme. Any other scheme (e.g., Basic) is ignored to avoid
// interfering with reverse proxies that inject their own credentials.
func bearerTokenFromHeader(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if len(header) >= 7 && strings.EqualFold(header[0:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

// forceSecureCookies honors SDSM_COOKIE_FORCE_SECURE=true to always mark cookies Secure.
// CookieOptions controls how cookies are issued by UI/API helpers.
type CookieOptions struct {
	ForceSecure bool
	// SameSite accepts: "lax", "strict", "default", or "none" (case-insensitive). Defaults to "none".
	SameSite string
}

var cookieOptions = CookieOptions{ForceSecure: false, SameSite: "none"}

// SetCookieOptions configures cookie behavior globally for the process.
func SetCookieOptions(opts CookieOptions) { cookieOptions = opts }

func forceSecureCookies() bool { return cookieOptions.ForceSecure }

// cookieShouldBeSecure decides the Secure flag for cookies based on env and request.
func cookieShouldBeSecure(c *gin.Context) bool {
	if forceSecureCookies() {
		return true
	}
	return requestIsSecure(c)
}

// resolveSameSite resolves the SameSite setting from SDSM_COOKIE_SAMESITE; defaults to None.
func resolveSameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(cookieOptions.SameSite)) {
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

// SetAuthCookie sets the auth cookie with flags compatible with iframe scenarios.
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

// ClearAuthCookie clears the auth cookie using the same attributes.
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

// RequireAuth is a UI middleware that enforces authentication via cookie or header.
func (a *AuthService) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try to get token from Authorization header when it carries a Bearer token
		tokenString := bearerTokenFromHeader(c.GetHeader("Authorization"))
		if tokenString == "" {
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

// RequireAPIAuth is an API middleware returning JSON errors instead of redirects.
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

		// Prefer Authorization header bearing a JWT but fall back to cookie for browser requests
		tokenString := bearerTokenFromHeader(c.GetHeader("Authorization"))
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
