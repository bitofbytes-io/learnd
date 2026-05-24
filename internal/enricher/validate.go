package enricher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/netguard"
)

const maxRedirects = 10

func newSafeHTTPClient(timeout time.Duration, allowedHosts ...string) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: safeDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if err := validateParsedURL(req.Context(), req.URL); err != nil {
				return err
			}
			if len(allowedHosts) > 0 {
				host := strings.ToLower(req.URL.Hostname())
				for _, allowed := range allowedHosts {
					if strings.EqualFold(host, allowed) {
						return nil
					}
				}
				return fmt.Errorf("redirect to disallowed host: %s", req.URL.Hostname())
			}
			return nil
		},
	}
}

func safeDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok && netguard.IsUnsafeIP(tcpAddr.IP) {
		_ = conn.Close()
		return nil, fmt.Errorf("remote address is not allowed")
	}

	return conn, nil
}

func validateFetchURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if err := validateParsedURL(ctx, parsedURL); err != nil {
		return nil, err
	}
	return parsedURL, nil
}

func validateParsedURL(ctx context.Context, parsedURL *url.URL) error {
	if parsedURL == nil {
		return fmt.Errorf("invalid URL: empty")
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("invalid URL: missing scheme or host")
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid URL: unsupported scheme")
	}

	if parsedURL.User != nil {
		return fmt.Errorf("invalid URL: userinfo not allowed")
	}

	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("invalid URL: missing host")
	}
	if netguard.IsLocalhost(host) {
		return fmt.Errorf("invalid URL: host is not allowed")
	}

	if ip := net.ParseIP(host); ip != nil {
		if netguard.IsUnsafeIP(ip) {
			return fmt.Errorf("invalid URL: host resolves to private IP")
		}
		return nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("invalid URL: failed to resolve host")
	}
	if len(addrs) == 0 {
		return fmt.Errorf("invalid URL: host has no addresses")
	}
	for _, addr := range addrs {
		if netguard.IsUnsafeIP(addr.IP) {
			return fmt.Errorf("invalid URL: host resolves to private IP")
		}
	}

	return nil
}
