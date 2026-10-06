package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeOrigin_SinglePath(t *testing.T) {
	t.Parallel()
	got, err := normalizeOrigin("HTTPS://Example.COM:443")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	again, err := normalizeOrigin(got)
	if err != nil {
		t.Fatalf("second normalize: %v", err)
	}
	if got != "https://example.com" || again != got {
		t.Fatalf("canonical = %q then %q, want https://example.com", got, again)
	}

	fromRequest, err := normalizeOrigin("https://example.com")
	if err != nil {
		t.Fatalf("request form: %v", err)
	}
	if fromRequest != got {
		t.Fatalf("config %q and request %q diverged", got, fromRequest)
	}
}

func TestNormalizeOrigin_Rejects(t *testing.T) {
	t.Parallel()
	rejected := []string{
		"*",
		"#",
		"?",
		":",
		":0",
		":99999",
		"null",
		"",
		"   ",
		"https://example.com/path",
		"https://example.com/",
		"https://example.com:443/",
		"https://example.com:0",
		"https://example.com:99999",
		"https://example.com:65536",
		"http://localhost:0",
		"https://example.com#",
		"https://example.com?",
		"https://example.com#frag",
		"https://example.com?x=1",
		"https://user:pass@example.com",
		"https://*.example.com",
		"javascript:alert(1)",
		"https://example.com:abc",
	}
	for _, raw := range rejected {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeOrigin(raw); !errors.Is(err, ErrInvalidOrigin) {
				t.Fatalf("normalizeOrigin(%q) err = %v, want ErrInvalidOrigin", raw, err)
			}
		})
	}
}

func TestCORS_HTTPRejectedInProdAndStaging(t *testing.T) {
	t.Parallel()
	envs := []string{"prod", "production", "PROD", "staging", "Staging"}
	for _, env := range envs {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			_, err := NewCORSMiddleware("http://app.example", WithEnv(env))
			if !errors.Is(err, ErrInsecureOrigin) {
				t.Fatalf("NewCORSMiddleware err = %v, want ErrInsecureOrigin", err)
			}
			w := invokeCORS(CORSMiddleware("http://app.example", WithEnv(env)), http.MethodGet, "http://app.example")
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("fail-closed reflected %q", w.Header().Get("Access-Control-Allow-Origin"))
			}
			if w.Header().Get("Vary") != "Origin" {
				t.Fatalf("Vary = %q, want Origin", w.Header().Get("Vary"))
			}
		})
	}
}

func TestCORS_DevAllowsHTTP(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware("http://localhost:3000", WithEnv("development"))
	if err != nil {
		t.Fatalf("dev http origin rejected: %v", err)
	}
	w := invokeCORS(handler, http.MethodGet, "http://localhost:3000")
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("ACAO = %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_ExactMatchAfterNormalisation(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware("HTTPS://Example.COM:443, https://api.example:8443", WithEnv("production"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	matched := invokeCORS(handler, http.MethodGet, "https://example.com")
	if matched.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("ACAO = %q, want https://example.com", matched.Header().Get("Access-Control-Allow-Origin"))
	}

	misses := []string{
		"https://example.com.evil.com",
		"https://notexample.com",
		"https://example.com/",
		"https://example.com/path",
		"http://example.com",
		"https://api.example",
		"https://api.example:443",
	}
	for _, origin := range misses {
		w := invokeCORS(handler, http.MethodGet, origin)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("origin %q reflected as %q", origin, got)
		}
	}
}

func TestCORS_VaryOriginUnconditional(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware("https://app.example", WithEnv("prod"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		origin string
		set    bool
	}{
		{name: "allowed", origin: "https://app.example", set: true},
		{name: "blocked", origin: "https://evil.example", set: true},
		{name: "null", origin: "null", set: true},
		{name: "missing", set: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.set {
				c.Request.Header.Set("Origin", tc.origin)
			}
			handler(c)
			if w.Header().Get("Vary") != "Origin" {
				t.Fatalf("Vary = %q, want Origin", w.Header().Get("Vary"))
			}
		})
	}
}

func TestCORS_CredentialsNeverWithWildcard(t *testing.T) {
	t.Parallel()
	_, err := NewCORSMiddleware("*")
	if !errors.Is(err, ErrWildcardWithCredentials) {
		t.Fatalf("default credentials + * err = %v", err)
	}
	_, err = NewCORSMiddleware("https://app.example,*", WithEnv("production"), WithAllowCredentials(true))
	if !errors.Is(err, ErrWildcardWithCredentials) {
		t.Fatalf("mixed * err = %v", err)
	}
	_, err = NewCORSMiddleware("*", WithAllowCredentials(false), WithEnv("development"))
	if !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("* without credentials err = %v, want ErrInvalidOrigin", err)
	}
	_, err = NewCORSMiddleware("*", WithAllowCredentials(false), WithEnv("prod"))
	if !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("prod * err = %v, want ErrInvalidOrigin", err)
	}

	w := invokeCORS(CORSMiddleware("*"), http.MethodGet, "https://app.example")
	if w.Header().Get("Access-Control-Allow-Origin") == "*" && w.Header().Get("Access-Control-Allow-Credentials") == "true" {
		t.Fatal("emitted wildcard with credentials")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("fail-closed still set ACAO=%q credentials=%q",
			w.Header().Get("Access-Control-Allow-Origin"),
			w.Header().Get("Access-Control-Allow-Credentials"))
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", w.Header().Get("Vary"))
	}
}

