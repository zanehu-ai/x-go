package ginutil

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/resp"
)

// ParseIDParam reads ":id" from the route, writes a 400 error and returns false if invalid.
func ParseIDParam(c *gin.Context) (int64, bool) {
	return ParseParam(c, "id")
}

// ParseParam reads a named route param, writes a 400 error and returns false if not a valid int64.
func ParseParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.CodeBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

// GetUserID reads "user_id" from the gin context.
// Handles uint64, int64, int, and float64 (JWT MapClaims decodes numbers as float64).
func GetUserID(c *gin.Context) uint64 {
	v, _ := c.Get("user_id")
	switch id := v.(type) {
	case uint64:
		return id
	case int64:
		if id < 0 {
			return 0
		}
		return uint64(id)
	case int:
		if id < 0 {
			return 0
		}
		return uint64(id)
	case float64:
		if id < 0 {
			return 0
		}
		return uint64(id)
	default:
		return 0
	}
}

// GetRole reads "role" (string) from the gin context.
func GetRole(c *gin.Context) string {
	return c.GetString("role")
}
