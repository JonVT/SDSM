package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/utils"
)

// EnsureRoleContext attaches the user's role to the Gin context and applies a
// safety net: when no admin users exist but a user named "admin" is authenticated,
// promote that user to admin to prevent lockout. A log message is written when
// promotion occurs. The returned middleware expects that an upstream auth middleware
// already set "username" in the context.
func EnsureRoleContext(users *manager.UserStore, logger *utils.Logger, contextLabel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		usernameVal, _ := c.Get("username")
		role := string(manager.RoleUser)
		if uname, ok := usernameVal.(string); ok {
			if u, ok := users.Get(uname); ok {
				if string(u.Role) != "" {
					role = string(u.Role)
				}
			}
			// Safety net: if there are zero admin users but a user named "admin" exists,
			// promote them to admin to prevent lockout (and persist the change).
			if users.AdminCount() == 0 && strings.EqualFold(uname, "admin") {
				if err := users.SetRole("admin", manager.RoleAdmin); err == nil {
					role = string(manager.RoleAdmin)
					if logger != nil {
						// Context label helps identify source (API/UI) in logs
						if strings.TrimSpace(contextLabel) == "" {
							logger.Write("No admin found; auto-promoted 'admin' to admin role.")
						} else {
							logger.Write("[" + contextLabel + "] No admin found; auto-promoted 'admin' to admin role.")
						}
					}
				}
			}
		}
		c.Set("role", role)
		c.Next()
	}
}
