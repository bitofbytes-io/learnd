package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// validateMetricsAddr checks syntax without DNS lookups or binding a socket.
func validateMetricsAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if strings.EqualFold(addr, "off") {
		return "off", nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", fmt.Errorf("METRICS_ADDR must be host:port or off")
	}
	if !validMetricsHost(host, strings.HasPrefix(addr, "[")) {
		return "", fmt.Errorf("METRICS_ADDR host must be a valid IP address or hostname")
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("METRICS_ADDR port must be numeric (0-65535)")
		}
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("METRICS_ADDR port must be numeric (0-65535)")
	}
	return addr, nil
}

// validMetricsHost accepts address literals and DNS hostname syntax without
// resolving names. Brackets are reserved for IPv6; a zone is an interface name
// or numeric index. Reject malformed numeric addresses rather than treating
// them as DNS names that will fail later in the nonfatal listener startup.
func validMetricsHost(host string, bracketed bool) bool {
	alnum := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	if bracketed {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() {
			return false
		}
		// ParseAddr rejects an explicitly empty zone. Leave other zone strings
		// to the platform: interface names may contain spaces, Unicode, and
		// punctuation (for example, Windows' "Local Area Connection").
		return true
	}
	if host == "" {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Is4()
	}
	if strings.Contains(host, ".") && strings.Trim(host, "0123456789.") == "" {
		return false
	}
	hostname := strings.TrimSuffix(host, ".")
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || !alnum(label[0]) || !alnum(label[len(label)-1]) {
			return false
		}
		for _, c := range []byte(label) {
			if !alnum(c) && c != '-' {
				return false
			}
		}
	}
	return true
}
