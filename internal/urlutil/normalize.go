package urlutil

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/drywaters/learnd/internal/netguard"
)

var trackingParams = map[string]struct{}{
	"fbclid":          {},
	"gclid":           {},
	"mc_cid":          {},
	"mc_eid":          {},
	"ref":             {},
	"ref_src":         {},
	"utm_campaign":    {},
	"utm_content":     {},
	"utm_id":          {},
	"utm_medium":      {},
	"utm_source":      {},
	"utm_term":        {},
	"utm_reader":      {},
	"utm_name":        {},
	"utm_referrer":    {},
	"utm_social":      {},
	"utm_social_type": {},
}

// NormalizeURL normalizes a URL for duplicate detection.
func NormalizeURL(raw string) (string, error) {
	parsed, err := parsePublicHTTPURL(raw)
	if err != nil {
		return "", err
	}

	parsed.Fragment = ""

	if parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		if parsed.Path == "" {
			parsed.Path = "/"
		}
	}
	parsed.RawPath = ""

	query := parsed.Query()
	for key := range query {
		if _, ok := trackingParams[strings.ToLower(key)]; ok {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

// ValidateSourceURL validates and canonicalizes a user-submitted source URL before storage.
func ValidateSourceURL(raw string) (string, error) {
	parsed, err := parsePublicHTTPURL(raw)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// SafeLinkURL returns a trusted link target for rendering legacy URLs.
func SafeLinkURL(raw string) (string, bool) {
	parsed, err := parsePublicHTTPURL(raw)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

func parsePublicHTTPURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("URL must be an absolute http or https URL")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL must use http or https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("URL must not include userinfo")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, fmt.Errorf("URL must include a host")
	}
	if netguard.IsLocalhost(host) {
		return nil, fmt.Errorf("URL host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && netguard.IsUnsafeIP(ip) {
		return nil, fmt.Errorf("URL host is not allowed")
	}

	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else {
		parsed.Host = host
	}

	return parsed, nil
}
