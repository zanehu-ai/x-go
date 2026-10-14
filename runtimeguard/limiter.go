// Package runtimeguard provides platform runtime safety primitives such as
// route rate limits and short-lived locks.
package runtimeguard

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
)

// Decision is the result of a limiter check.
type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter checks whether a key may consume one event in a fixed window.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Decision, error)
}

type bucket struct {
	windowStart time.Time
	count       int
}

// MemoryLimiter is a fixed-window limiter for single-process dev and tests.
type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
	now     func() time.Time
}

// NewMemoryLimiter creates an in-process limiter.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{buckets: map[string]bucket{}, now: time.Now}
}

// Allow consumes one event for key if the window has capacity.
func (l *MemoryLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (Decision, error) {
	key = strings.TrimSpace(key)
	if l == nil || key == "" || limit <= 0 || window <= 0 {
		return Decision{Allowed: false, RetryAfter: window}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b := l.buckets[key]
	if b.windowStart.IsZero() || now.Sub(b.windowStart) >= window {
		b = bucket{windowStart: now}
	}
	if b.count >= limit {
		retry := b.windowStart.Add(window).Sub(now)
		if retry < 0 {
			retry = 0
		}
		l.buckets[key] = b
		return Decision{Allowed: false, Remaining: 0, RetryAfter: retry}, nil
	}
	b.count++
	l.buckets[key] = b
	return Decision{Allowed: true, Remaining: limit - b.count}, nil
}

// LimitConfig configures a Gin route limiter.
type LimitConfig struct {
	Name   string
	Limit  int
	Window time.Duration
	Key    func(*gin.Context) string
}

// GinMiddleware returns a fail-closed rate-limit middleware.
func GinMiddleware(l Limiter, cfg LimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ""
		if cfg.Key != nil {
			key = cfg.Key(c)
		}
		if l == nil {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		decision, err := l.Allow(c.Request.Context(), key, cfg.Limit, cfg.Window)
		if err != nil || !decision.Allowed {
			if decision.RetryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds()+0.999)))
			}
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Header("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
		c.Next()
	}
}

// IPKey returns a key using only the client IP.
func IPKey(scope string) func(*gin.Context) string {
	return func(c *gin.Context) string {
		return scope + ":ip:" + c.ClientIP()
	}
}

// TenantPrincipalIPKey includes tenant/principal claims when present and falls
// back to IP.
func TenantPrincipalIPKey(scope string) func(*gin.Context) string {
	return func(c *gin.Context) string {
		tenantID, principalID := claimIDs(c)
		if tenantID != "" || principalID != "" {
			return scope + ":tenant:" + tenantID + ":principal:" + principalID + ":ip:" + c.ClientIP()
		}
		return scope + ":ip:" + c.ClientIP()
	}
}

func claimIDs(c *gin.Context) (string, string) {
	if v, ok := c.Get("tenant_claims"); ok && v != nil {
		if claims, ok := v.(*auth.TenantClaims); ok && claims != nil {
			return strconv.FormatUint(claims.TenantID, 10), strconv.FormatUint(claims.PrincipalID, 10)
		}
	}
	if v, ok := c.Get("platform_claims"); ok && v != nil {
		if claims, ok := v.(*auth.PlatformClaims); ok && claims != nil {
			return "", strconv.FormatUint(claims.PrincipalID, 10)
		}
	}
	return "", ""
}
