// Package utils hosts small, dependency-free helpers shared across x-go.
//
// File ip.go is the Go port of the legacy Java IpUtil at
// templates/game/backend/platform-core/platform-common/src/main/java/
// net/ys818/platform/common/utils/IpUtil.java
//
// Behavioural-parity invariants preserved from the Java original:
//
//  1. The header probe order matches the Java implementation exactly:
//     X-Forwarded-For → Proxy-Client-IP → WL-Proxy-Client-IP →
//     HTTP_CLIENT_IP → HTTP_X_FORWARDED_FOR → X-Real-IP → RemoteAddr.
//  2. A header value is "valid" when it is non-empty and not equal to
//     "unknown" (case-insensitive). Whitespace-only values are treated
//     as empty after trimming, mirroring the upstream behaviour where
//     downstream extractFirstIp() trims the result.
//  3. For comma-separated chains (X-Forwarded-For style) only the first
//     segment is returned, trimmed of surrounding whitespace.
//  4. When every probe fails or the request is nil, the function returns
//     the literal string "unknown" — same sentinel as the Java caller
//     contract relies on (loggers / audit downstream).
//
// Idiomatic Go additions (NOT present in Java) that callers may use
// independently:
//
//   - ParseClientIP works directly on a header-string instead of an
//     http.Request, so it is reusable from non-HTTP callers (e.g. gRPC
//     metadata, queue payload audit).
//   - IsPrivate / IsLoopback expose net.IP classification helpers that
//     callers commonly want next to the extracted IP (rate limiter
//     bypass, allow-list checks). These are stdlib-only.
package utils

import (
	"net"
	"net/http"
	"strings"
)

// UnknownIP is the sentinel string returned when no header / RemoteAddr
// produces a usable value. Matches the Java IpUtil "unknown" literal.
const UnknownIP = "unknown"

// clientIPHeaders lists the headers probed by GetClientIP, in the exact
// order used by the Java IpUtil. Order is load-bearing — proxy chains
// configured upstream rely on X-Forwarded-For being checked first.
var clientIPHeaders = []string{
	"X-Forwarded-For",
	"Proxy-Client-IP",
	"WL-Proxy-Client-IP",
	"HTTP_CLIENT_IP",
	"HTTP_X_FORWARDED_FOR",
	"X-Real-IP",
}

// GetClientIP extracts the best-guess client IP from an *http.Request.
//
// It mirrors Java IpUtil.getClientIp(HttpServletRequest) one-for-one:
// probe each known header in order, pick the first valid value, fall
// back to req.RemoteAddr, and return UnknownIP if everything fails or
// req is nil.
//
// Note: Go's http.Request.RemoteAddr is "host:port", so the port is
// stripped before returning. Java's request.getRemoteAddr() returned a
// bare host, so this normalisation keeps callers' downstream parsing
// behaviour identical.
func GetClientIP(req *http.Request) string {
	if req == nil {
		return UnknownIP
	}

	for _, h := range clientIPHeaders {
		if ip := ParseClientIP(req.Header.Get(h)); ip != "" {
			return ip
		}
	}

	if req.RemoteAddr == "" {
		return UnknownIP
	}
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil && host != "" {
		return host
	}
	return req.RemoteAddr
}

// ParseClientIP normalises a single header value (which may itself be a
// comma-separated chain such as "client, proxy1, proxy2") into the
// originating client IP.
//
// Returns "" when the header value is empty, whitespace-only, or
// case-insensitively equal to "unknown" — matching the Java isValidIp
// + extractFirstIp combination. Callers that want the public sentinel
// instead should fall back to UnknownIP themselves; an empty return
// allows ParseClientIP to compose cleanly inside header-probe loops.
func ParseClientIP(headerVal string) string {
	v := strings.TrimSpace(headerVal)
	if v == "" {
		return ""
	}
	// Take the first comma-separated segment before the "unknown" check
	// so a chain like "unknown, 1.2.3.4" still passes through to the
	// caller's next probe rather than being silently accepted.
	if idx := strings.IndexByte(v, ','); idx >= 0 {
		v = strings.TrimSpace(v[:idx])
	}
	if v == "" {
		return ""
	}
	if strings.EqualFold(v, UnknownIP) {
		return ""
	}
	return v
}

// IsLoopback reports whether ip is an IPv4 or IPv6 loopback address
// (127.0.0.0/8 and ::1). Thin convenience wrapper over net.IP.IsLoopback
// so callers don't need an extra import.
func IsLoopback(ip net.IP) bool {
	return ip != nil && ip.IsLoopback()
}

// IsPrivate reports whether ip falls inside RFC 1918 (10/8, 172.16/12,
// 192.168/16) or RFC 4193 (fc00::/7) ranges. Loopback is NOT private —
// use IsLoopback for that classification, mirroring net.IP semantics.
//
// Implementation note: stdlib net.IP.IsPrivate (added in Go 1.17)
// already covers the IPv4 RFC 1918 + IPv6 RFC 4193 set, so we delegate.
// Defined as a package-level helper so test fixtures and call-sites
// read symmetrically with IsLoopback / IsLinkLocal.
func IsPrivate(ip net.IP) bool {
	return ip != nil && ip.IsPrivate()
}

// IsLinkLocal reports whether ip is a link-local unicast address
// (169.254/16 for IPv4, fe80::/10 for IPv6). Useful for filtering out
// auto-configured addresses before logging.
func IsLinkLocal(ip net.IP) bool {
	return ip != nil && ip.IsLinkLocalUnicast()
}

// ParseIP parses s into a net.IP. Returns nil on malformed input or
// when s is empty / "unknown". Wraps net.ParseIP with the IpUtil
// sentinel-handling so callers can chain GetClientIP → ParseIP → Is*.
func ParseIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, UnknownIP) {
		return nil
	}
	return net.ParseIP(s)
}
