package utils

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zanehu-ai/x-go/job"
)

// updateFixtures lives in fixture_flags_test.go (canonical home for the
// shared `-update-fixtures` flag in this package).

// backoffFixturesPath is the on-disk location of the shared backoff parity
// vectors. Both the Go side (this file) and the Java side
// (templates/game/.../ExponentialBackoffCalculatorParityTest.java) read this
// exact file — the Java test walks up from its working directory until it
// finds x-go/utils/testdata/, so there is exactly one copy.
const backoffFixturesPath = "testdata/backoff_vectors.json"

// TestCalculateBackoffJavaParity asserts the Java
// ExponentialBackoffCalculator.calculateBackoffMinutes table is reproduced
// exactly by the Go wrapper.
//
// Java reference (initial=10m, multiplier=2):
//
//	retryCount=0 -> 10m  (Java: retryCount<=0 returns initial)
//	retryCount=1 -> 10m
//	retryCount=2 -> 20m
//	retryCount=3 -> 40m
//	retryCount=4 -> 80m
//	retryCount=5 -> 160m
func TestCalculateBackoffJavaParity(t *testing.T) {
	calc := DefaultCalculator()
	cases := []struct {
		name       string
		retryCount int
		want       time.Duration
	}{
		{"retryCount=0 returns initial (Java <=0 branch)", 0, 10 * time.Minute},
		{"retryCount=1 first retry", 1, 10 * time.Minute},
		{"retryCount=2 second retry", 2, 20 * time.Minute},
		{"retryCount=3 third retry", 3, 40 * time.Minute},
		{"retryCount=4 fourth retry", 4, 80 * time.Minute},
		{"retryCount=5 fifth retry", 5, 160 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calc.CalculateBackoff(tc.retryCount)
			if err != nil {
				t.Fatalf("CalculateBackoff(%d) returned error: %v", tc.retryCount, err)
			}
			if got != tc.want {
				t.Fatalf("CalculateBackoff(%d) = %v, want %v", tc.retryCount, got, tc.want)
			}
		})
	}
}

