package tenant

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/resp"
)

// Tenant is the minimal reusable tenant identity shape.
type Tenant struct {
	ID   string
	Code string
	Name string
}

type contextKey struct{}

// InjectToContext stores the tenant ID in the context.
func InjectToContext(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, contextKey{}, tenantID)
}

// FromContext retrieves the tenant ID from the context.
// Returns empty string if not set.
func FromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

// FromGinContext retrieves the tenant ID from the Gin context key.
func FromGinContext(c *gin.Context, key string) string {
	return c.GetString(key)
}

// Middleware returns a Gin middleware that extracts the tenant ID from the given
// context key (typically set by an auth middleware upstream) and injects it into
// the request context for downstream use.
func Middleware(ginContextKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetString(ginContextKey)
		if tenantID == "" {
			resp.Error(c, http.StatusBadRequest, resp.CodeBadRequest, "missing tenant context")
			c.Abort()
			return
		}
		ctx := InjectToContext(c.Request.Context(), tenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
