package job

// cronguard.go — Cron expression guard for the Synapse job scheduler.
//
// Why this exists
// ---------------
// Phase A Burst 1 (PR #200, merged 2026-04-29) surfaced two compounding bugs
// when Java scheduled-job configurations were ported to Go:
//
//   1. Field-count mismatch: Quartz cron uses 6 fields (leading seconds field),
//      e.g. `0 0 0 * * ?`. robfig/cron expects 5 fields (minute resolution).
//      The leading `0` silently becomes the minute field, shifting every field
//      left and producing a valid-but-wrong schedule.
//
//   2. Implicit timezone: JVM deploy uses Asia/Shanghai as JVM default TZ.
//      Go uses UTC unless told otherwise. A Java midnight cron `0 0 0 * * ?`,
//      ported as-is, fires at UTC 00:00 = Beijing 08:00 — eight hours late.
//
// robfig/cron v3+ supports an explicit `CRON_TZ=` prefix; we require it for
// every expression that is not already an unambiguous UTC expression with an
// explicit trailing "UTC" note in the surrounding config.
//
// Policy (hard-coded, agreed in Phase A W2 design doc)
// ----------------------------------------------------
//   - 6-field expressions are rejected; caller must strip the leading seconds
//     field and re-validate.
//   - Quartz-only metacharacters `?`, `L`, `W`, `#` are rejected regardless of
//     field count; they have no robfig/cron equivalent.
//   - Every expression MUST carry an explicit `CRON_TZ=` prefix.  The sole
//     exception is the literal sentinel "UTC" passed in the prefix position
//     (see Normalize for exact rules).  This ensures the JVM-TZ/Go-UTC
//     double-crack cannot recur silently.
//
// This file is the Go reference implementation for the shared fixture at
// x-go/job/testdata/cron_guard_vectors.json; the Java conformance test
// CronGuardConformanceTest.java loads the same JSON and asserts that
// net.ys818.platform.common.schedule.CronExpressionGuard produces identical
// accept/reject decisions.

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors — use errors.Is for matching.
var (
	// ErrCronSixField is returned when the expression has 6 whitespace-
	// separated fields (Quartz format, leading seconds).
	ErrCronSixField = errors.New("cron: 6-field detected (Quartz format with leading seconds field); remove the leading seconds field to get a 5-field robfig/cron expression")

	// ErrCronQuartzChar is returned when a Quartz-only metacharacter is
	// present: '?', 'L', 'W', or '#'.
	ErrCronQuartzChar = errors.New("cron: Quartz-only metacharacter detected ('?', 'L', 'W', '#'); these are not supported by robfig/cron — replace with standard POSIX cron equivalents")

	// ErrCronMissingTZ is returned when no explicit CRON_TZ= prefix is
	// present. Every expression must declare its timezone to avoid silent
	// UTC/Asia-Shanghai drift bugs.
	ErrCronMissingTZ = errors.New("cron: no explicit timezone; prefix with CRON_TZ=UTC for UTC or CRON_TZ=Asia/Shanghai for Beijing time — implicit timezone is rejected to prevent UTC/JVM-TZ drift")
)

// Normalize validates and normalises a cron expression for use with
// robfig/cron v3.
//
// Accepted input forms:
//
//	CRON_TZ=<tz> <min> <hour> <dom> <mon> <dow>   — explicit TZ prefix (preferred)
//
// Rejected input forms:
//
//	<sec> <min> <hour> <dom> <mon> <dow>           — 6-field Quartz
//	any expression containing ? L W #              — Quartz metacharacters
//	<min> <hour> <dom> <mon> <dow> (no TZ prefix)  — implicit TZ
//
// On success it returns the normalised expression (the input with consistent
// whitespace) and a nil error. On failure it returns an empty string and a
// descriptive error (one of the Err* sentinels above, potentially wrapped with
// fmt.Errorf + %w).
func Normalize(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	// ── 1. Extract optional CRON_TZ= prefix ─────────────────────────────────
	//
	// robfig/cron v3 parses "CRON_TZ=<tz> <fields...>" natively.
	// We require the prefix; strip it here so field-count validation below
	// operates on only the five schedule fields.
	hasTZPrefix := false
	schedulePart := expr
	if strings.HasPrefix(expr, "CRON_TZ=") {
		hasTZPrefix = true
		// Strip prefix; remainder is "<tz> <fields...>"
		rest := expr[len("CRON_TZ="):]
		// Split on first space to separate tz value from fields.
		idx := strings.IndexByte(rest, ' ')
		if idx < 0 {
			return "", fmt.Errorf("cron: malformed CRON_TZ prefix — expected CRON_TZ=<timezone> <fields>: %w", ErrCronMissingTZ)
		}
		if strings.TrimSpace(rest[:idx]) == "" {
			return "", fmt.Errorf("cron: empty CRON_TZ prefix — expected CRON_TZ=<timezone> <fields>: %w", ErrCronMissingTZ)
		}
		schedulePart = rest[idx+1:]
	}

	// ── 2. Quartz metacharacter check ────────────────────────────────────────
	//
	// Run this before field-count so the error message is maximally specific
	// (a 6-field Quartz expression like "0 0 0 * * ?" contains both problems;
	// reporting the metacharacter first guides the user to the right fix).
	for _, ch := range []byte{'?', 'L', 'W', '#'} {
		if strings.ContainsRune(schedulePart, rune(ch)) {
			return "", fmt.Errorf("cron: expression %q contains Quartz-only metacharacter %q: %w", expr, string(ch), ErrCronQuartzChar)
		}
	}

	// ── 3. Field count check ─────────────────────────────────────────────────
	fields := strings.Fields(schedulePart)
	switch len(fields) {
	case 5:
		// Correct.
	case 6:
		return "", fmt.Errorf("cron: expression %q has 6 fields (Quartz format); remove the leading seconds field %q: %w",
			expr, fields[0], ErrCronSixField)
	default:
		return "", fmt.Errorf("cron: expression %q has %d fields; expected exactly 5 (minute hour dom month dow): %w",
			expr, len(fields), ErrCronSixField)
	}

	// ── 4. Timezone requirement ──────────────────────────────────────────────
	if !hasTZPrefix {
		return "", fmt.Errorf("cron: expression %q has no CRON_TZ= prefix: %w", expr, ErrCronMissingTZ)
	}

	// ── 5. Normalise whitespace in the schedule part and reassemble ──────────
	normalizedFields := strings.Join(fields, " ")
	// Re-extract TZ value for output.
	tzPart := expr[:strings.Index(expr, " ")]
	normalized := tzPart + " " + normalizedFields

	return normalized, nil
}
