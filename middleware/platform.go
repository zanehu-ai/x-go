package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zanehu-ai/x-go/auth"
	"github.com/zanehu-ai/x-go/obs"
)

// HeaderTokenExpiringSoon is injected when the authenticated token is within
// the expiry warning threshold.
const HeaderTokenExpiringSoon = "X-Token-Expiring-Soon"

// DefaultExpiryWarnThreshold is the default warning window for silent renewal.
const DefaultExpiryWarnThreshold = 30 * time.Minute

var (
	// ErrAuthNotImplemented is returned by skeleton auth integrations.
	ErrAuthNotImplemented = errors.New("middleware: auth not implemented")
	// ErrTenantNotInContext is returned when tenant id is missing from context.
	ErrTenantNotInContext = errors.New("middleware: tenant id not in context")
	// ErrTenantGone signals that the tenant referenced by a valid token no longer exists.
	ErrTenantGone = errors.New("tenant: gone")
)

type ctxKey string

const (
	tenantCtxKey    ctxKey = "synapse.tenant_id"
	requestIDCtxKey ctxKey = "synapse.request_id"
)

// Auth is a pass-through skeleton for projects that wire custom auth later.
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// TenantContext reads X-Tenant-ID and injects it into the request context.
func TenantContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			ctx := context.WithValue(c.Request.Context(), tenantCtxKey, tid)
			c.Request = c.Request.WithContext(ctx)
			c.Set(string(tenantCtxKey), tid)
		}
		c.Next()
	}
}

// TenantIDFromContext reads tenant id from context.
func TenantIDFromContext(ctx context.Context) (string, error) {
	v, ok := ctx.Value(tenantCtxKey).(string)
	if !ok || v == "" {
		return "", ErrTenantNotInContext
	}
	return v, nil
}

// RequestID injects X-Request-ID into context and response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		ctx := context.WithValue(c.Request.Context(), requestIDCtxKey, rid)
		ctx = obs.WithRequestID(ctx, rid)
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}

// RequestIDFromContext extracts request id from context.
func RequestIDFromContext(ctx context.Context) string {
	if rid, ok := obs.RequestIDFromContext(ctx); ok {
		return rid
	}
	v, _ := ctx.Value(requestIDCtxKey).(string)
	return v
}

// Logger logs each request with structured obs fields.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		obs.Default().Info(c.Request.Context(), "request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			obs.FieldDurationMs, latency.Milliseconds(),
			obs.FieldRequestID, RequestIDFromContext(c.Request.Context()),
			"client_ip", c.ClientIP(),
		)
	}
}

// BearerToken extracts the token value from an Authorization header.
func BearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// TokenVersionLookup returns the principal's current token version.
type TokenVersionLookup func(ctx context.Context, principalID uint64) (int, error)

// PlatformToken validates a platform JWT and injects platform_claims.
func PlatformToken(tokenSvc *auth.TokenService, tokenVersion TokenVersionLookup, expiryWarn ...time.Duration) gin.HandlerFunc {
	threshold := DefaultExpiryWarnThreshold
	if len(expiryWarn) > 0 && expiryWarn[0] > 0 {
		threshold = expiryWarn[0]
	}
	return func(c *gin.Context) {
		tok := BearerToken(c.GetHeader("Authorization"))
		if tok == "" || tokenSvc == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "platform token required"})
			return
		}
		claims, err := tokenSvc.ParsePlatformToken(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid platform token"})
			return
		}
		if tokenVersion != nil {
			current, lookupErr := tokenVersion(c.Request.Context(), claims.PrincipalID)
			if lookupErr != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token validation unavailable"})
				return
			}
			if claims.TokenVersion != current {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token revoked"})
				return
			}
		}
		ctx := obs.WithPrincipalID(c.Request.Context(), claims.PrincipalID)
		c.Request = c.Request.WithContext(ctx)
		c.Set("platform_claims", claims)
		if auth.IsExpiringSoon(claims, threshold) {
			c.Header(HeaderTokenExpiringSoon, "true")
		}
		c.Next()
	}
}

