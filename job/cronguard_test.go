package job

// cronguard_test.go — Behavioural tests for Normalize and fixture-driven
// cross-language conformance helpers.
//
// The -update-fixtures flag regenerates testdata/cron_guard_vectors.json from
// the in-test case table; the Java CronGuardConformanceTest loads the same
// JSON and asserts that net.ys818.platform.common.schedule.CronExpressionGuard
// produces identical accept/reject decisions.
//
// Run normally:
//
//	cd x-go && go test ./job/... -race -v
//
// Regenerate fixture:
//
//	cd x-go && go test ./job/... -run TestCronGuardUpdateFixtures -update-cron-fixtures
//
// Note: the -update-cron-fixtures flag is intentionally distinct from
// x-go/utils -update-fixtures to avoid cross-package flag collisions when
// running tests from the workspace root.

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var updateCronFixtures = flag.Bool(
	"update-cron-fixtures",
	false,
	"regenerate x-go/job/testdata/cron_guard_vectors.json from in-test cases",
)

// ---------------------------------------------------------------------------
// Shared fixture cases — same data feeds Go tests + JSON regen.
// ---------------------------------------------------------------------------

type cronGuardCase struct {
	Name          string `json:"name"`
	Input         string `json:"input"`
	Accept        bool   `json:"accept"`
	Normalized    string `json:"normalized,omitempty"`
	ErrorContains string `json:"error_contains,omitempty"`
	Note          string `json:"note,omitempty"`
}

// cronGuardCases is the source of truth for both the Go tests and the
// regenerated JSON fixture consumed by the Java conformance test.
// Every row here becomes a DynamicTest on the Java side automatically.
var cronGuardCases = []cronGuardCase{
	// ── Accepted expressions ─────────────────────────────────────────────────
	{
		Name:       "valid_utc_midnight",
		Input:      "CRON_TZ=UTC 0 0 * * *",
		Accept:     true,
		Normalized: "CRON_TZ=UTC 0 0 * * *",
		Note:       "UTC midnight — canonical form, always accepted",
	},
	{
		Name:       "valid_shanghai_midnight",
		Input:      "CRON_TZ=Asia/Shanghai 0 0 * * *",
		Accept:     true,
		Normalized: "CRON_TZ=Asia/Shanghai 0 0 * * *",
		Note:       "Beijing midnight (Asia/Shanghai UTC+8); equivalent UTC firing time is 16:00 previous day",
	},
	{
		Name:       "valid_extra_whitespace_normalized",
		Input:      "CRON_TZ=UTC  0  0  *  *  *",
		Accept:     true,
		Normalized: "CRON_TZ=UTC 0 0 * * *",
		Note:       "Extra internal whitespace is normalised to single spaces",
	},
	{
		Name:       "valid_every_5_minutes",
		Input:      "CRON_TZ=UTC */5 * * * *",
		Accept:     true,
		Normalized: "CRON_TZ=UTC */5 * * * *",
		Note:       "5-field step expression with explicit TZ — accepted",
	},
	{
		Name:       "valid_weekly_sunday",
		Input:      "CRON_TZ=Asia/Shanghai 0 20 * * 0",
		Accept:     true,
		Normalized: "CRON_TZ=Asia/Shanghai 0 20 * * 0",
		Note:       "Weekly Sunday 20:00 Shanghai time; DOW 0 is standard POSIX",
	},
	// ── Rejected: 6-field / Quartz metacharacters ─────────────────────────────
	{
		Name:          "reject_six_field_quartz_midnight",
		Input:         "0 0 0 * * ?",
		Accept:        false,
		ErrorContains: "Quartz-only metacharacter",
		Note:          "Quartz 6-field with seconds AND '?' — metachar check runs first, reports '?'",
	},
	{
		Name:          "reject_six_field_no_quartz_char",
		Input:         "0 0 2 * * 1",
		Accept:        false,
		ErrorContains: "6 fields",
		Note:          "6-field Quartz without metacharacters: leading '0' is seconds; no CRON_TZ prefix either",
	},
	{
		Name:          "reject_question_mark",
		Input:         "CRON_TZ=UTC 0 0 ? * *",
		Accept:        false,
		ErrorContains: "Quartz-only metacharacter",
		Note:          "'?' is Quartz DOM wildcard; no robfig/cron equivalent — use '*' for 'any day'",
	},
	{
		Name:          "reject_L_char",
		Input:         "CRON_TZ=UTC 0 0 L * *",
		Accept:        false,
		ErrorContains: "Quartz-only metacharacter",
		Note:          "'L' means last-day-of-month in Quartz; not supported by robfig/cron",
	},
	{
		Name:          "reject_W_char",
		Input:         "CRON_TZ=UTC 0 0 1W * *",
		Accept:        false,
		ErrorContains: "Quartz-only metacharacter",
		Note:          "'W' means nearest weekday in Quartz; not supported by robfig/cron",
	},
	{
		Name:          "reject_hash_char",
		Input:         "CRON_TZ=UTC 0 0 * * 5#3",
		Accept:        false,
		ErrorContains: "Quartz-only metacharacter",
		Note:          "'#' means Nth weekday of month in Quartz; not supported by robfig/cron",
	},
	// ── Rejected: missing timezone ────────────────────────────────────────────
	{
		Name:          "reject_implicit_tz_five_field",
		Input:         "0 0 * * *",
		Accept:        false,
		ErrorContains: "no explicit timezone",
		Note:          "Valid 5-field POSIX cron but missing CRON_TZ= prefix — rejected to prevent UTC/JVM-TZ drift; add CRON_TZ=UTC or CRON_TZ=Asia/Shanghai",
	},
	{
		Name:          "reject_implicit_tz_daily_2am",
		Input:         "0 2 * * *",
		Accept:        false,
		ErrorContains: "no explicit timezone",
		Note:          "Common 'every day at 2am' cron; ambiguous without TZ prefix — was this Shanghai 2am or UTC 2am?",
	},
	{
		Name:          "reject_six_field_with_tz_prefix",
		Input:         "CRON_TZ=UTC 0 0 0 * * *",
		Accept:        false,
		ErrorContains: "6 fields",
		Note:          "Even with CRON_TZ prefix, 6 schedule fields are rejected",
	},
}

