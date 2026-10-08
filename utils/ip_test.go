package utils

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Behavioural-parity tests for the Go port of Java IpUtil. Each
// fixture asserts an invariant called out in ip.go's package doc.
// Adding a new behaviour means adding a new fixture row here AND a
// matching invariant comment in ip.go.
//
// The same fixtures are exported to testdata/ip_vectors.json (via the
// -update-fixtures flag, declared in fixture_flags_test.go as the
// canonical home for this package's parity flag) so that the 818-gaming
// Java side can load and assert against them as a cross-language
// conformance test. See docs/migration/c4b-wave-1-spec.md §C4b-1.2.

// ---------------------------------------------------------------------
// Fixture tables — single source of truth for both the Go test runs
// and the JSON exporter. Adding a row here propagates automatically to
// both Go assertions and the cross-language fixture file.
// ---------------------------------------------------------------------

// namedFixture is the structural contract every fixture row satisfies:
// each row carries a stable display name used for both `t.Run` subtest
// labels and JSON sort order.
type namedFixture interface{ fixtureName() string }

type parseClientIPCase struct {
	Name     string `json:"name"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

func (c parseClientIPCase) fixtureName() string { return c.Name }

type ipBoolCase struct {
	Name     string `json:"name"`
	Input    string `json:"input"`
	Expected bool   `json:"expected"`
}

func (c ipBoolCase) fixtureName() string { return c.Name }

// byName is a stable comparator usable from slices.SortStableFunc for
// any fixture row that implements namedFixture.
func byName[T namedFixture](a, b T) int { return cmp.Compare(a.fixtureName(), b.fixtureName()) }

var parseClientIPCases = []parseClientIPCase{
	// 1. plain IPv4 → echoed back
	{"plain_ipv4", "203.0.113.7", "203.0.113.7"},
	// 2. plain IPv6 → echoed back
	{"plain_ipv6", "2001:db8::1", "2001:db8::1"},
	// 3. XFF chain "client, proxy1, proxy2" → first segment only
	{"xff_chain", "203.0.113.7, 198.51.100.1, 198.51.100.2", "203.0.113.7"},
	// 4. XFF chain with no spaces → trimmed first segment
	{"xff_chain_no_space", "203.0.113.7,198.51.100.1", "203.0.113.7"},
	// 5. XFF chain with extra whitespace → first segment trimmed
	{"xff_chain_ws", "  203.0.113.7  ,  198.51.100.1  ", "203.0.113.7"},
	// 6. empty string → empty (caller falls back to next header)
	{"empty", "", ""},
	// 7. whitespace-only → empty
	{"whitespace_only", "   \t  ", ""},
	// 8. literal "unknown" → empty (Java isValidIp rejects)
	{"unknown_lower", "unknown", ""},
	{"unknown_mixed_case", "UnKnOwN", ""},
	// 9. chain leading with "unknown" → empty (so probe loop
	//    advances to the next header instead of latching onto
	//    a useless sentinel)
	{"unknown_then_real", "unknown, 203.0.113.7", ""},
	// 10. single trailing comma → first segment kept
	{"trailing_comma", "203.0.113.7,", "203.0.113.7"},
	// 11. comma at start (malformed) → empty first segment, returns ""
	{"leading_comma", ", 203.0.113.7", ""},
	// 12. non-IP string → returned as-is (parity: Java didn't
	//     validate format, only emptiness/sentinel)
	{"non_ip_garbage", "not-an-ip", "not-an-ip"},
	// 13. IPv6 without brackets and no comma → echoed back
	{"ipv6_loopback", "::1", "::1"},
	// 14. IPv6 entry inside an XFF chain → first segment kept
	{"xff_chain_ipv6_first", "2001:db8::1, 198.51.100.1", "2001:db8::1"},
	// 15. leading whitespace on a valid IPv4 → trimmed
	{"leading_whitespace_ipv4", "  203.0.113.7", "203.0.113.7"},
}

var isPrivateCases = []ipBoolCase{
	// IPv4 RFC 1918
	{"ipv4_10_8", "10.1.2.3", true},
	{"ipv4_172_16", "172.16.5.5", true},
	{"ipv4_172_31", "172.31.255.254", true},
	{"ipv4_192_168", "192.168.0.1", true},
	{"ipv4_172_15_public", "172.15.0.1", false},
	{"ipv4_172_32_public", "172.32.0.1", false},
	{"ipv4_public_8_8_8_8", "8.8.8.8", false},
	{"ipv4_loopback_is_not_private", "127.0.0.1", false},
	// IPv6 RFC 4193 — both fc00::/8 and fd00::/8 halves of /7
	// MUST be classified as private (Java InetAddress.isSiteLocalAddress
	// does NOT cover ULA — Java side has to reimplement Go's IsPrivate).
	{"ipv6_ula_fc00", "fc00::1", true},
	{"ipv6_ula_fd00", "fd00::1", true},
	{"ipv6_public_2001_db8", "2001:db8::1", false},
	{"ipv6_loopback_is_not_private", "::1", false},
	// Malformed inputs return false (nil net.IP is safe).
	{"malformed_alpha", "not-an-ip", false},
	{"malformed_partial_v4", "10.0.0", false},
	// Forms Go net.ParseIP rejects but Java InetAddress.getByName
	// would otherwise accept — keep parity by rejecting on the Java
	// pre-check and asserting `false` here.
	{"bracketed_ipv6_ula", "[fc00::1]", false},
	{"ipv6_zone_id_ula", "fc00::1%1", false},
}

var isLoopbackCases = []ipBoolCase{
	{"ipv4_loopback_127_0_0_1", "127.0.0.1", true},
	{"ipv4_loopback_high", "127.255.255.254", true},
	{"ipv6_loopback_colon_colon_1", "::1", true},
	{"ipv4_private_not_loopback", "192.168.1.1", false},
	{"ipv6_public_not_loopback", "2001:db8::1", false},
	{"malformed_alpha", "not-an-ip", false},
	// Bracketed / shorthand variants Go ParseIP rejects.
	{"bracketed_ipv6_loopback", "[::1]", false},
	{"ipv4_loopback_shorthand", "127.0.0", false},
}

var isLinkLocalCases = []ipBoolCase{
	{"ipv4_link_local_169_254", "169.254.1.1", true},
	{"ipv6_link_local_fe80", "fe80::1", true},
	{"ipv4_public_not_link_local", "8.8.8.8", false},
	{"ipv4_private_not_link_local", "10.0.0.1", false},
	{"ipv6_public_not_link_local", "2001:db8::1", false},
	{"malformed_alpha", "not-an-ip", false},
	// Zone-id / shorthand variants Go ParseIP rejects.
	{"ipv6_zone_id_link_local", "fe80::1%1", false},
	{"ipv4_link_local_shorthand", "169.254.1", false},
}

// ---------------------------------------------------------------------
// TestMain handles the -update-fixtures flag before delegating to the
// usual test runner. When the flag is set we regenerate the JSON file
// AFTER the test suite passes, so a stale flag invocation can't poison
// the fixture file with a failing source-of-truth.
// ---------------------------------------------------------------------

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if code == 0 && *updateFixtures {
		if err := writeIPVectorsJSON(); err != nil {
			// Surface write errors via stderr + non-zero exit so CI
			// can't silently miss a fixture-regen failure.
			_, _ = os.Stderr.WriteString("update-fixtures: " + err.Error() + "\n")
			code = 1
		}
	}
	os.Exit(code)
}

// writeIPVectorsJSON renders the package-level fixture tables to
// testdata/ip_vectors.json, deterministically (sorted keys / stable
// case order). The output is consumed by the Java conformance test in
// templates/game/backend/.../IpUtilConformanceTest.java.
func writeIPVectorsJSON() error {
	out := struct {
		Version       int                 `json:"version"`
		ReferenceImpl string              `json:"reference_impl"`
		Note          string              `json:"note"`
		ParseClientIP []parseClientIPCase `json:"parse_client_ip"`
		IsPrivate     []ipBoolCase        `json:"is_private"`
		IsLoopback    []ipBoolCase        `json:"is_loopback"`
		IsLinkLocal   []ipBoolCase        `json:"is_link_local"`
	}{
		Version:       1,
		ReferenceImpl: "x-go/utils/ip.go",
		Note:          "Regenerate via: cd x-go/utils && go test -update-fixtures . — see C4b Wave 1 SPEC §1.2",
		ParseClientIP: append([]parseClientIPCase(nil), parseClientIPCases...),
		IsPrivate:     append([]ipBoolCase(nil), isPrivateCases...),
		IsLoopback:    append([]ipBoolCase(nil), isLoopbackCases...),
		IsLinkLocal:   append([]ipBoolCase(nil), isLinkLocalCases...),
	}

	// Stable order: sort each sub-array by name so re-runs produce a
	// byte-for-byte identical file regardless of in-test ordering.
	slices.SortStableFunc(out.ParseClientIP, byName[parseClientIPCase])
	slices.SortStableFunc(out.IsPrivate, byName[ipBoolCase])
	slices.SortStableFunc(out.IsLoopback, byName[ipBoolCase])
	slices.SortStableFunc(out.IsLinkLocal, byName[ipBoolCase])

	// Use a streaming encoder so we can disable HTML-escaping; the
	// fixture file is read by Java/JSON-jackson which would otherwise
	// see `&` instead of a literal `&` in the human-facing note.
	// Encoder.Encode also appends a trailing newline.
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return err
	}
	buf := raw.Bytes()

	if err := os.MkdirAll("testdata", 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("testdata", "ip_vectors.json"), buf, 0o644)
}

// ---------------------------------------------------------------------
// Test functions — drive ParseClientIP / IsPrivate / IsLoopback /
// IsLinkLocal off the same tables that feed the JSON exporter.
// ---------------------------------------------------------------------

func TestParseClientIP(t *testing.T) {
	t.Parallel()
	for _, tc := range parseClientIPCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			got := ParseClientIP(tc.Input)
			if got != tc.Expected {
				t.Fatalf("ParseClientIP(%q) = %q, want %q", tc.Input, got, tc.Expected)
			}
		})
	}
}

func TestGetClientIP_NilRequest(t *testing.T) {
	t.Parallel()
	if got := GetClientIP(nil); got != UnknownIP {
		t.Fatalf("GetClientIP(nil) = %q, want %q", got, UnknownIP)
	}
}

func TestGetClientIP_HeaderProbeOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{
			name:    "xff_wins_over_real_ip",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Real-IP": "10.0.0.1"},
			remote:  "192.0.2.5:12345",
			want:    "203.0.113.7",
		},
		{
			name:    "proxy_client_ip_used_when_xff_absent",
			headers: map[string]string{"Proxy-Client-IP": "198.51.100.10"},
			remote:  "192.0.2.5:12345",
			want:    "198.51.100.10",
		},
		{
			name:    "wl_proxy_client_ip",
			headers: map[string]string{"WL-Proxy-Client-IP": "198.51.100.11"},
			want:    "198.51.100.11",
		},
		{
			name:    "http_client_ip",
			headers: map[string]string{"HTTP_CLIENT_IP": "198.51.100.12"},
			want:    "198.51.100.12",
		},
		{
			name:    "http_x_forwarded_for_chain",
			headers: map[string]string{"HTTP_X_FORWARDED_FOR": "203.0.113.99, 10.0.0.1"},
			want:    "203.0.113.99",
		},
		{
			name:    "x_real_ip_when_others_unknown",
			headers: map[string]string{"X-Forwarded-For": "unknown", "X-Real-IP": "203.0.113.50"},
			want:    "203.0.113.50",
		},
		{
			name:   "remote_addr_strips_port",
			remote: "203.0.113.77:54321",
			want:   "203.0.113.77",
		},
		{
			name:   "remote_addr_ipv6_with_port",
			remote: "[2001:db8::1]:54321",
			want:   "2001:db8::1",
		},
		{
			name:   "remote_addr_no_port",
			remote: "203.0.113.77",
			want:   "203.0.113.77",
		},
		{
			name:    "all_unknown_falls_through_to_remote",
			headers: map[string]string{"X-Forwarded-For": "unknown", "Proxy-Client-IP": "unknown"},
			remote:  "203.0.113.88:9999",
			want:    "203.0.113.88",
		},
		{
			name: "no_headers_no_remote_returns_unknown",
			want: UnknownIP,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodGet, "http://example.test/", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tc.remote
			if got := GetClientIP(req); got != tc.want {
				t.Fatalf("GetClientIP() = %q, want %q (headers=%v remote=%q)",
					got, tc.want, tc.headers, tc.remote)
			}
		})
	}
}

func TestIsPrivate(t *testing.T) {
	t.Parallel()
	for _, tc := range isPrivateCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.Input)
			if got := IsPrivate(ip); got != tc.Expected {
				t.Fatalf("IsPrivate(%s) = %v, want %v", tc.Input, got, tc.Expected)
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	t.Parallel()
	for _, tc := range isLoopbackCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.Input)
			if got := IsLoopback(ip); got != tc.Expected {
				t.Fatalf("IsLoopback(%s) = %v, want %v", tc.Input, got, tc.Expected)
			}
		})
	}
}

func TestIsLinkLocal(t *testing.T) {
	t.Parallel()
	for _, tc := range isLinkLocalCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.Input)
			if got := IsLinkLocal(ip); got != tc.Expected {
				t.Fatalf("IsLinkLocal(%s) = %v, want %v", tc.Input, got, tc.Expected)
			}
		})
	}
}

func TestParseIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		wantNil bool
	}{
		{"valid_ipv4", "203.0.113.1", false},
		{"valid_ipv6", "2001:db8::1", false},
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"unknown_sentinel", "unknown", true},
		{"unknown_mixed_case", "UnKnOwN", true},
		{"malformed", "not-an-ip", true},
		{"ipv4_with_extra_octet", "1.2.3.4.5", true},
		{"trimmed_ipv4", "  203.0.113.1  ", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip := ParseIP(tc.in)
			if (ip == nil) != tc.wantNil {
				t.Fatalf("ParseIP(%q) = %v, wantNil=%v", tc.in, ip, tc.wantNil)
			}
		})
	}
}

// Nil-safety smoke check: passing nil net.IP to the classifiers must
// never panic — Java callers commonly handle null IP downstream.
func TestClassifiers_NilIPSafe(t *testing.T) {
	t.Parallel()
	if IsLoopback(nil) {
		t.Fatal("IsLoopback(nil) should be false")
	}
	if IsPrivate(nil) {
		t.Fatal("IsPrivate(nil) should be false")
	}
	if IsLinkLocal(nil) {
		t.Fatal("IsLinkLocal(nil) should be false")
	}
}
