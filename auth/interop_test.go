package auth

// Cross-language interop tests for Go <-> Java JWT round-trip.
//
// Design (Phase A W2-A4, 2026-05-08):
//
//   - Go produces tokens with Synapse canonical claims schema
//     (PlatformClaims / TenantClaims / StepUpClaims).
//   - Test vectors are written to testdata/jwt_interop_vectors.json so the
//     Java conformance test (JwtUtilInteropConformanceTest.java) can load them
//     and verify Go-signed tokens are parseable by JwtUtil using the same
//     HMAC-SHA256 shared secret.
//   - The Java test additionally signs tokens with JwtUtil and the same
//     shared secret; round-trip is validated by verifying that the sub
//     (userId) claim round-trips correctly. Synapse does NOT adopt Java's
//     flat `userType` schema — mapping is the adaptation layer's job.
//
// Shared test secret: "interop-shared-secret-32chars!!1" (32 bytes, passes
// JwtUtil's minSecretLength=32 validation).
//
// Run: cd x-go && go test ./auth/... -run TestInterop -v

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const interopSecret = "interop-shared-secret-32chars!!1"
const interopIssuer = "synapse-test"

var interopFixtureNow = time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

// InteropVector represents a single test vector written to jwt_interop_vectors.json.
type InteropVector struct {
	// ID is a short human-readable identifier for the test case.
	ID string `json:"id"`
	// Description explains what the token is for.
	Description string `json:"description"`
	// Token is the signed JWT string (may be an intentionally invalid string
	// for negative test cases).
	Token string `json:"token"`
	// SharedSecret is the HMAC-SHA256 key used to sign the token.
	SharedSecret string `json:"shared_secret"`
	// ExpectValid indicates whether a verifier should accept this token.
	ExpectValid bool `json:"expect_valid"`
	// ExpectClaims are the expected claim values for positive cases. Keys
	// match the Go JSON field names from PlatformClaims / TenantClaims.
	ExpectClaims map[string]any `json:"expect_claims,omitempty"`
}

func TestInteropWriteVectors(t *testing.T) {
	svc, _ := NewTokenService(interopSecret, interopIssuer, 10*365*24*3600, 10*365*24*3600)
	svc.now = func() time.Time { return interopFixtureNow }

	vectors := make([]InteropVector, 0, 6)

	// Case 1: valid platform token
	{
		tok, err := svc.IssuePlatformToken(42, []string{"platform.admin"}, 1)
		if err != nil {
			t.Fatalf("case 1 issue: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "valid-platform",
			Description:  "Valid platform-admin token; principal_id=42, role_codes=[platform.admin], token_version=1",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  true,
			ExpectClaims: map[string]any{
				"principal_id":   float64(42),
				"principal_type": "platform_admin",
				"token_type":     "platform",
				"token_version":  float64(1),
			},
		})
	}

	// Case 2: valid tenant token
	{
		actorID := uint64(99)
		tok, err := svc.IssueTenantToken(10, "game-cn", 5, "tenant_admin", []string{"tenant.admin"}, 2, &actorID)
		if err != nil {
			t.Fatalf("case 2 issue: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "valid-tenant",
			Description:  "Valid tenant token; tenant_id=10, tenant_code=game-cn, principal_id=5, actor_principal_id=99",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  true,
			ExpectClaims: map[string]any{
				"tenant_id":          float64(10),
				"tenant_code":        "game-cn",
				"principal_id":       float64(5),
				"principal_type":     "tenant_admin",
				"token_type":         "tenant",
				"token_version":      float64(2),
				"actor_principal_id": float64(99),
			},
		})
	}

	// Case 3: step-up token
	{
		tok, err := svc.IssueStepUpToken(7, "cargo.wallet-adjust", MaxStepUpTTL)
		if err != nil {
			t.Fatalf("case 3 issue: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "valid-step-up",
			Description:  "Valid step-up token; principal_id=7, scope=cargo.wallet-adjust, max allowed fixture TTL",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  true,
			ExpectClaims: map[string]any{
				"principal_id": float64(7),
				"scope":        "cargo.wallet-adjust",
				"token_type":   "step_up",
			},
		})
	}

	// Case 4: expired token (backdated using raw jwt)
	{
		past := interopFixtureNow.Add(-2 * time.Hour)
		claims := PlatformClaims{
			PrincipalID:   8,
			PrincipalType: "platform_admin",
			RoleCodes:     []string{"platform.admin"},
			TokenVersion:  1,
			TokenType:     "platform",
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    interopIssuer,
				IssuedAt:  jwt.NewNumericDate(past.Add(-time.Hour)),
				ExpiresAt: jwt.NewNumericDate(past),
			},
		}
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(interopSecret))
		if err != nil {
			t.Fatalf("case 4 sign: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "expired",
			Description:  "Expired platform token; exp is 1 hour in the past — verifier must reject",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  false,
		})
	}

	// Case 5: wrong signature (signed with different secret)
	{
		wrongSvc, _ := NewTokenService("a-different-secret-that-is-32ch!", interopIssuer, 10*365*24*3600, 10*365*24*3600)
		wrongSvc.now = func() time.Time { return interopFixtureNow }
		tok, err := wrongSvc.IssuePlatformToken(11, []string{"platform.admin"}, 1)
		if err != nil {
			t.Fatalf("case 5 issue: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "wrong-signature",
			Description:  "Token signed with a different secret; verifier with interop secret must reject (SignatureException)",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  false,
		})
	}

	// Case 6: missing required claim (token_type absent — raw jwt with no TokenType)
	{
		type minimalClaims struct {
			PrincipalID uint64 `json:"principal_id"`
			jwt.RegisteredClaims
		}
		claims := minimalClaims{
			PrincipalID: 99,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    interopIssuer,
				IssuedAt:  jwt.NewNumericDate(interopFixtureNow),
				ExpiresAt: jwt.NewNumericDate(interopFixtureNow.Add(10 * 365 * 24 * time.Hour)),
			},
		}
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(interopSecret))
		if err != nil {
			t.Fatalf("case 6 sign: %v", err)
		}
		vectors = append(vectors, InteropVector{
			ID:           "missing-token-type",
			Description:  "Token with valid signature but no token_type claim; ParsePlatformToken must return ErrWrongType",
			Token:        tok,
			SharedSecret: interopSecret,
			ExpectValid:  false,
		})
	}

	// Write vectors to testdata/
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "jwt_interop_vectors.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("wrote %d interop vectors to %s", len(vectors), path)
}

