package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultCORSMaxAge = 86400

	headerAllowOrigin      = "Access-Control-Allow-Origin"
	headerAllowMethods     = "Access-Control-Allow-Methods"
	headerAllowHeaders     = "Access-Control-Allow-Headers"
	headerAllowCredentials = "Access-Control-Allow-Credentials"
	headerMaxAge           = "Access-Control-Max-Age"
	headerVary             = "Vary"
)

// ErrInvalidOrigin means a configured or incoming value is not an exact origin.
// Rejected forms include "*", paths, trailing slashes, bare "#" / "?" / ":",
// the literal "null", and ports outside 1–65535 (including :0 and :99999).
var ErrInvalidOrigin = errors.New("middleware: invalid CORS origin")

// ErrWildcardWithCredentials means "*" was combined with credentialed responses.
// The middleware never emits Access-Control-Allow-Origin: * together with
// Access-Control-Allow-Credentials: true.
var ErrWildcardWithCredentials = errors.New("middleware: wildcard CORS origin cannot be used with credentials")

// ErrInsecureOrigin means a non-https origin was configured for a prod-like
// environment. staging, prod, and production share this rule.
var ErrInsecureOrigin = errors.New("middleware: insecure CORS origin for environment")

// CORSOption configures CORSMiddleware / NewCORSMiddleware.
type CORSOption func(*corsConfig)

type corsConfig struct {
	origins          []string
	headers          []string
	methods          []string
	env              string
	allowCredentials bool
}

type compiledCORS struct {
	allowed          map[string]struct{}
	allowCredentials bool
	allowHeaders     string
	allowMethods     string
	maxAge           string
}

// WithAllowedOrigins appends exact origins. Comma-separated values are split
// the same way as the CORSMiddleware origins argument.
func WithAllowedOrigins(origins ...string) CORSOption {
	return func(cfg *corsConfig) {
		for _, origin := range origins {
			cfg.origins = append(cfg.origins, splitCSV(origin)...)
		}
	}
}

// WithAllowedHeaders replaces the default allow-headers list.
// The default is Content-Type and Authorization. Idempotency-Key and
// X-Admin-Token are not included unless a caller adds them here.
func WithAllowedHeaders(headers ...string) CORSOption {
	return func(cfg *corsConfig) {
		cfg.headers = append([]string(nil), headers...)
	}
}

// WithAllowedMethods replaces the default allow-methods list.
func WithAllowedMethods(methods ...string) CORSOption {
	return func(cfg *corsConfig) {
		cfg.methods = append([]string(nil), methods...)
	}
}

// WithAllowCredentials sets whether a matched exact origin may receive
// Access-Control-Allow-Credentials: true. The default is true so existing
// callers keep working. Wildcard origins never enable credentials.
func WithAllowCredentials(allow bool) CORSOption {
	return func(cfg *corsConfig) {
		cfg.allowCredentials = allow
	}
}

// WithEnv selects the origin policy. staging, prod, and production (any case)
// are prod-like: only https origins are accepted. Any other value, including
// empty, keeps the historical behaviour of allowing http origins such as
// local dev servers.
func WithEnv(env string) CORSOption {
	return func(cfg *corsConfig) {
		cfg.env = env
	}
}

// CORSMiddleware allows the given comma-separated origins and handles preflight.
// Extra behaviour is set with CORSOption values. Callers that only pass an
// origin list keep working.
//
// Every response sets Vary: Origin. A matched browser origin is echoed only
// after it passes the same normaliser used for the allow-list, then compared
// by exact string equality.
//
// Invalid configuration fail-closes: the returned handler still sets
// Vary: Origin and reflects no origin. Use NewCORSMiddleware at startup when
// the process should refuse to boot on a bad allow-list.
func CORSMiddleware(origins string, opts ...CORSOption) gin.HandlerFunc {
	handler, err := NewCORSMiddleware(origins, opts...)
	if err != nil {
		return failClosedCORS()
	}
	return handler
}

// NewCORSMiddleware validates origins and returns middleware.
// It is the options-style constructor existing servers should migrate to
// when they need a startup error instead of a fail-closed handler.
func NewCORSMiddleware(origins string, opts ...CORSOption) (gin.HandlerFunc, error) {
	compiled, err := compileCORS(origins, opts)
	if err != nil {
		return nil, err
	}
	return compiled.handler(), nil
}

