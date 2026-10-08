package webhook

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// signingVector is the JSON schema for each row in signing_vectors.json.
// It is the source of truth consumed by WebhookSigningConformanceTest.java.
type signingVector struct {
	// Human-readable name for the test case.
	Name string `json:"name"`
	// HMAC secret used for signing / correct verification.
	// For the wrong_secret_mismatch vector, this is the CORRECT key the verifier
	// must use; the header was signed with a different (wrong) key.
	Secret string `json:"secret"`
	// Unix timestamp in seconds embedded in the header.
	TimestampS int64 `json:"timestamp_s"`
	// BodyHex is set for vectors whose body contains non-UTF-8 bytes.
	// Java consumers MUST decode this hex into raw bytes instead of using Body.
	BodyHex string `json:"body_hex,omitempty"`
	// Body is the raw body as a UTF-8 string. For binary-body vectors Body may
	// round-trip through JSON with unicode escapes; prefer BodyHex in those
	// cases.
	Body string `json:"body"`
	// Header is the full "t=<ts>,v1=<hex>" header value produced by SignHeader.
	Header string `json:"header"`
	// Sig is just the hex MAC — ComputeSignature output — without the "v1=" prefix.
	// For tampered vectors (wrong_secret_mismatch, tampered_body, tampered_timestamp)
	// this is the ORIGINAL sig (before tampering); it will not match
	// ComputeSignature(secret, timestamp_s, body) because one of those inputs differs.
	Sig string `json:"sig"`
	// WantVerify is true when VerifyHeader(secret, header, body, now_s, max_age_s)
	// should return nil, and false when it should return an error.
	WantVerify bool `json:"want_verify"`
	// Note is a human-readable explanation of the case.
	Note string `json:"note"`
	// MaxAgeS is the tolerance in seconds passed to VerifyHeader.
	// 0 means no skew check (unit-test-only mode per §3.4 of the SDK contract).
	MaxAgeS int64 `json:"max_age_s"`
	// NowS is the Unix timestamp that should be passed as "now" to VerifyHeader.
	NowS int64 `json:"now_s"`
	// SkipSignTest is true for vectors that deliberately carry a mismatched (secret,
	// timestamp_s, body, sig) tuple — their purpose is to exercise verifier rejection
	// paths, not ComputeSignature / SignHeader correctness. Java conformance tests
	// MUST skip the computeSignature / sign sub-tests for these rows.
	SkipSignTest bool `json:"skip_sign_test,omitempty"`
}

// fixtureRoot is the top-level JSON structure for signing_vectors.json.
type fixtureRoot struct {
	Version       int             `json:"version"`
	ReferenceImpl string          `json:"reference_impl"`
	Note          string          `json:"note"`
	Vectors       []signingVector `json:"signing_vectors"`
}

const (
	fixtureSecret      = "test-secret-key"
	fixtureWrongSecret = "wrong-secret"
	fixtureBaseTS      = int64(1_777_000_000)
	fixtureNow         = fixtureBaseTS
)

