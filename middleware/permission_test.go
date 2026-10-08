package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
)

type stubAuthorizer struct {
	allowed bool
	err     error
	calls   int
}

func (s *stubAuthorizer) HasPermission(ctx context.Context, tenantID, principalID uint64, permCode string) (bool, error) {
	s.calls++
	return s.allowed, s.err
}

func TestRequireTenantPermissionAllows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authz := &stubAuthorizer{allowed: true}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_claims", &auth.TenantClaims{TenantID: 7, PrincipalID: 99})
		c.Next()
	})
	r.GET("/tenants/:tenant_id/orders", func(c *gin.Context) {
		if _, ok := RequireTenantPermission(c, authz, "orders.read"); !ok {
			return
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenants/7/orders", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if authz.calls != 1 {
		t.Fatalf("calls = %d, want 1", authz.calls)
	}
}

func TestRequireTenantPermissionRejectsMismatchedTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_claims", &auth.TenantClaims{TenantID: 7, PrincipalID: 99})
		c.Next()
	})
	r.GET("/tenants/:tenant_id/orders", func(c *gin.Context) {
		if _, ok := RequireTenantPermission(c, &stubAuthorizer{allowed: true}, "orders.read"); !ok {
			return
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenants/8/orders", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestParseUintID(t *testing.T) {
	if got, err := ParseUintID("42"); err != nil || got != 42 {
		t.Fatalf("ParseUintID = %d, %v; want 42, nil", got, err)
	}
	if _, err := ParseUintID("0"); err == nil {
		t.Fatal("ParseUintID(0) should fail")
	}
}