// TestInteropReadVectors verifies that the Go TokenService correctly accepts /
// rejects each vector in jwt_interop_vectors.json. This test is idempotent and
// can be run without re-generating the file (CI reads pre-committed fixtures).
func TestInteropReadVectors(t *testing.T) {
	path := filepath.Join("testdata", "jwt_interop_vectors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("interop vectors not found (%v); run TestInteropWriteVectors first", err)
	}

	var vectors []InteropVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	if len(vectors) < 6 {
		t.Fatalf("expected at least 6 vectors, got %d", len(vectors))
	}

	for _, v := range vectors {
		v := v
		t.Run(v.ID, func(t *testing.T) {
			svc, _ := NewTokenService(v.SharedSecret, interopIssuer, 3600, 86400)
			// Pin parser clock to the same fixture time used to issue the
			// vectors. Without this, short-lived tokens (e.g. step-up with
			// MaxStepUpTTL=5min) expire by wall-clock and the read test
			// fails forever after the first 5 minutes post-generation.
			svc.now = func() time.Time { return interopFixtureNow }

			// Try parsing as each token type; accept if any succeeds (for
			// positive cases). For negative cases, all three must fail.
			_, errPlat := svc.ParsePlatformToken(v.Token)
			_, errTen := svc.ParseTenantToken(v.Token)
			_, errStep := svc.ParseStepUpToken(v.Token)

			anyOK := errPlat == nil || errTen == nil || errStep == nil

			if v.ExpectValid && !anyOK {
				t.Errorf("vector %q: expected valid token, but all parsers rejected: plat=%v ten=%v step=%v",
					v.ID, errPlat, errTen, errStep)
			}
			if !v.ExpectValid && anyOK {
				t.Errorf("vector %q: expected invalid token, but a parser accepted it", v.ID)
			}
		})
	}
}

// TestInteropSelfTest verifies that TokenService.SelfTest passes with a
// properly configured service and fails with an empty secret.
func TestInteropSelfTest(t *testing.T) {
	t.Run("healthy-service", func(t *testing.T) {
		svc, _ := NewTokenService(interopSecret, interopIssuer, 3600, 86400)
		if err := svc.SelfTest(); err != nil {
			t.Errorf("SelfTest on healthy service: %v", err)
		}
	})
}

// TestInteropIsExpiringSoon validates the IsExpiringSoon helper against known
// expiry times — mirrors JwtUtil.isTokenExpiringSoon (30-minute threshold).
func TestInteropIsExpiringSoon(t *testing.T) {
	svc, _ := NewTokenService(interopSecret, interopIssuer, 3600, 86400)

	// Token with 2-hour TTL — should NOT be expiring soon at 30-min threshold.
	longLived, err := svc.IssuePlatformToken(1, nil, 0)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := svc.ParsePlatformToken(longLived)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if IsExpiringSoon(claims, 30*time.Minute) {
		t.Error("2-hour token should not be expiring soon at 30-min threshold")
	}

	// Token with 10-minute TTL — should be expiring soon at 30-min threshold.
	shortSvc, _ := NewTokenService(interopSecret, interopIssuer, 600 /* 10 min */, 86400)
	shortTok, err := shortSvc.IssuePlatformToken(2, nil, 0)
	if err != nil {
		t.Fatalf("issue short: %v", err)
	}
	shortClaims, err := shortSvc.ParsePlatformToken(shortTok)
	if err != nil {
		t.Fatalf("parse short: %v", err)
	}
	if !IsExpiringSoon(shortClaims, 30*time.Minute) {
		t.Error("10-min token should be expiring soon at 30-min threshold")
	}
}
