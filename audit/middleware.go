package audit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/ginutil"
)

// Middleware returns a Gin middleware that automatically logs write operations
// (POST, PUT, PATCH, DELETE). Logging is asynchronous and non-blocking —
// it does not affect the request response time or status.
func Middleware(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method

		if method != http.MethodPost && method != http.MethodPut &&
			method != http.MethodPatch && method != http.MethodDelete {
			c.Next()
			return
		}

		c.Next()

		userID := int64(ginutil.GetUserID(c))
		username := c.GetString("username")
		status := c.Writer.Status()
		path := c.Request.URL.Path
		ip := c.ClientIP()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					_ = r // Audit logging must never crash the process
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = store.Save(ctx, &Entry{
				UserID:   userID,
				Username: username,
				Action:   method,
				Resource: path,
				Detail:   fmt.Sprintf("status:%d", status),
				IP:       ip,
			})
		}()
	}
}