// TestCalculateBackoffDeterministic confirms repeated calls with the same
// retryCount produce identical durations (no jitter, no hidden state). This is
// the wrapper-path equivalent of the Java "no randomness" invariant.
func TestCalculateBackoffDeterministic(t *testing.T) {
	calc := DefaultCalculator()
	first, err := calc.CalculateBackoff(3)
	if err != nil {
		t.Fatalf("CalculateBackoff(3) returned error: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := calc.CalculateBackoff(3)
		if err != nil {
			t.Fatalf("CalculateBackoff(3) iteration %d returned error: %v", i, err)
		}
		if got != first {
			t.Fatalf("CalculateBackoff(3) drifted at iteration %d: got %v, want %v", i, got, first)
		}
	}
}

// TestCalculateBackoffCapReached pins the Max-cap behaviour that the Go
// BackoffPolicy adds on top of Java semantics. With a 1h cap, retryCount=10
// (which would compute 10m * 2^9 = 5120m without a cap) must clamp to 1h.
func TestCalculateBackoffCapReached(t *testing.T) {
	calc, err := NewCalculator(10*time.Minute, 2, time.Hour, 5)
	if err != nil {
		t.Fatalf("NewCalculator returned error: %v", err)
	}
	got, err := calc.CalculateBackoff(10)
	if err != nil {
		t.Fatalf("CalculateBackoff(10) returned error: %v", err)
	}
	if got != time.Hour {
		t.Fatalf("CalculateBackoff(10) with 1h cap = %v, want %v", got, time.Hour)
	}
}

// TestCalculateBackoffRejectsNegative asserts the explicit-error policy
// described in the doc comment for ErrNegativeRetryCount: Go callers prefer an
// error over Java's silent <=0 fallthrough.
func TestCalculateBackoffRejectsNegative(t *testing.T) {
	calc := DefaultCalculator()
	_, err := calc.CalculateBackoff(-1)
	if !errors.Is(err, ErrNegativeRetryCount) {
		t.Fatalf("CalculateBackoff(-1) error = %v, want ErrNegativeRetryCount", err)
	}
}

// TestNewCalculatorCustomMultiplier exercises a non-default multiplier (3 ×)
// to confirm the wrapper passes the parameter through to job.BackoffPolicy
// rather than hard-coding 2.
//
// initial=1s, multiplier=3, max=1h:
//
//	retryCount=1 -> 1s * 3^0 = 1s
//	retryCount=2 -> 1s * 3^1 = 3s
//	retryCount=3 -> 1s * 3^2 = 9s
//	retryCount=4 -> 1s * 3^3 = 27s
func TestNewCalculatorCustomMultiplier(t *testing.T) {
	calc, err := NewCalculator(time.Second, 3, time.Hour, 5)
	if err != nil {
		t.Fatalf("NewCalculator returned error: %v", err)
	}
	cases := []struct {
		retryCount int
		want       time.Duration
	}{
		{1, time.Second},
		{2, 3 * time.Second},
		{3, 9 * time.Second},
		{4, 27 * time.Second},
	}
	for _, tc := range cases {
		got, err := calc.CalculateBackoff(tc.retryCount)
		if err != nil {
			t.Fatalf("CalculateBackoff(%d) returned error: %v", tc.retryCount, err)
		}
		if got != tc.want {
			t.Fatalf("multiplier=3 CalculateBackoff(%d) = %v, want %v", tc.retryCount, got, tc.want)
		}
	}
}

// TestNewCalculatorRejectsInvalidPolicy asserts validation errors from the
// underlying job.BackoffPolicy bubble up unchanged (we don't want to mask
// invalid base/max combos).
func TestNewCalculatorRejectsInvalidPolicy(t *testing.T) {
	_, err := NewCalculator(10*time.Second, 2, time.Second, 5)
	if !errors.Is(err, job.ErrInvalidBackoffPolicy) {
		t.Fatalf("NewCalculator with base>max error = %v, want ErrInvalidBackoffPolicy", err)
	}
}

// TestNewCalculatorRejectsNegativeMaxRetry confirms the wrapper-specific
// validation for max retry count.
func TestNewCalculatorRejectsNegativeMaxRetry(t *testing.T) {
	_, err := NewCalculator(time.Second, 2, time.Hour, -1)
	if !errors.Is(err, ErrNegativeRetryCount) {
		t.Fatalf("NewCalculator with maxRetry=-1 error = %v, want ErrNegativeRetryCount", err)
	}
}

// TestCrossPackageEquivalenceWithJobPolicy is the load-bearing parity test:
// for every retryCount, Calculator must return the same duration as a directly
// configured job.BackoffPolicy.Delay using the documented (retryCount-1)
// mapping.
func TestCrossPackageEquivalenceWithJobPolicy(t *testing.T) {
	calc := DefaultCalculator()
	policy := calc.Policy()

	for retryCount := 0; retryCount <= 8; retryCount++ {
		got, err := calc.CalculateBackoff(retryCount)
		if err != nil {
			t.Fatalf("CalculateBackoff(%d) returned error: %v", retryCount, err)
		}

		// Java retryCount<=0 maps to attempt=0; retryCount>=1 maps to
		// retryCount-1. Calculator implements this; the test mirrors it
		// to assert the contract directly.
		attempt := retryCount - 1
		if attempt < 0 {
			attempt = 0
		}
		want, err := policy.Delay(attempt)
		if err != nil {
			t.Fatalf("policy.Delay(%d) returned error: %v", attempt, err)
		}
		if got != want {
			t.Fatalf("retryCount=%d: Calculator=%v, job.BackoffPolicy.Delay(attempt=%d)=%v",
				retryCount, got, attempt, want)
		}
	}
}

// TestCalculateNextRetryTime asserts the next-retry-time computation matches
// Java calculateNextRetryTime semantics: lastRetryTime + backoff.
func TestCalculateNextRetryTime(t *testing.T) {
	calc := DefaultCalculator()
	last := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	got, err := calc.CalculateNextRetryTime(last, 3)
	if err != nil {
		t.Fatalf("CalculateNextRetryTime returned error: %v", err)
	}
	want := last.Add(40 * time.Minute) // retryCount=3 -> 40m per Java table
	if !got.Equal(want) {
		t.Fatalf("CalculateNextRetryTime = %v, want %v", got, want)
	}
}

// TestCalculateNextRetryTimeZeroLast covers the Java
// `if (lastRetryTime == null) lastRetryTime = LocalDateTime.now()` branch by
// passing the Go zero value and asserting the result is "near now + delay".
func TestCalculateNextRetryTimeZeroLast(t *testing.T) {
	calc := DefaultCalculator()
	before := time.Now().UTC()
	got, err := calc.CalculateNextRetryTime(time.Time{}, 1)
	if err != nil {
		t.Fatalf("CalculateNextRetryTime returned error: %v", err)
	}
	after := time.Now().UTC()

	earliest := before.Add(10 * time.Minute)
	latest := after.Add(10*time.Minute + time.Second) // 1s slack
	if got.Before(earliest) || got.After(latest) {
		t.Fatalf("CalculateNextRetryTime with zero last = %v, expected within [%v, %v]",
			got, earliest, latest)
	}
}

// TestShouldRetryAndIsPermanentFailure exercises the boundary between "still
// retrying" and "permanent failure" using the default max=5.
func TestShouldRetryAndIsPermanentFailure(t *testing.T) {
	calc := DefaultCalculator()
	cases := []struct {
		retryCount      int
		wantShouldRetry bool
		wantPermanent   bool
	}{
		{-1, true, false}, // negative coerced to 0
		{0, true, false},
		{1, true, false},
		{4, true, false},
		{5, false, true}, // boundary: retryCount == max -> permanent
		{6, false, true},
	}
	for _, tc := range cases {
		if got := calc.ShouldRetry(tc.retryCount); got != tc.wantShouldRetry {
			t.Errorf("ShouldRetry(%d) = %v, want %v", tc.retryCount, got, tc.wantShouldRetry)
		}
		if got := calc.IsPermanentFailure(tc.retryCount); got != tc.wantPermanent {
			t.Errorf("IsPermanentFailure(%d) = %v, want %v", tc.retryCount, got, tc.wantPermanent)
		}
	}
}

// TestMaxRetryCountAccessor is a sanity check that the configured threshold is
// readable, since callers may need to surface it in logs/UI.
func TestMaxRetryCountAccessor(t *testing.T) {
	calc, err := NewCalculator(time.Second, 2, time.Hour, 7)
	if err != nil {
		t.Fatalf("NewCalculator returned error: %v", err)
	}
	if got := calc.MaxRetryCount(); got != 7 {
		t.Fatalf("MaxRetryCount() = %d, want 7", got)
	}
	if got := DefaultCalculator().MaxRetryCount(); got != DefaultMaxRetryCount {
		t.Fatalf("DefaultCalculator MaxRetryCount() = %d, want %d", got, DefaultMaxRetryCount)
	}
}

// backoffFixtureFile mirrors the shape of testdata/backoff_vectors.json. The
// Go reference computes every expected value using DefaultCalculator (or, for
// should_retry / is_permanent_failure, a per-fixture custom max). Java's
// ExponentialBackoffCalculatorParityTest loads the same on-disk file by walking
// up from its working directory until it finds x-go/utils/testdata/, so
// there is exactly one copy of the fixture set — that is the parity check
// (path ③ of the C4b cross-language validation strategy).
type backoffFixtureFile struct {
	Version            int                         `json:"version"`
	ReferenceImpl      string                      `json:"reference_impl"`
	Description        string                      `json:"description,omitempty"`
	Params             backoffFixtureParams        `json:"params"`
	CalculateBackoff   []calculateBackoffFixture   `json:"calculate_backoff"`
	ShouldRetry        []shouldRetryFixture        `json:"should_retry"`
	IsPermanentFailure []isPermanentFailureFixture `json:"is_permanent_failure"`
}

type backoffFixtureParams struct {
	InitialDelayMs       int64   `json:"initialDelayMs"`
	Multiplier           float64 `json:"multiplier"`
	MaxDelayMs           int64   `json:"maxDelayMs"`
	DefaultMaxRetryCount int     `json:"defaultMaxRetryCount"`
}

type calculateBackoffFixture struct {
	Name            string `json:"name"`
	RetryCount      int    `json:"retryCount"`
	ExpectedDelayMs int64  `json:"expected_delay_ms"`
	Note            string `json:"note,omitempty"`
}

type shouldRetryFixture struct {
	Name       string `json:"name"`
	RetryCount int    `json:"retryCount"`
	Max        int    `json:"max"`
	Expected   bool   `json:"expected"`
	Note       string `json:"note,omitempty"`
}

type isPermanentFailureFixture struct {
	Name       string `json:"name"`
	RetryCount int    `json:"retryCount"`
	Max        int    `json:"max"`
	Expected   bool   `json:"expected"`
	Note       string `json:"note,omitempty"`
}

// loadBackoffFixtures parses testdata/backoff_vectors.json. It is split out so
// the assertion test and the regenerator share one parser.
func loadBackoffFixtures(t *testing.T) backoffFixtureFile {
	t.Helper()
	raw, err := os.ReadFile(backoffFixturesPath)
	if err != nil {
		t.Fatalf("read %s: %v", backoffFixturesPath, err)
	}
	var f backoffFixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal %s: %v", backoffFixturesPath, err)
	}
	return f
}