func compileCORS(origins string, opts []CORSOption) (*compiledCORS, error) {
	cfg := corsConfig{
		allowCredentials: true,
		headers:          []string{"Content-Type", "Authorization"},
		methods:          []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	}
	cfg.origins = splitCSV(origins)
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	allowed := make(map[string]struct{}, len(cfg.origins))
	var problems []error
	for _, raw := range cfg.origins {
		if raw == "*" {
			problems = append(problems, wildcardError(cfg.allowCredentials))
			continue
		}
		normalized, err := normalizeOrigin(raw)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if err := enforceEnvPolicy(cfg.env, normalized); err != nil {
			problems = append(problems, err)
			continue
		}
		allowed[normalized] = struct{}{}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}

	allowHeaders, err := canonicalList(cfg.headers, false)
	if err != nil {
		return nil, err
	}
	allowMethods, err := canonicalList(cfg.methods, true)
	if err != nil {
		return nil, err
	}
	if allowHeaders == "" || allowMethods == "" {
		return nil, fmt.Errorf("%w: allowed headers and methods must be non-empty", ErrInvalidOrigin)
	}

	return &compiledCORS{
		allowed:          allowed,
		allowCredentials: cfg.allowCredentials,
		allowHeaders:     allowHeaders,
		allowMethods:     allowMethods,
		maxAge:           strconv.Itoa(defaultCORSMaxAge),
	}, nil
}

func wildcardError(allowCredentials bool) error {
	if allowCredentials {
		return fmt.Errorf("%w", ErrWildcardWithCredentials)
	}
	return fmt.Errorf("%w: %q", ErrInvalidOrigin, "*")
}

func (cc *compiledCORS) handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		setVaryOrigin(c)
		cc.applyAllowHeaders(c)
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (cc *compiledCORS) applyAllowHeaders(c *gin.Context) {
	raw := c.GetHeader("Origin")
	if raw == "" {
		return
	}
	// Same normaliser as the allow-list. No second cleanup path.
	normalized, err := normalizeOrigin(raw)
	if err != nil {
		return
	}
	if _, ok := cc.allowed[normalized]; !ok {
		return
	}
	c.Header(headerAllowOrigin, normalized)
	c.Header(headerAllowMethods, cc.allowMethods)
	c.Header(headerAllowHeaders, cc.allowHeaders)
	c.Header(headerMaxAge, cc.maxAge)
	if cc.allowCredentials {
		c.Header(headerAllowCredentials, "true")
	}
}

func failClosedCORS() gin.HandlerFunc {
	return (&compiledCORS{
		allowed:          map[string]struct{}{},
		allowCredentials: false,
		allowHeaders:     "Content-Type, Authorization",
		allowMethods:     "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		maxAge:           strconv.Itoa(defaultCORSMaxAge),
	}).handler()
}

func setVaryOrigin(c *gin.Context) {
	existing := c.Writer.Header().Values(headerVary)
	for _, value := range existing {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Origin") {
				return
			}
		}
	}
	c.Writer.Header().Add(headerVary, "Origin")
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func canonicalList(items []string, upper bool) (string, error) {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.ContainsAny(item, "\r\n") || strings.Contains(item, ",") {
			return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, item)
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if upper {
			item = strings.ToUpper(item)
		}
		out = append(out, item)
	}
	return strings.Join(out, ", "), nil
}

// normalizeOrigin is the only origin canonicaliser. Allow-list entries and
// the request Origin header both go through it before exact comparison.
// It rejects malformed origins; it does not strip paths, queries, fragments,
// or trailing slashes into a looser match.
func normalizeOrigin(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	switch s {
	case "", "*", "#", "?", ":", "null":
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	if strings.ContainsAny(s, "#?\\") {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == ' ' {
			return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
		}
	}

	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "*") {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	port, err := canonicalPort(scheme, u.Port())
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidOrigin, raw)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port == "" {
		return scheme + "://" + host, nil
	}
	return scheme + "://" + host + ":" + port, nil
}

func canonicalPort(scheme, port string) (string, error) {
	if port == "" {
		return "", nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errInvalidPort
	}
	if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
		return "", nil
	}
	return strconv.Itoa(n), nil
}

var errInvalidPort = errors.New("invalid port")

func enforceEnvPolicy(env, normalized string) error {
	if !prodLikeEnv(env) {
		return nil
	}
	if !strings.HasPrefix(normalized, "https://") {
		return fmt.Errorf("%w: %q (env %q)", ErrInsecureOrigin, normalized, strings.TrimSpace(env))
	}
	return nil
}

func prodLikeEnv(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "prod", "production", "staging":
		return true
	default:
		return false
	}
}
