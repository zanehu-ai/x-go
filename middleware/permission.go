package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
)

// PermissionAuthorizer is the minimal surface for tenant permission checks.
type PermissionAuthorizer interface {
	HasPermission(ctx context.Context, tenantID, principalID uint64, permCode string) (bool, error)
}

// TenantClaimsForPath validates tenant_claims against a uint64 tenant path param.
func TenantClaimsForPath(c *gin.Context, paramName string) (*auth.TenantClaims, bool) {
	if paramName == "" {
		paramName = "tenant_id"
	}
	v, exists := c.Get("tenant_claims")
	claims, ok := v.(*auth.TenantClaims)
	if !exists || !ok || claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant token required"})
		return nil, false
	}
	pathTenantID, err := ParseUintID(c.Param(paramName))
	if err != nil || pathTenantID != claims.TenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "tenant path does not match token"})
		return nil, false
	}
	return claims, true
}

// RequireTenantPermission validates tenant path scope and checks a permission.
func RequireTenantPermission(c *gin.Context, authz PermissionAuthorizer, permCode string, paramName ...string) (*auth.TenantClaims, bool) {
	name := "tenant_id"
	if len(paramName) > 0 && paramName[0] != "" {
		name = paramName[0]
	}
	claims, ok := TenantClaimsForPath(c, name)
	if !ok {
		return nil, false
	}
	if authz == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied", "permission_code": permCode})
		return nil, false
	}
	allowed, err := authz.HasPermission(c.Request.Context(), claims.TenantID, claims.PrincipalID, permCode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
		return nil, false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied", "permission_code": permCode})
		return nil, false
	}
	return claims, true
}

// ParseUintID parses a non-zero uint64 id.
func ParseUintID(v string) (uint64, error) {
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil || id == 0 {
		if err == nil {
			err = errors.New("invalid id")
		}
		return 0, err
	}
	return id, nil
}