// TestBackoffJSONFixtureRoundTrip is the load-bearing parity gate for path ③.
// It re-asserts every fixture in testdata/backoff_vectors.json against the Go
// reference implementation. When run with `-update-fixtures` it rewrites the
// file instead, so the Go side stays authoritative and Java is the consumer
// (the design choice in C4b Wave 1 spec §1.3).
func TestBackoffJSONFixtureRoundTrip(t *testing.T) {
	calc := DefaultCalculator()

	if *updateFixtures {
		regenerateBackoffFixtures(t, calc)
		return
	}

	f := loadBackoffFixtures(t)

	// Guard the params block so a casual edit to the JSON can't desync the
	// parameters the Java side reads.
	wantInitial := DefaultInitialInterval.Milliseconds()
	if f.Params.InitialDelayMs != wantInitial {
		t.Fatalf("params.initialDelayMs = %d, want %d (DefaultInitialInterval)", f.Params.InitialDelayMs, wantInitial)
	}
	if f.Params.Multiplier != DefaultMultiplier {
		t.Fatalf("params.multiplier = %v, want %v (DefaultMultiplier)", f.Params.Multiplier, DefaultMultiplier)
	}
	if f.Params.MaxDelayMs != defaultMax.Milliseconds() {
		t.Fatalf("params.maxDelayMs = %d, want %d (defaultMax)", f.Params.MaxDelayMs, defaultMax.Milliseconds())
	}
	if f.Params.DefaultMaxRetryCount != DefaultMaxRetryCount {
		t.Fatalf("params.defaultMaxRetryCount = %d, want %d", f.Params.DefaultMaxRetryCount, DefaultMaxRetryCount)
	}

	if len(f.CalculateBackoff) < 10 {
		t.Fatalf("calculate_backoff fixtures = %d, want >= 10 (per C4b-1.3 brief)", len(f.CalculateBackoff))
	}

	for _, fx := range f.CalculateBackoff {
		t.Run("calculate_backoff/"+fx.Name, func(t *testing.T) {
			got, err := calc.CalculateBackoff(fx.RetryCount)
			if err != nil {
				t.Fatalf("CalculateBackoff(%d) error: %v", fx.RetryCount, err)
			}
			if got.Milliseconds() != fx.ExpectedDelayMs {
				t.Fatalf("CalculateBackoff(%d) = %dms, fixture says %dms",
					fx.RetryCount, got.Milliseconds(), fx.ExpectedDelayMs)
			}
		})
	}

	for _, fx := range f.ShouldRetry {
		t.Run("should_retry/"+fx.Name, func(t *testing.T) {
			c, err := NewCalculator(DefaultInitialInterval, DefaultMultiplier, defaultMax, fx.Max)
			if err != nil {
				t.Fatalf("NewCalculator(maxRetry=%d) error: %v", fx.Max, err)
			}
			if got := c.ShouldRetry(fx.RetryCount); got != fx.Expected {
				t.Fatalf("ShouldRetry(%d, max=%d) = %v, fixture says %v",
					fx.RetryCount, fx.Max, got, fx.Expected)
			}
		})
	}

	for _, fx := range f.IsPermanentFailure {
		t.Run("is_permanent_failure/"+fx.Name, func(t *testing.T) {
			c, err := NewCalculator(DefaultInitialInterval, DefaultMultiplier, defaultMax, fx.Max)
			if err != nil {
				t.Fatalf("NewCalculator(maxRetry=%d) error: %v", fx.Max, err)
			}
			if got := c.IsPermanentFailure(fx.RetryCount); got != fx.Expected {
				t.Fatalf("IsPermanentFailure(%d, max=%d) = %v, fixture says %v",
					fx.RetryCount, fx.Max, got, fx.Expected)
			}
		})
	}
}

