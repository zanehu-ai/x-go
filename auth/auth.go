// Package auth provides JWT-based platform and tenant authentication.
//
// Two token types per §7.5 of the architecture:
//   - PlatformToken: for platform_admin operations
//   - TenantToken:   for tenant-scoped operations; includes actor_principal_id when issued via act-as
//
// # Cross-language canonical reference (Phase A W2-A4, 2026-05-08)
//
// The Java-side canonical implementation is bybys.common.util.JwtUtil
// (templates/game/backend/src/main/java/com/bybys/common/util/JwtUtil.java).
// platform-infra/JwtTokenService is excluded by PlatformComponentsConfig.java:23-32
// and is @Deprecated dead code — do NOT build SDK against it.
//
// Go claims schema intentionally differs from JwtUtil's flat userType enum —
// see docs/migration/phase-a-platform-gaps/identity.md §6 for the full comparison
// and the rationale for not aligning downward.
//
// JwtUtil-specific behaviors ported to Go:
//   - IsExpiringSoon: 30-min expiry warning (mirrors JwtUtil.isTokenExpiringSoon)
//   - SelfTest: startup self-check (mirrors JwtUtil.getHealthStatus round-trip)
//
// Cross-language interop vectors: synapse-go/auth/testdata/jwt_interop_vectors.json
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken  = errors.New("auth: invalid or expired token")
	ErrWrongType     = errors.New("auth: token type mismatch")
	ErrInvalidStepUp = errors.New("auth: invalid step-up token request")
)

// DefaultAudience is the default audience string for Synapse-issued tokens.
const DefaultAudience = "synapse"

// ExpectedAudience is kept as a compatibility alias for callers and tests that
// used the previous package-level constant.
const ExpectedAudience = DefaultAudience

// knownBadSecrets is the exact-match blacklist ported from
// 818-gaming JwtUtil.isDefaultPlaceholder() (bybys/common/util/JwtUtil.java).
// These are known dev/test placeholders that must never reach production.
// Comparison is case-insensitive exact match (mirrors Java behavior).
var knownBadSecrets = []string{
	"your_production_jwt_secret_key_here",
	"dev_jwt_secret_key_please_change_in_production",
	"dev_jwt_secret_key_please_change_in_production_32chars",
	"test_jwt_secret_key_32chars_for_testing",
	"change_me",
	"secret",
	"jwt_secret",
	"default_secret",
}

const minSecretLen = 32

// MaxStepUpTTL is the longest lifetime accepted for a step-up token.
const MaxStepUpTTL = 5 * time.Minute

// ValidateSecretStrength checks that secret meets the minimum bar for use as a
// JWT signing key:
//   - length >= 32 characters
//   - must not be one of the known dev/placeholder values (case-insensitive)
//
// TODO(identity.md §3 P0-2): optional strength rules (mixed-case + digit +
// special character) are intentionally NOT enforced here; add them as a
// separate hardening PR once the mandatory floor is stable.
func ValidateSecretStrength(secret string) error {
	if len(secret) < minSecretLen {
		return fmt.Errorf("JWT secret must be at least %d characters (got %d)", minSecretLen, len(secret))
	}
	lower := strings.ToLower(secret)
	for _, bad := range knownBadSecrets {
		if lower == strings.ToLower(bad) {
			return fmt.Errorf("JWT secret matches known placeholder %q; set a unique production secret", bad)
		}
	}
	return nil
}

// PlatformClaims are the JWT claims for a platform-admin token.
type PlatformClaims struct {
	PrincipalID   uint64   `json:"principal_id"`
	PrincipalType string   `json:"principal_type"` // "platform_admin"
	RoleCodes     []string `json:"role_codes"`
	TokenVersion  int      `json:"token_version"`
	TokenType     string   `json:"token_type"` // "platform"
	jwt.RegisteredClaims
}

// TenantClaims are the JWT claims for a tenant-scoped token.
type TenantClaims struct {
	TenantID         uint64   `json:"tenant_id"`
	TenantCode       string   `json:"tenant_code"`
	PrincipalID      uint64   `json:"principal_id"`
	PrincipalType    string   `json:"principal_type"`
	RoleCodes        []string `json:"role_codes"`
	TokenVersion     int      `json:"token_version"`
	TokenType        string   `json:"token_type"`                   // "tenant"
	ActorPrincipalID *uint64  `json:"actor_principal_id,omitempty"` // set when issued via act-as
	jwt.RegisteredClaims
}

