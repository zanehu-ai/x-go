package rbac

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// permissionCase is a single PermissionMatches test vector.
type permissionCase struct {
	Name      string `json:"name"`
	Granted   string `json:"granted"`
	Requested string `json:"requested"`
	Expected  bool   `json:"expected"`
}

// permissionVectors is the fixture schema written to testdata/permission_vectors.json.
type permissionVectors struct {
	Version       int              `json:"version"`
	ReferenceImpl string           `json:"reference_impl"`
	ExactMatch    []permissionCase `json:"exact_match"`
	PrefixWild    []permissionCase `json:"prefix_wildcard"`
	GlobalWild    []permissionCase `json:"global_wildcard"`
	NonMatch      []permissionCase `json:"non_match"`
	EdgeCases     []permissionCase `json:"edge_cases"`
}

// allCases returns every case from the fixture for test-driven verification.
func (v permissionVectors) allCases() []permissionCase {
	var all []permissionCase
	all = append(all, v.ExactMatch...)
	all = append(all, v.PrefixWild...)
	all = append(all, v.GlobalWild...)
	all = append(all, v.NonMatch...)
	all = append(all, v.EdgeCases...)
	return all
}

// buildVectors constructs the canonical set of test cases (≥20) that cover
// the Go PermissionMatches semantics documented in rbac.go (frozen reference).
func buildVectors() permissionVectors {
	return permissionVectors{
		Version:       1,
		ReferenceImpl: "x-go/rbac/rbac.go",
		ExactMatch: []permissionCase{
			{Name: "exact_simple", Granted: "cargo.read", Requested: "cargo.read", Expected: true},
			{Name: "exact_deep", Granted: "cargo.parcel.create", Requested: "cargo.parcel.create", Expected: true},
			{Name: "exact_single_segment", Granted: "admin", Requested: "admin", Expected: true},
			{Name: "exact_case_sensitive_upper_no_match", Granted: "Cargo.read", Requested: "cargo.read", Expected: false},
			{Name: "exact_case_sensitive_lower_no_match", Granted: "cargo.read", Requested: "Cargo.read", Expected: false},
		},
		PrefixWild: []permissionCase{
			{Name: "prefix_one_level", Granted: "cargo.*", Requested: "cargo.parcel.create", Expected: true},
			{Name: "prefix_one_level_direct_child", Granted: "cargo.*", Requested: "cargo.read", Expected: true},
			{Name: "prefix_two_levels", Granted: "cargo.parcel.*", Requested: "cargo.parcel.delete", Expected: true},
			{Name: "prefix_two_levels_deep", Granted: "cargo.parcel.*", Requested: "cargo.parcel.sub.read", Expected: true},
			{Name: "prefix_exact_prefix_itself", Granted: "cargo.*", Requested: "cargo", Expected: true},
			{Name: "prefix_exact_prefix_two_levels", Granted: "cargo.parcel.*", Requested: "cargo.parcel", Expected: true},
			{Name: "prefix_wrong_domain", Granted: "cargo.*", Requested: "game.room.read", Expected: false},
			{Name: "prefix_partial_mismatch", Granted: "cargo.parcel.*", Requested: "cargo.parcelfoo.read", Expected: false},
		},
		GlobalWild: []permissionCase{
			{Name: "global_wildcard_any", Granted: "*", Requested: "tenant.create", Expected: true},
			{Name: "global_wildcard_deep", Granted: "*", Requested: "cargo.parcel.read", Expected: true},
			{Name: "global_wildcard_single", Granted: "*", Requested: "admin", Expected: true},
		},
		NonMatch: []permissionCase{
			{Name: "non_match_different_action", Granted: "cargo.parcel.create", Requested: "cargo.parcel.read", Expected: false},
			{Name: "non_match_different_resource", Granted: "cargo.wallet.read", Requested: "cargo.parcel.read", Expected: false},
			{Name: "non_match_prefix_not_global", Granted: "cargo.*", Requested: "game.room.read", Expected: false},
			{Name: "non_match_invalid_star_prefix", Granted: "*foo", Requested: "foo", Expected: false},
			{Name: "non_match_double_star", Granted: "foo.**", Requested: "foo.bar", Expected: false},
			{Name: "non_match_multi_wildcard", Granted: "foo.*.*", Requested: "foo.bar.baz", Expected: false},
		},
		EdgeCases: []permissionCase{
			{Name: "edge_empty_granted", Granted: "", Requested: "cargo.read", Expected: false},
			{Name: "edge_empty_requested", Granted: "cargo.*", Requested: "", Expected: false},
			{Name: "edge_both_empty", Granted: "", Requested: "", Expected: false},
			{Name: "edge_whitespace_granted", Granted: "  ", Requested: "cargo.read", Expected: false},
			{Name: "edge_whitespace_requested", Granted: "cargo.*", Requested: "  ", Expected: false},
			{Name: "edge_granted_with_spaces_trimmed", Granted: " cargo.read ", Requested: "cargo.read", Expected: true},
			{Name: "edge_star_string_not_prefix_wildcard", Granted: "*foo", Requested: "*foo", Expected: true},
		},
	}
}

// TestPermissionVectorsFixture verifies that the built-in cases all pass
// against the Go PermissionMatches function (guards against regressions when
// the test cases themselves are edited).
func TestPermissionVectorsFixture(t *testing.T) {
	vectors := buildVectors()
	for _, tc := range vectors.allCases() {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			got := PermissionMatches(tc.Granted, tc.Requested)
			if got != tc.Expected {
				t.Errorf("PermissionMatches(%q, %q) = %v, want %v",
					tc.Granted, tc.Requested, got, tc.Expected)
			}
		})
	}
}

// TestUpdateFixtures writes the canonical fixture JSON to
// x-go/rbac/testdata/permission_vectors.json when invoked with
//
//	go test ./x-go/rbac/... -update-fixtures -run TestUpdateFixtures -count=1
//
// Without the flag the test is a no-op so it does not disturb normal CI runs.
func TestUpdateFixtures(t *testing.T) {
	if !*updateFixtures {
		t.Skip("skipping: -update-fixtures not set")
	}

	vectors := buildVectors()

	// Self-check: every case must pass Go PermissionMatches before we write.
	for _, tc := range vectors.allCases() {
		got := PermissionMatches(tc.Granted, tc.Requested)
		if got != tc.Expected {
			t.Fatalf("self-check failed: PermissionMatches(%q, %q) = %v, want %v (fix the test data before regenerating)",
				tc.Granted, tc.Requested, got, tc.Expected)
		}
	}

	data, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	outPath := filepath.Join(dir, "permission_vectors.json")
	if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Logf("wrote %d bytes to %s (%d total cases)",
		len(data), outPath, len(vectors.allCases()))
}
