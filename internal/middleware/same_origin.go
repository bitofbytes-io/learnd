package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// SameOrigin rejects unsafe browser requests unless Origin or Referer matches the request origin.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || sameOrigin(r) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Forbidden", http.StatusForbidden)
	})
}

func sameOrigin(r *http.Request) bool {
	requestOrigin := requestOrigin(r)
	if requestOrigin == "" {
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		return normalizeOrigin(origin) == requestOrigin
	}

	if referer := r.Header.Get("Referer"); referer != "" {
		return normalizeOrigin(referer) == requestOrigin
	}

	return false
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwardedProto != "" {
		scheme = strings.ToLower(forwardedProto)
	}
	return normalizeOrigin(scheme + "://" + r.Host)
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