// StepUpClaims are issued by control-api after a successful re-auth (typically
// password verification) and consumed by downstream services to gate a single
// sensitive action. Short-lived (≤ 5 min) and bound to a specific Scope so a
// step-up minted for one action cannot be replayed against another.
type StepUpClaims struct {
	PrincipalID uint64 `json:"principal_id"`
	Scope       string `json:"scope"`      // e.g. "cargo.wallet-adjust"
	TokenType   string `json:"token_type"` // "step_up"
	jwt.RegisteredClaims
}

// TokenService signs and verifies JWTs.
type TokenService struct {
	secret   []byte
	issuer   string
	audience string
	platTTL  time.Duration
	tenTTL   time.Duration
	now      func() time.Time
}

// NewTokenService creates a TokenService.
// It validates secret strength (length >= 32, not a known placeholder) and
// returns an error if validation fails. Call sites should treat the error as
// fatal: a weak or placeholder secret must never be used to sign tokens.
func NewTokenService(secret, issuer string, platTTLSec, tenTTLSec int) (*TokenService, error) {
	return NewTokenServiceWithAudience(secret, issuer, DefaultAudience, platTTLSec, tenTTLSec)
}

// NewTokenServiceWithAudience creates a TokenService with an explicit JWT
// audience. Use this when multiple services share an issuer or signing key and
// each service needs a distinct audience boundary.
func NewTokenServiceWithAudience(secret, issuer, audience string, platTTLSec, tenTTLSec int) (*TokenService, error) {
	if err := ValidateSecretStrength(secret); err != nil {
		return nil, err
	}
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return nil, errors.New("auth: audience is required")
	}
	return &TokenService{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: audience,
		platTTL:  time.Duration(platTTLSec) * time.Second,
		tenTTL:   time.Duration(tenTTLSec) * time.Second,
		now:      time.Now,
	}, nil
}

// Audience returns the configured JWT audience.
func (s *TokenService) Audience() string { return s.audience }

func (s *TokenService) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// TenantTTL returns the configured tenant token lifetime.
// Exposed so callers (e.g. identity.Service) can compute token expiry
// without duplicating the TTL constant.
func (s *TokenService) TenantTTL() time.Duration { return s.tenTTL }