// buildSigningVectors returns the canonical set of signing test vectors.
// All signatures are computed via ComputeSignature (the frozen reference).
// Minimum 14 cases are required; the Java conformance test asserts >= 10.
func buildSigningVectors() []signingVector {
	ts := fixtureBaseTS
	now := fixtureNow
	secret := fixtureSecret

	sign := func(sec string, t int64, body []byte) (string, string) {
		h, _ := SignHeader(sec, time.Unix(t, 0), body)
		return h, ComputeSignature(sec, t, body)
	}

	var vv []signingVector

	// ── 1. standard JSON payload ────────────────────────────────────────────
	body1 := []byte(`{"event":"order.created","order_id":"ORD-001"}`)
	h1, s1 := sign(secret, ts, body1)
	vv = append(vv, signingVector{
		Name: "standard_json_payload", Secret: secret, TimestampS: ts,
		Body: string(body1), Header: h1, Sig: s1,
		WantVerify: true, Note: "standard JSON payload signs and verifies",
		MaxAgeS: 300, NowS: now,
	})

	// ── 2. empty body ───────────────────────────────────────────────────────
	body2 := []byte{}
	h2, s2 := sign(secret, ts, body2)
	vv = append(vv, signingVector{
		Name: "empty_body", Secret: secret, TimestampS: ts,
		Body: "", Header: h2, Sig: s2,
		WantVerify: true, Note: "empty body is valid; MAC covers only the timestamp prefix",
		MaxAgeS: 300, NowS: now,
	})

	// ── 3. UTF-8 Chinese body ───────────────────────────────────────────────
	body3 := []byte(`{"msg":"测试"}`)
	h3, s3 := sign(secret, ts, body3)
	vv = append(vv, signingVector{
		Name: "utf8_chinese_body", Secret: secret, TimestampS: ts,
		Body: string(body3), Header: h3, Sig: s3,
		WantVerify: true,
		Note:       "UTF-8 Chinese body bytes used as-is; no charset conversion allowed",
		MaxAgeS:    300, NowS: now,
	})

	// ── 4. timestamp in window (4 min ago) ──────────────────────────────────
	ts4 := ts - 240 // 4 minutes old
	body4 := []byte(`{"event":"payment.settled"}`)
	h4, s4 := sign(secret, ts4, body4)
	vv = append(vv, signingVector{
		Name: "timestamp_in_window", Secret: secret, TimestampS: ts4,
		Body: string(body4), Header: h4, Sig: s4,
		WantVerify: true, Note: "timestamp 240s (4 min) old is within 300s (5 min) tolerance window",
		MaxAgeS: 300, NowS: now,
	})

	// ── 5. timestamp expired (10 min ago) ───────────────────────────────────
	ts5 := ts - 600 // 10 minutes old
	body5 := []byte(`{"event":"payment.settled"}`)
	h5, s5 := sign(secret, ts5, body5)
	vv = append(vv, signingVector{
		Name: "timestamp_expired", Secret: secret, TimestampS: ts5,
		Body: string(body5), Header: h5, Sig: s5,
		WantVerify: false,
		Note:       "timestamp 600s (10 min) old exceeds 300s tolerance; verify must reject with timestamp_skew",
		MaxAgeS:    300, NowS: now,
	})

	// ── 6. wrong secret → mismatch ──────────────────────────────────────────
	// Header is signed with the wrong key; the vector's Secret field is the
	// CORRECT key that the verifier must use. Since header MAC ≠ recomputed MAC
	// under the correct key, VerifyHeader must return ErrInvalidSignature.
	body6 := []byte(`{"event":"transfer.completed"}`)
	h6, s6 := sign(fixtureWrongSecret, ts, body6) // signed with wrong key
	vv = append(vv, signingVector{
		Name: "wrong_secret_mismatch", Secret: secret, TimestampS: ts,
		// Body and Header intentionally mismatched: header carries MAC from wrong key
		Body: string(body6), Header: h6, Sig: s6,
		WantVerify: false,
		Note:       "header signed with 'wrong-secret'; verifier uses 'test-secret-key'; MAC mismatch => invalid_signature",
		MaxAgeS:    300, NowS: now,
		SkipSignTest: true, // Sig was computed with wrong key; computeSignature(secret,ts,body) differs
	})

	// ── 7. tampered body ────────────────────────────────────────────────────
	body7orig := []byte(`{"amount":100}`)
	body7tampered := []byte(`{"amount":999}`)
	h7, s7 := sign(secret, ts, body7orig) // header signed over original
	vv = append(vv, signingVector{
		Name: "tampered_body", Secret: secret, TimestampS: ts,
		Body:   string(body7tampered), // body that reaches verifier is different
		Header: h7, Sig: s7,
		WantVerify: false,
		Note:       "body tampered after signing; verifier must reject with invalid_signature",
		MaxAgeS:    300, NowS: now,
		SkipSignTest: true, // Sig is over body7orig; computeSignature(secret,ts,body7tampered) differs
	})

	// ── 8. tampered timestamp ───────────────────────────────────────────────
	// Header carries altered timestamp, but MAC was computed over original ts.
	body8 := []byte(`{"event":"refund.initiated"}`)
	_, s8 := sign(secret, ts, body8)                     // MAC over original ts
	h8tampered := fmt.Sprintf("t=%d,v1=%s", ts+9999, s8) // header ts altered
	vv = append(vv, signingVector{
		Name: "tampered_timestamp", Secret: secret, TimestampS: ts + 9999,
		Body: string(body8), Header: h8tampered, Sig: s8,
		WantVerify: false,
		Note:       "timestamp field in header altered; recomputed MAC over altered ts will not match original MAC",
		MaxAgeS:    300, NowS: ts + 9999, // set now to altered ts to pass skew but fail MAC
		SkipSignTest: true, // Sig is over original ts; computeSignature(secret,ts+9999,body8) differs
	})

	// ── 9. binary body (0x00–0x0f) ──────────────────────────────────────────
	body9 := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	h9, s9 := sign(secret, ts, body9)
	vv = append(vv, signingVector{
		Name: "binary_body", Secret: secret, TimestampS: ts,
		BodyHex: hex.EncodeToString(body9),
		Body:    string(body9), Header: h9, Sig: s9,
		WantVerify: true,
		Note:       "body is raw bytes 0x00-0x0f; Java test must decode body_hex into byte[] and not use the body string",
		MaxAgeS:    300, NowS: now,
	})

	// ── 10. large body (> 1 KB) ─────────────────────────────────────────────
	longBody := []byte(`{"data":"`)
	for i := 0; i < 1080; i++ {
		longBody = append(longBody, byte('a'+i%26))
	}
	longBody = append(longBody, '"', '}')
	h10, s10 := sign(secret, ts, longBody)
	vv = append(vv, signingVector{
		Name: "large_body", Secret: secret, TimestampS: ts,
		Body: string(longBody), Header: h10, Sig: s10,
		WantVerify: true, Note: "large body >1 KB; signing is not length-limited",
		MaxAgeS: 300, NowS: now,
	})

	// ── 11. tolerance=0 disables skew check ─────────────────────────────────
	ts11 := ts - 86400 // 1 day ago
	body11 := []byte(`{"event":"archived"}`)
	h11, s11 := sign(secret, ts11, body11)
	vv = append(vv, signingVector{
		Name: "zero_tolerance_no_skew_check", Secret: secret, TimestampS: ts11,
		Body: string(body11), Header: h11, Sig: s11,
		WantVerify: true,
		Note:       "tolerance=0 disables skew check (unit-test-only mode per §3.4); any valid MAC passes regardless of age",
		MaxAgeS:    0, NowS: now,
	})

	// ── 12. boundary: 299 s old → pass ──────────────────────────────────────
	ts12 := ts - 299
	body12 := []byte(`{"event":"boundary.test"}`)
	h12, s12 := sign(secret, ts12, body12)
	vv = append(vv, signingVector{
		Name: "timestamp_boundary_299s_pass", Secret: secret, TimestampS: ts12,
		Body: string(body12), Header: h12, Sig: s12,
		WantVerify: true, Note: "timestamp 299s old: abs(299) > 300 is false; ACCEPTED",
		MaxAgeS: 300, NowS: now,
	})

	// ── 13. boundary: exactly 300 s old → accepted (> not >=) ───────────────
	ts13 := ts - 300
	body13 := []byte(`{"event":"boundary.test"}`)
	h13, s13 := sign(secret, ts13, body13)
	vv = append(vv, signingVector{
		Name: "timestamp_boundary_300s_accepted", Secret: secret, TimestampS: ts13,
		Body: string(body13), Header: h13, Sig: s13,
		WantVerify: true,
		Note:       "timestamp exactly 300s old: abs(300) > 300 is false so NOT rejected by Go's strict > operator",
		MaxAgeS:    300, NowS: now,
	})

	// ── 14. boundary: 301 s old → rejected ──────────────────────────────────
	ts14 := ts - 301
	body14 := []byte(`{"event":"boundary.test"}`)
	h14, s14 := sign(secret, ts14, body14)
	vv = append(vv, signingVector{
		Name: "timestamp_boundary_301s_reject", Secret: secret, TimestampS: ts14,
		Body: string(body14), Header: h14, Sig: s14,
		WantVerify: false,
		Note:       "timestamp 301s old: abs(301) > 300 is true; REJECTED with timestamp_skew",
		MaxAgeS:    300, NowS: now,
	})

	return vv
}

