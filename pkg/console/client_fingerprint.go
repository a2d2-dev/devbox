package console

import (
	"fmt"
	"net/netip"
	"strings"
)

// maskIP redacts a source IP so the response never carries a full address.
// IPv4 keeps the first three octets and masks the last as "x"
// (203.0.113.7 -> 203.0.113.x). IPv6 is truncated to its /32 prefix
// (first two hextets) rendered as "prefix::/32". Unparseable or empty input
// yields an empty string rather than echoing raw text.
func maskIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Strip a bracketed/single-colon port form; leave bare IPv6 (many colons).
	if host, ok := hostWithoutPort(raw); ok {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	addr = addr.WithZone("")
	if addr.Is4() || addr.Is4In6() {
		a := addr.As4()
		return fmt.Sprintf("%d.%d.%d.x", a[0], a[1], a[2])
	}
	a := addr.As16()
	// First two hextets form the /32 prefix; everything after is dropped.
	return fmt.Sprintf("%x:%x::/32", uint16(a[0])<<8|uint16(a[1]), uint16(a[2])<<8|uint16(a[3]))
}

// hostWithoutPort strips a trailing port only for unambiguous "host:port"
// forms (bracketed IPv6 or an IPv4/host with exactly one colon). Bare IPv6
// addresses (multiple colons, no brackets) are returned untouched.
func hostWithoutPort(raw string) (string, bool) {
	if strings.HasPrefix(raw, "[") {
		if i := strings.Index(raw, "]"); i > 0 {
			return raw[1:i], true
		}
		return raw, false
	}
	if strings.Count(raw, ":") == 1 {
		return raw[:strings.Index(raw, ":")], true
	}
	return raw, false
}

// parseUA turns a raw User-Agent string into a coarse ("Browser · OS") label
// and a device type ("desktop"/"mobile"/"tablet"/"unknown") using simple
// substring rules. It never returns the original UA. Order of checks matters:
// more specific tokens are tested before more general ones.
func parseUA(ua string) (label, deviceType string) {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "Unknown device", "unknown"
	}
	lower := strings.ToLower(ua)

	browser := "Unknown"
	switch {
	case strings.Contains(lower, "edg/") || strings.Contains(lower, "edge"):
		browser = "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome") || strings.Contains(lower, "crios"):
		browser = "Chrome"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	case strings.Contains(lower, "curl"):
		browser = "curl"
	case strings.Contains(lower, "wget"):
		browser = "wget"
	case strings.Contains(lower, "go-http-client"):
		browser = "Go client"
	}

	osName := "Unknown OS"
	switch {
	case strings.Contains(lower, "windows"):
		osName = "Windows"
	case strings.Contains(lower, "iphone"):
		osName = "iOS"
	case strings.Contains(lower, "ipad"):
		osName = "iPadOS"
	case strings.Contains(lower, "android"):
		osName = "Android"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macos") || strings.Contains(lower, "macintosh"):
		osName = "macOS"
	case strings.Contains(lower, "cros"):
		osName = "ChromeOS"
	case strings.Contains(lower, "linux"):
		osName = "Linux"
	}

	deviceType = "desktop"
	switch {
	case strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet"):
		deviceType = "tablet"
	case strings.Contains(lower, "mobile") || strings.Contains(lower, "iphone") || strings.Contains(lower, "android"):
		deviceType = "mobile"
	case browser == "curl" || browser == "wget" || browser == "Go client":
		deviceType = "unknown"
	}

	return browser + " · " + osName, deviceType
}
