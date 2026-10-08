// Package utils — file phone.go ports the Java PhoneUtils helper at
// templates/game/backend/platform-core/platform-common/src/main/java/
// net/ys818/platform/common/utils/PhoneUtils.java into Go, preserving
// validation and masking semantics so Synapse Go code and 818-gaming
// Java code can validate the same Chinese mobile-number inputs without
// diverging.
//
// Behavioural-parity invariants (preserved from the Java original):
//
//  1. The validation regex is exactly `^1[3-9]\d{9}$` — 11 ASCII digits,
//     leading "1", second digit in [3-9]. Empty / nil input returns false.
//     Any non-digit byte (whitespace, dash, letter, full-width digit)
//     fails the match.
//  2. MaskChinesePhone aligns with Java mask() at the **code-point** level
//     for the Basic Multilingual Plane (where Java UTF-16 char count and
//     Go rune count agree): inputs with rune count < 11 are returned
//     **unchanged** (including the empty string); inputs with rune count
//     >= 11 keep runes[0,3) + "****" + runes[7,end). For inputs longer
//     than 11 runes the trailing runes after index 7 are kept verbatim,
//     matching Java's `phone.substring(7)`. Non-BMP supplementary code
//     points (e.g. emoji) are **out of contract**: Java counts them as
//     two UTF-16 code units while Go counts them as one rune, so callers
//     should pre-validate to ASCII or BMP before logging.
//  3. Both functions are pure, allocation-light (mask allocates one
//     []rune for non-empty inputs >= 11 runes), and safe for concurrent
//     use; no package-level state beyond the compiled regexp.
//
// This file is the **frozen reference** for the cross-language conformance
// suite under x-go/utils/testdata/phone_vectors.json. Any behaviour
// change here must be paired with a fixture regeneration AND a Java-side
// PhoneUtilsConformanceTest re-run, otherwise Go and Java will silently
// disagree on user-input validation.
package utils

import "regexp"

// chinesePhonePattern matches an 11-digit Chinese mobile number: leading
// "1", second digit 3-9, then any 9 ASCII digits. Compiled once at package
// load to keep IsValidChinesePhone allocation-free on the hot path.
var chinesePhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// IsValidChinesePhone reports whether s is a syntactically valid Chinese
// mobile-phone number (11 ASCII digits, leading "1", second digit 3-9).
// Empty input returns false. Mirrors Java PhoneUtils.isValid.
func IsValidChinesePhone(s string) bool {
	if s == "" {
		return false
	}
	return chinesePhonePattern.MatchString(s)
}

// MaskChinesePhone returns a log-safe rendering of s with the middle four
// runes replaced by "****". When the rune count is < 11 the input is
// returned unchanged so loggers never crash on short / malformed numbers
// — this matches Java PhoneUtils.mask, which also short-circuits on
// length < 11 rather than throwing.
//
// For rune count >= 11 the result is runes[:3] + "****" + runes[7:], so
// an input longer than 11 runes preserves its trailing runes (e.g.
// "13812345678999" → "138****5678999"). Operating on runes (not bytes)
// keeps Go aligned with Java's UTF-16-char-based substring semantics for
// BMP code points and avoids producing invalid UTF-8 by slicing inside a
// multi-byte sequence. Supplementary-plane code points (Java surrogate
// pairs, length() == 2) are out of contract — see the package comment.
func MaskChinesePhone(s string) string {
	runes := []rune(s)
	if len(runes) < 11 {
		return s
	}
	return string(runes[:3]) + "****" + string(runes[7:])
}