// IssuePlatformToken signs a platform-admin JWT.
func (s *TokenService) IssuePlatformToken(principalID uint64, roleCodes []string, tokenVersion int) (string, error) {
	now := s.currentTime()
	claims := PlatformClaims{
		PrincipalID:   principalID,
		PrincipalType: "platform_admin",
		RoleCodes:     roleCodes,
		TokenVersion:  tokenVersion,
		TokenType:     "platform",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.platTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// IssueTenantToken signs a tenant-scoped JWT.
// actorPrincipalID is non-nil when this token is issued via a platform_admin act-as flow.
func (s *TokenService) IssueTenantToken(
	tenantID uint64, tenantCode string,
	principalID uint64, principalType string,
	roleCodes []string, tokenVersion int,
	actorPrincipalID *uint64,
) (string, error) {
	now := s.currentTime()
	claims := TenantClaims{
		TenantID:         tenantID,
		TenantCode:       tenantCode,
		PrincipalID:      principalID,
		PrincipalType:    principalType,
		RoleCodes:        roleCodes,
		TokenVersion:     tokenVersion,
		TokenType:        "tenant",
		ActorPrincipalID: actorPrincipalID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// ParsePlatformToken verifies and decodes a platform token.
func (s *TokenService) ParsePlatformToken(tokenStr string) (*PlatformClaims, error) {
	claims := &PlatformClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, s.keyFunc, jwt.WithTimeFunc(s.currentTime))
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.TokenType != "platform" || claims.PrincipalID == 0 || claims.PrincipalType != "platform_admin" {
		return nil, ErrWrongType
	}
	if err := checkAudienceCompat(claims.Audience, s.audience); err != nil {
		return nil, err
	}
	return claims, nil
}

// ParseTenantToken verifies and decodes a tenant token.
func (s *TokenService) ParseTenantToken(tokenStr string) (*TenantClaims, error) {
	claims := &TenantClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, s.keyFunc, jwt.WithTimeFunc(s.currentTime))
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.TokenType != "tenant" || claims.TenantID == 0 || claims.PrincipalID == 0 {
		return nil, ErrWrongType
	}
	if err := checkAudienceCompat(claims.Audience, s.audience); err != nil {
		return nil, err
	}
	return claims, nil
}

func checkAudienceCompat(aud jwt.ClaimStrings, expected string) error {
	for _, candidate := range aud {
		if candidate == expected {
			return nil
		}
	}
	return ErrInvalidToken
}

// IssueStepUpToken signs a step-up JWT for a specific scope. ttl should be
// short — 60s is a sensible default, and 5 min is the practical ceiling.
// Issued by the API after a re-auth challenge; consumed by
// downstream services via ParseStepUpToken on the X-Step-Up-Token header.
func (s *TokenService) IssueStepUpToken(principalID uint64, scope string, ttl time.Duration) (string, error) {
	scope = strings.TrimSpace(scope)
	if principalID == 0 || scope == "" || ttl <= 0 || ttl > MaxStepUpTTL {
		return "", ErrInvalidStepUp
	}
	now := s.currentTime()
	claims := StepUpClaims{
		PrincipalID: principalID,
		Scope:       scope,
		TokenType:   "step_up",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// ParseStepUpToken verifies signature and that this is in fact a step-up
// token (rejects platform/tenant tokens replayed in this slot). Caller is
// expected to additionally check that claims.Scope matches the action being
// performed; mismatched scopes are a security-relevant misuse, not a
// validation error here.
func (s *TokenService) ParseStepUpToken(tokenStr string) (*StepUpClaims, error) {
	claims := &StepUpClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, s.keyFunc, jwt.WithTimeFunc(s.currentTime))
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.TokenType != "step_up" || claims.PrincipalID == 0 || claims.Scope == "" {
		return nil, ErrWrongType
	}
	if err := checkAudienceCompat(claims.Audience, s.audience); err != nil {
		return nil, err
	}
	return claims, nil
}

// IsExpiringSoon reports whether the token represented by the given claims will
// expire within threshold. It mirrors bybys.common.util.JwtUtil.isTokenExpiringSoon
// (JwtUtil.java:473-491) which uses a 30-minute threshold; callers should pass
// 30*time.Minute for full parity, or a service-specific warning threshold.
//
// The method accepts any jwt.Claims whose ExpiresAt is populated (i.e.
// PlatformClaims, TenantClaims, or StepUpClaims all embed jwt.RegisteredClaims
// which implements this interface).
//
// Edge cases:
//   - Returns false if claims.GetExpirationTime returns error or nil mc.
//   - If mc.Time is the zero value, the call returns true (token is treated
//     as already-expiring); zero exp is malformed and callers should refresh.
//   - Uses wall-clock time.Until. For test code that pins TokenService.now
//     to a fixture time, use (s *TokenService).IsExpiringSoon instead to
//     keep clock semantics consistent between Parse* and IsExpiringSoon.
func IsExpiringSoon(claims jwt.Claims, threshold time.Duration) bool {
	mc, err := claims.GetExpirationTime()
	if err != nil || mc == nil {
		return false
	}
	if mc.Time.IsZero() {
		return true
	}
	return time.Until(mc.Time) < threshold
}

// IsExpiringSoon is the TokenService-bound variant that uses the service's
// injectable clock (s.currentTime) instead of wall-clock time.Now. Tests
// that pin svc.now for fixture-time determinism should call this method so
// expiry checks consult the same time source as token parsing.
//
// Production behavior matches the top-level IsExpiringSoon because s.now is
// nil there → currentTime() falls through to time.Now.
//
// Edge cases match the top-level function: zero mc.Time → treated as
// already-expiring (returns true).
func (s *TokenService) IsExpiringSoon(claims jwt.Claims, threshold time.Duration) bool {
	mc, err := claims.GetExpirationTime()
	if err != nil || mc == nil {
		return false
	}
	if mc.Time.IsZero() {
		return true
	}
	return mc.Time.Sub(s.currentTime()) < threshold
}

// SelfTest performs an end-to-end sign-then-parse round-trip using a sentinel
// principal to verify that the token service is correctly configured (secret
// is loaded, algorithm is HS256, system clock is sane). It mirrors the
// healthStatus round-trip in bybys.common.util.JwtUtil.getHealthStatus
// (JwtUtil.java:140-168) and is consumed by the /healthz/jwt endpoint.
//
// Returns nil on success; a non-nil error if signing or parsing fails.
func (s *TokenService) SelfTest() error {
	const sentinelID = uint64(999999)
	tok, err := s.IssuePlatformToken(sentinelID, []string{"_selftest"}, 0)
	if err != nil {
		return fmt.Errorf("auth: SelfTest sign failed: %w", err)
	}
	claims, err := s.ParsePlatformToken(tok)
	if err != nil {
		return fmt.Errorf("auth: SelfTest parse failed: %w", err)
	}
	if claims.PrincipalID != sentinelID {
		return fmt.Errorf("auth: SelfTest principal_id mismatch: got %d", claims.PrincipalID)
	}
	return nil
}

func (s *TokenService) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}
	return s.secret, nil
}
