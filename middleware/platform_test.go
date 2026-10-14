package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
	"github.com/zanehu-ai/x-go/cache"
)

func TestTenantContextFromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TenantContext())

	var captured string
	r.GET("/probe", func(c *gin.Context) {
		tid, err := TenantIDFromContext(c.Request.Context())
		if err == nil {
			captured = tid
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("X-Tenant-ID", "tenant-abc")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if captured != "tenant-abc" {
		t.Fatalf("tenant id = %q, want tenant-abc", captured)
	}
}

func TestTenantIDFromContextMissing(t *testing.T) {
	_, err := TenantIDFromContext(context.Background())
	if !errors.Is(err, ErrTenantNotInContext) {
		t.Fatalf("error = %v, want ErrTenantNotInContext", err)
	}
}

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())

	var captured string
	r.GET("/probe", func(c *gin.Context) {
		captured = RequestIDFromContext(c.Request.Context())
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("X-Request-ID", "rid-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if captured != "rid-123" || w.Header().Get("X-Request-ID") != "rid-123" {
		t.Fatalf("request id not propagated: context=%q header=%q", captured, w.Header().Get("X-Request-ID"))
	}
}

func TestPlatformTokenMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokenSvc := newTokenService(t, 600, 3600)
	tok, err := tokenSvc.IssuePlatformToken(7, []string{"platform.admin"}, 1)
	if err != nil {
		t.Fatalf("IssuePlatformToken: %v", err)
	}

	r := gin.New()
	r.Use(PlatformToken(tokenSvc, nil, 30*time.Minute))
	r.GET("/probe", func(c *gin.Context) {
		id, typ := PlatformPrincipal(c)
		if id != 7 || typ != "platform_admin" {
			t.Fatalf("principal = (%d, %q), want (7, platform_admin)", id, typ)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get(HeaderTokenExpiringSoon); got != "true" {
		t.Fatalf("%s = %q, want true", HeaderTokenExpiringSoon, got)
	}
}

func TestTenantTokenMiddlewareRejectsPlatformToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokenSvc := newTokenService(t, 3600, 3600)
	tok, err := tokenSvc.IssuePlatformToken(7, []string{"platform.admin"}, 1)
	if err != nil {
		t.Fatalf("IssuePlatformToken: %v", err)
	}

	r := gin.New()
	r.Use(TenantToken(tokenSvc, nil))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

type stubFeature struct {
	enabled bool
	err     error
	calls   int
}

func (s *stubFeature) IsEnabled(ctx context.Context, tenantID uint64, code string) (bool, error) {
	s.calls++
	return s.enabled, s.err
}

func TestRequireFeature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectTenantClaims(123))
	r.Use(RequireFeature(&stubFeature{enabled: true}, "payments"))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestRequireFeaturePlatformShortCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubFeature{enabled: false}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("platform_claims", &auth.PlatformClaims{PrincipalID: 1})
		c.Next()
	})
	r.Use(RequireFeature(stub, "payments"))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if stub.calls != 0 {
		t.Fatalf("feature checker calls = %d, want 0", stub.calls)
	}
}

func TestRequireFeatureNilCheckerFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectTenantClaims(123))
	r.Use(RequireFeature(nil, "payments"))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("feature check unavailable")) {
		t.Fatalf("body = %s, want feature check unavailable", w.Body.String())
	}
}

func TestRequireTenantActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lookup := func(ctx context.Context, tenantID uint64) (int8, error) { return 0, nil }
	r := gin.New()
	r.Use(injectTenantClaims(7))
	r.Use(RequireTenantActive(lookup, nil))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestRequireTenantActiveNilLookupFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectTenantClaims(7))
	r.Use(RequireTenantActive(nil, nil))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("tenant status check unavailable")) {
		t.Fatalf("body = %s, want tenant status check unavailable", w.Body.String())
	}
}

func TestRequireTenantActiveGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lookup := func(ctx context.Context, tenantID uint64) (int8, error) { return 0, ErrTenantGone }
	r := gin.New()
	r.Use(injectTenantClaims(7))
	r.Use(RequireTenantActive(lookup, nil))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("tenant_gone")) {
		t.Fatalf("body = %s, want tenant_gone", w.Body.String())
	}
}

func TestRequireTenantActiveCacheHitSkipsLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	lookup := func(ctx context.Context, tenantID uint64) (int8, error) {
		calls++
		return 0, nil
	}
	statusCache := cache.New[uint64, int8](16, time.Minute)
	statusCache.Set(7, 0)

	r := gin.New()
	r.Use(injectTenantClaims(7))
	r.Use(RequireTenantActive(lookup, statusCache))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if calls != 0 {
		t.Fatalf("lookup calls = %d, want 0", calls)
	}
}

func injectTenantClaims(tenantID uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("tenant_claims", &auth.TenantClaims{
			TenantID:    tenantID,
			TenantCode:  "test",
			PrincipalID: 99,
		})
		c.Next()
	}
}

func newTokenService(t *testing.T, platformTTL, tenantTTL int) *auth.TokenService {
	t.Helper()
	tokenSvc, err := auth.NewTokenService("test_jwt_secret_key_0000000000000000", "test", platformTTL, tenantTTL)
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}
	return tokenSvc
}