func TestCORS_DuplicateOrigins(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware(
		"https://app.example, https://app.example",
		WithAllowedOrigins("HTTPS://APP.example:443", "https://app.example"),
		WithAllowedHeaders("Authorization", "authorization", "Content-Type"),
		WithAllowedMethods("get", "POST", "get"),
		WithEnv("production"),
	)
	if err != nil {
		t.Fatal(err)
	}
	w := invokeCORS(handler, http.MethodGet, "https://app.example")
	if got := w.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "https://app.example" {
		t.Fatalf("ACAO values = %#v", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("allow-headers = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("allow-methods = %q", got)
	}
}

func TestCORS_NullAndMissingOrigin(t *testing.T) {
	t.Parallel()
	_, err := NewCORSMiddleware("https://app.example, null", WithEnv("production"))
	if err == nil || !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("allow-list containing null err = %v", err)
	}

	handler, err := NewCORSMiddleware("https://app.example", WithEnv("production"))
	if err != nil {
		t.Fatal(err)
	}
	nullRes := invokeCORS(handler, http.MethodGet, "null")
	if nullRes.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("reflected null origin as %q", nullRes.Header().Get("Access-Control-Allow-Origin"))
	}
	if nullRes.Header().Get("Vary") != "Origin" {
		t.Fatalf("null origin Vary = %q", nullRes.Header().Get("Vary"))
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler(c)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("missing Origin was reflected")
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("missing Origin Vary = %q", w.Header().Get("Vary"))
	}
}

func TestCORS_DefaultHeadersOmitSensitiveNames(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware("https://app.example", WithEnv("production"))
	if err != nil {
		t.Fatal(err)
	}
	w := invokeCORS(handler, http.MethodOptions, "https://app.example")
	got := w.Header().Get("Access-Control-Allow-Headers")
	if got != "Content-Type, Authorization" {
		t.Fatalf("allow-headers = %q", got)
	}
	lower := strings.ToLower(got)
	if strings.Contains(lower, "idempotency-key") || strings.Contains(lower, "x-admin-token") {
		t.Fatalf("default allow-headers include a sensitive name: %q", got)
	}
}

func TestCORS_PreflightOnAuthenticatedRouteGroup(t *testing.T) {
	t.Parallel()
	r := gin.New()
	api := r.Group("/api")
	api.Use(
		CORSMiddleware("https://app.example", WithEnv("production")),
		PlatformToken(nil, nil),
	)
	api.Any("/orders", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	preflight := httptest.NewRequest(http.MethodOptions, "/api/orders", nil)
	preflight.Header.Set("Origin", "https://app.example")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	preflightW := httptest.NewRecorder()
	r.ServeHTTP(preflightW, preflight)

	if preflightW.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204; body %s", preflightW.Code, preflightW.Body.String())
	}
	if preflightW.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("preflight ACAO = %q", preflightW.Header().Get("Access-Control-Allow-Origin"))
	}
	if preflightW.Header().Get("Vary") != "Origin" {
		t.Fatalf("preflight Vary = %q", preflightW.Header().Get("Vary"))
	}
	if preflightW.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("preflight missing credentials header for exact origin")
	}
	if strings.Contains(preflightW.Body.String(), "platform token required") {
		t.Fatal("preflight reached auth middleware")
	}

	actual := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	actual.Header.Set("Origin", "https://app.example")
	actualW := httptest.NewRecorder()
	r.ServeHTTP(actualW, actual)
	if actualW.Code != http.StatusUnauthorized {
		t.Fatalf("authenticated route status = %d, want 401", actualW.Code)
	}
	if actualW.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("401 ACAO = %q", actualW.Header().Get("Access-Control-Allow-Origin"))
	}
	if actualW.Header().Get("Vary") != "Origin" {
		t.Fatalf("401 Vary = %q", actualW.Header().Get("Vary"))
	}
}

func TestCORS_InvalidSiblingFailClosed(t *testing.T) {
	t.Parallel()
	_, err := NewCORSMiddleware("https://good.example, https://bad.example/path", WithEnv("production"))
	if !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("err = %v, want ErrInvalidOrigin", err)
	}
	w := invokeCORS(CORSMiddleware("https://good.example, https://bad.example/", WithEnv("prod")), http.MethodGet, "https://good.example")
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("partial allow-list was applied")
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", w.Header().Get("Vary"))
	}
}

func TestCORS_CustomHeadersAvailableWhenRequested(t *testing.T) {
	t.Parallel()
	handler, err := NewCORSMiddleware("https://app.example",
		WithEnv("staging"),
		WithAllowedHeaders("Content-Type", "Authorization", "Idempotency-Key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	w := invokeCORS(handler, http.MethodOptions, "https://app.example")
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization, Idempotency-Key" {
		t.Fatalf("allow-headers = %q", got)
	}
}

func invokeCORS(handler gin.HandlerFunc, method, origin string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", nil)
	if origin != "" {
		c.Request.Header.Set("Origin", origin)
	}
	handler(c)
	return w
}