// TestUpdateFixtures writes x-go/webhook/testdata/signing_vectors.json
// when invoked with -update-fixtures. It is also a correctness check: every
// vector is sign-then-verify round-tripped against the frozen Go implementation
// so a future refactor of the test data cannot silently produce a broken fixture.
func TestUpdateFixtures(t *testing.T) {
	vectors := buildSigningVectors()

	if len(vectors) < 10 {
		t.Fatalf("fixture must have ≥10 vectors; got %d", len(vectors))
	}

	// Round-trip correctness: verify every want_verify=true vector passes
	// VerifyHeader, and every want_verify=false vector fails it.
	for _, v := range vectors {
		now := time.Unix(v.NowS, 0)
		tol := time.Duration(v.MaxAgeS) * time.Second

		// Reconstruct payload: prefer BodyHex when present.
		var payload []byte
		if v.BodyHex != "" {
			var err error
			payload, err = hex.DecodeString(v.BodyHex)
			if err != nil {
				t.Fatalf("vector %q: invalid body_hex: %v", v.Name, err)
			}
		} else {
			payload = []byte(v.Body)
		}

		err := VerifyHeader(v.Secret, v.Header, payload, now, tol)
		if v.WantVerify && err != nil {
			t.Errorf("vector %q: VerifyHeader returned unexpected error: %v", v.Name, err)
		}
		if !v.WantVerify && err == nil {
			t.Errorf("vector %q: VerifyHeader returned nil but expected an error", v.Name)
		}
	}

	if !*updateFixtures {
		return
	}

	const fixtureDir = "testdata"
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}

	root := fixtureRoot{
		Version:       1,
		ReferenceImpl: "x-go/webhook/webhook.go",
		Note:          "Regenerate via: cd x-go/webhook && go test -run TestUpdateFixtures -update-fixtures .",
		Vectors:       vectors,
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	data = append(data, '\n')

	const fixturePath = "testdata/signing_vectors.json"
	if err := os.WriteFile(fixturePath, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", fixturePath, err)
	}
	t.Logf("wrote %s (%d bytes, %d vectors)", fixturePath, len(data), len(vectors))
	t.Logf("next step: copy %s to templates/game/backend/platform-core/platform-common/src/test/resources/conformance/webhook_signing_vectors.json", fixturePath)
}
