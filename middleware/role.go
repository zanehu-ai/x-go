package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/resp"
)

// RequireRole returns middleware that checks if the authenticated user has one of the allowed roles.
// It reads the "role" key from gin.Context (typically set by an auth middleware upstream).
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		role := c.GetString("role")
		if !allowed[role] {
			resp.Error(c, http.StatusForbidden, resp.CodeForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireHeaderSecret returns middleware that validates a request header against a known secret.
// Commonly used to protect internal/admin endpoints (e.g., headerName = "X-Admin-Secret").
func RequireHeaderSecret(headerName, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader(headerName)
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			resp.Error(c, http.StatusForbidden, resp.CodeForbidden, "invalid secret")
			c.Abort()
			return
		}
		c.Next()
	}
}
