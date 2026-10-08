package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The -update-fixtures flag used below is declared package-wide in
// fixture_flags_test.go and shared with the other parity tests in this
// package. See that file for the rationale.

// ---------------------------------------------------------------------------
// Shared fixture cases. The same slices feed:
//   - the Go behavioural tests (TestIsValidChinesePhone_ParityWithUpstream
//     and TestMaskChinesePhone_ParityWithUpstream)
//   - JSON regeneration when `go test -update-fixtures` is passed
// Adding a row here automatically expands both surfaces; the Java side
// re-runs against the regenerated JSON, so a new row immediately becomes
// a cross-language assertion without further manual sync.
// ---------------------------------------------------------------------------

type isValidCase struct {
	Name     string `json:"name"`
	Input    string `json:"input"`
	Expected bool   `json:"expected"`
}

type maskCase struct {
	Name     string `json:"name"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

// isValidCases covers every behavioural branch of IsValidChinesePhone:
// empty / leading-digit / second-digit / length / non-digit content.
// Names are stable identifiers that surface in JUnit failure output, so
// keep them snake_case and descriptive.
var isValidCases = []isValidCase{
	// 1. nil / empty short-circuit
	{Name: "empty", Input: "", Expected: false},

	// 2. canonical valid numbers (one per second-digit boundary 3-9)
	{Name: "second_digit_3", Input: "13012345678", Expected: true},
	{Name: "second_digit_4", Input: "14012345678", Expected: true},
	{Name: "second_digit_5", Input: "15012345678", Expected: true},
	{Name: "second_digit_6", Input: "16012345678", Expected: true},
	{Name: "second_digit_7", Input: "17012345678", Expected: true},
	{Name: "second_digit_8", Input: "18012345678", Expected: true},
	{Name: "second_digit_9", Input: "19012345678", Expected: true},
	{Name: "canonical_138", Input: "13812345678", Expected: true},

	// 3. second digit out of [3-9]
	{Name: "second_digit_0", Input: "10012345678", Expected: false},
	{Name: "second_digit_1", Input: "11112345678", Expected: false},
	{Name: "second_digit_2", Input: "12012345678", Expected: false},

	// 4. wrong leading digit
	{Name: "leading_2", Input: "23812345678", Expected: false},
	{Name: "leading_0", Input: "03812345678", Expected: false},

	// 5. length boundaries
	{Name: "too_short", Input: "1381234567", Expected: false},  // 10 digits
	{Name: "too_long", Input: "138123456789", Expected: false}, // 12 digits

	// 6. non-digit content
	{Name: "alpha_in_middle", Input: "138abcd5678", Expected: false},
}

// maskCases enumerates the Java-parity behaviour of MaskChinesePhone:
//   - rune count < 11 → passthrough (incl. empty / 5 / 10 / non-ASCII inputs)
//   - rune count == 11 → "xxx****yyyy"
//   - rune count > 11 → keep trailing runes after index 7 verbatim
//
// Non-ASCII passthrough case ("手机号123" = 6 runes) locks the rune-based
// semantics: Java length() = 6 < 11 also short-circuits, so both sides
// return the input unchanged. Non-BMP (supplementary-plane) code points
// remain explicitly out of contract — see phone.go package comment.
var maskCases = []maskCase{
	{Name: "empty_passthrough", Input: "", Expected: ""},
	{Name: "len_5_passthrough", Input: "13812", Expected: "13812"},
	{Name: "len_10_passthrough", Input: "1381234567", Expected: "1381234567"},
	{Name: "canonical_138", Input: "13812345678", Expected: "138****5678"},
	{Name: "canonical_156", Input: "15612345678", Expected: "156****5678"},
	{Name: "canonical_180", Input: "18012345678", Expected: "180****5678"},
	{Name: "len_12_keeps_tail", Input: "138123456789", Expected: "138****56789"},
	{Name: "len_14_keeps_tail", Input: "13812345678999", Expected: "138****5678999"},
	{Name: "non_ascii_short_passthrough", Input: "手机号123", Expected: "手机号123"},
}

// ---------------------------------------------------------------------------
// Behavioural tests (Go side).
// ---------------------------------------------------------------------------

// TestIsValidChinesePhone_ParityWithUpstream asserts every fixture row
// against IsValidChinesePhone. Test names match the JSON `name` field so
// a failing case is grep-able across both languages.
func TestIsValidChinesePhone_ParityWithUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range isValidCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidChinesePhone(tc.Input); got != tc.Expected {
				t.Fatalf("IsValidChinesePhone(%q) = %v, want %v",
					tc.Input, got, tc.Expected)
			}
		})
	}
}

// TestMaskChinesePhone_ParityWithUpstream asserts mask rune-level
// equivalence with the Java reference for every fixture row (BMP only;
// see phone.go package comment for the supplementary-plane caveat).
func TestMaskChinesePhone_ParityWithUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range maskCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			if got := MaskChinesePhone(tc.Input); got != tc.Expected {
				t.Fatalf("MaskChinesePhone(%q) = %q, want %q",
					tc.Input, got, tc.Expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fixture regeneration.
// ---------------------------------------------------------------------------

// phoneVectorsFile is the JSON document layout consumed by both this Go
// regen test and the Java PhoneUtilsConformanceTest. Field names are
// load-bearing — renaming them breaks the Java parser. `version` lets
// us bump the schema in lockstep across languages if a future change
// (e.g. adding `category`) is needed.
type phoneVectorsFile struct {
	Version       int           `json:"version"`
	ReferenceImpl string        `json:"reference_impl"`
	IsValid       []isValidCase `json:"is_valid"`
	Mask          []maskCase    `json:"mask"`
}

// TestUpdateFixtures regenerates testdata/phone_vectors.json from the
// shared isValidCases / maskCases slices when -update-fixtures is set.
// Without the flag it is a no-op (skipped) so normal `go test` runs are
// not surprised by file writes.
func TestUpdateFixtures(t *testing.T) {
	if !*updateFixtures {
		t.Skip("set -update-fixtures to regenerate testdata/phone_vectors.json")
	}

	// Resolve the path relative to this test file so the regen still
	// works no matter where `go test` is invoked from.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate testdata directory")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}

	doc := phoneVectorsFile{
		Version:       1,
		ReferenceImpl: "x-go/utils/phone.go",
		IsValid:       isValidCases,
		Mask:          maskCases,
	}

	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal vectors: %v", err)
	}
	// Trailing newline keeps the file POSIX-friendly and matches our
	// pre-commit gofmt-equivalent expectations for JSON fixtures.
	buf = append(buf, '\n')

	target := filepath.Join(dir, "phone_vectors.json")
	if err := os.WriteFile(target, buf, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