// regenerateBackoffFixtures rewrites testdata/backoff_vectors.json from the Go
// reference. It preserves the existing fixture *list* (names, retryCount, max)
// and recomputes only the expected_delay_ms / expected fields, so the curated
// fixture set stays stable across regenerations.
func regenerateBackoffFixtures(t *testing.T, calc Calculator) {
	t.Helper()
	f := loadBackoffFixtures(t)

	// Source every regenerated value from the provided calc — including the
	// per-fixture should_retry / is_permanent_failure calculators and the
	// final params block — so a non-default calc passed in tomorrow won't
	// silently rewrite the file with default-constant values.
	p := calc.Policy()

	for i := range f.CalculateBackoff {
		got, err := calc.CalculateBackoff(f.CalculateBackoff[i].RetryCount)
		if err != nil {
			t.Fatalf("regen CalculateBackoff(%d): %v", f.CalculateBackoff[i].RetryCount, err)
		}
		f.CalculateBackoff[i].ExpectedDelayMs = got.Milliseconds()
	}
	for i := range f.ShouldRetry {
		c, err := NewCalculator(p.Base, p.Multiplier, p.Max, f.ShouldRetry[i].Max)
		if err != nil {
			t.Fatalf("regen NewCalculator(max=%d): %v", f.ShouldRetry[i].Max, err)
		}
		f.ShouldRetry[i].Expected = c.ShouldRetry(f.ShouldRetry[i].RetryCount)
	}
	for i := range f.IsPermanentFailure {
		c, err := NewCalculator(p.Base, p.Multiplier, p.Max, f.IsPermanentFailure[i].Max)
		if err != nil {
			t.Fatalf("regen NewCalculator(max=%d): %v", f.IsPermanentFailure[i].Max, err)
		}
		f.IsPermanentFailure[i].Expected = c.IsPermanentFailure(f.IsPermanentFailure[i].RetryCount)
	}

	// Refresh params from the provided calculator so they never drift.
	f.Params.InitialDelayMs = p.Base.Milliseconds()
	f.Params.Multiplier = p.Multiplier
	f.Params.MaxDelayMs = p.Max.Milliseconds()
	f.Params.DefaultMaxRetryCount = calc.MaxRetryCount()

	// Use Encoder with SetEscapeHTML(false) so `<`, `>`, `&` in `note`
	// fields stay literal — fixture authors expect to read raw `<=`, not
	// `<=` after a regen.
	tmp, err := os.CreateTemp(filepath.Dir(backoffFixturesPath), "backoff_vectors.*.json.tmp")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&f); err != nil {
		_ = tmp.Close()
		t.Fatalf("encode fixtures: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}
	if err := os.Rename(tmp.Name(), backoffFixturesPath); err != nil {
		t.Fatalf("rename temp -> %s: %v", backoffFixturesPath, err)
	}
	t.Logf("rewrote %s (%d calculate_backoff, %d should_retry, %d is_permanent_failure)",
		filepath.Clean(backoffFixturesPath),
		len(f.CalculateBackoff), len(f.ShouldRetry), len(f.IsPermanentFailure))
}