// TenantToken validates a tenant JWT and injects tenant_claims.
func TenantToken(tokenSvc *auth.TokenService, tokenVersion TokenVersionLookup, expiryWarn ...time.Duration) gin.HandlerFunc {
	threshold := DefaultExpiryWarnThreshold
	if len(expiryWarn) > 0 && expiryWarn[0] > 0 {
		threshold = expiryWarn[0]
	}
	return func(c *gin.Context) {
		tok := BearerToken(c.GetHeader("Authorization"))
		if tok == "" || tokenSvc == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "tenant token required"})
			return
		}
		claims, err := tokenSvc.ParseTenantToken(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant token"})
			return
		}
		if tokenVersion != nil {
			current, lookupErr := tokenVersion(c.Request.Context(), claims.PrincipalID)
			if lookupErr != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token validation unavailable"})
				return
			}
			if claims.TokenVersion != current {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token revoked"})
				return
			}
		}
		ctx := obs.WithTenantID(c.Request.Context(), claims.TenantID)
		ctx = obs.WithPrincipalID(ctx, claims.PrincipalID)
		c.Request = c.Request.WithContext(ctx)
		c.Set("tenant_claims", claims)
		if auth.IsExpiringSoon(claims, threshold) {
			c.Header(HeaderTokenExpiringSoon, "true")
		}
		c.Next()
	}
}

// PlatformPrincipal extracts platform principal info from gin context.
func PlatformPrincipal(c *gin.Context) (uint64, string) {
	v, exists := c.Get("platform_claims")
	if !exists {
		return 0, ""
	}
	claims, ok := v.(*auth.PlatformClaims)
	if !ok || claims == nil {
		return 0, ""
	}
	return claims.PrincipalID, claims.PrincipalType
}

// FeatureChecker is the minimal surface RequireFeature needs.
type FeatureChecker interface {
	IsEnabled(ctx context.Context, tenantID uint64, featureCode string) (bool, error)
}

// RequireFeature blocks tenant requests when a feature is disabled.
func RequireFeature(svc FeatureChecker, featureCode string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if v, ok := c.Get("platform_claims"); ok && v != nil {
			c.Next()
			return
		}
		v, exists := c.Get("tenant_claims")
		claims, ok := v.(*auth.TenantClaims)
		if !exists || !ok || claims == nil || claims.TenantID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "tenant token required"})
			return
		}
		if svc == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "feature check unavailable"})
			return
		}
		enabled, err := svc.IsEnabled(c.Request.Context(), claims.TenantID, featureCode)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "feature check failed"})
			return
		}
		if !enabled {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":        "feature_disabled",
				"feature_code": featureCode,
			})
			return
		}
		c.Next()
	}
}

// TenantStatusLookup returns the current tenant status (0=active).
type TenantStatusLookup func(ctx context.Context, tenantID uint64) (int8, error)

// StatusCache is the minimal cache surface RequireTenantActive needs.
type StatusCache interface {
	Get(tenantID uint64) (int8, bool)
	Set(tenantID uint64, status int8)
}

// RequireTenantActive blocks tenant requests when the tenant is suspended or gone.
func RequireTenantActive(lookup TenantStatusLookup, statusCache StatusCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		if v, ok := c.Get("platform_claims"); ok && v != nil {
			c.Next()
			return
		}
		v, exists := c.Get("tenant_claims")
		claims, ok := v.(*auth.TenantClaims)
		if !exists || !ok || claims == nil || claims.TenantID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "tenant token required"})
			return
		}
		if lookup == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tenant status check unavailable"})
			return
		}
		status, err := lookupStatus(c.Request.Context(), lookup, statusCache, claims.TenantID)
		if err != nil {
			if errors.Is(err, ErrTenantGone) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "tenant_gone"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tenant status check failed"})
			return
		}
		if status != 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "tenant_suspended"})
			return
		}
		c.Next()
	}
}

func lookupStatus(ctx context.Context, lookup TenantStatusLookup, c StatusCache, tenantID uint64) (int8, error) {
	if c != nil {
		if v, ok := c.Get(tenantID); ok {
			return v, nil
		}
	}
	status, err := lookup(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if c != nil {
		c.Set(tenantID, status)
	}
	return status, nil
}
