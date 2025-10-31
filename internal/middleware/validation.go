package middleware

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New()
}

// Input sanitization helpers
func SanitizeString(input string) string {
	// Remove null bytes and control characters except newlines and tabs
	input = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`).ReplaceAllString(input, "")
	// Trim whitespace
	return strings.TrimSpace(input)
}

func SanitizeFilename(input string) string {
	// Remove dangerous characters for filenames
	input = regexp.MustCompile(`[<>:"/\\|?*]`).ReplaceAllString(input, "")
	return SanitizeString(input)
}

// SanitizePath preserves directory separators and normalizes the path while
// removing control characters and trimming whitespace. It intentionally does
// not strip '/' or '\\' so absolute paths remain intact. Use for config fields
// that accept filesystem paths (e.g., SDSM root_path).
func SanitizePath(input string) string {
	// Remove control chars and trim
	cleaned := SanitizeString(input)
	// Normalize path using OS-specific rules; this collapses redundant
	// separators and ".." safely without removing leading separators.
	// Note: we import path/filepath at top of file
	return filepath.Clean(cleaned)
}

func ValidatePort(portStr string) (int, error) {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, gin.Error{Err: err, Type: gin.ErrorTypePublic, Meta: "Port must be between 1 and 65535"}
	}
	return port, nil
}

// Validation middleware
func ValidateJSON(v interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := c.ShouldBindJSON(v); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid JSON format",
				"details": err.Error(),
			})
			c.Abort()
			return
		}

		if err := validate.Struct(v); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Validation failed",
				"details": err.Error(),
			})
			c.Abort()
			return
		}

		c.Set("validated_data", v)
		c.Next()
	}
}

// Form validation helper
func ValidateFormData(c *gin.Context, requiredFields []string) bool {
	for _, field := range requiredFields {
		if strings.TrimSpace(c.PostForm(field)) == "" {
			return false
		}
	}
	return true
}