// ---------------------------------------------------------------------------
// Behavioural tests (Go side).
// ---------------------------------------------------------------------------

func TestCronGuardNormalize(t *testing.T) {
	t.Parallel()
	for _, tc := range cronGuardCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			normalized, err := Normalize(tc.Input)
			if tc.Accept {
				if err != nil {
					t.Fatalf("Normalize(%q) returned unexpected error: %v", tc.Input, err)
				}
				if normalized != tc.Normalized {
					t.Fatalf("Normalize(%q) = %q, want %q", tc.Input, normalized, tc.Normalized)
				}
			} else {
				if err == nil {
					t.Fatalf("Normalize(%q) returned nil error, want error containing %q", tc.Input, tc.ErrorContains)
				}
				if tc.ErrorContains != "" && !containsStr(err.Error(), tc.ErrorContains) {
					t.Fatalf("Normalize(%q) error = %q, want error containing %q", tc.Input, err.Error(), tc.ErrorContains)
				}
			}
		})
	}
}

// TestCronGuardSentinelErrors verifies that the exported error sentinels are
// reachable via errors.Is for callers who want to branch on error type.
func TestCronGuardSentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("six_field_sentinel", func(t *testing.T) {
		t.Parallel()
		_, err := Normalize("0 0 2 * * 1")
		if !errors.Is(err, ErrCronSixField) {
			t.Fatalf("expected ErrCronSixField, got: %v", err)
		}
	})

	t.Run("quartz_char_sentinel", func(t *testing.T) {
		t.Parallel()
		_, err := Normalize("CRON_TZ=UTC 0 0 ? * *")
		if !errors.Is(err, ErrCronQuartzChar) {
			t.Fatalf("expected ErrCronQuartzChar, got: %v", err)
		}
	})

	t.Run("missing_tz_sentinel", func(t *testing.T) {
		t.Parallel()
		_, err := Normalize("0 0 * * *")
		if !errors.Is(err, ErrCronMissingTZ) {
			t.Fatalf("expected ErrCronMissingTZ, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Fixture regeneration.
// ---------------------------------------------------------------------------

type cronGuardVectorsFile struct {
	Version       int             `json:"version"`
	ReferenceImpl string          `json:"reference_impl"`
	Cases         []cronGuardCase `json:"cases"`
}

// TestCronGuardUpdateFixtures regenerates testdata/cron_guard_vectors.json
// when -update-cron-fixtures is set. Without the flag it is a no-op (skipped).
func TestCronGuardUpdateFixtures(t *testing.T) {
	if !*updateCronFixtures {
		t.Skip("set -update-cron-fixtures to regenerate testdata/cron_guard_vectors.json")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate testdata directory")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}

	doc := cronGuardVectorsFile{
		Version:       1,
		ReferenceImpl: "x-go/job/cronguard.go",
		Cases:         cronGuardCases,
	}

	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal vectors: %v", err)
	}
	buf = append(buf, '\n')

	target := filepath.Join(dir, "cron_guard_vectors.json")
	if err := os.WriteFile(target, buf, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Logf("wrote %d bytes to %s", len(buf), target)
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func containsStr(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && (s[:len(sub)] == sub || containsStr(s[1:], sub)))
}
