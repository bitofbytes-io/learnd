package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

const cookieName = "learnd_session"

// Auth middleware validates requests using either Bearer token or cookie.
// Programmatic clients (iOS Shortcuts, CLI) use Authorization: Bearer <token>.
// Browser clients use a cookie set during login.
func Auth(apiToken string, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check Authorization header first (for programmatic access)
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
					if constantTimeEqual(token, apiToken) {
						next.ServeHTTP(w, r)
						return
					}
				}
				// Invalid bearer token
				slog.Info("api authentication failed", "reason", "invalid_bearer_token", "path", r.URL.Path)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Fall back to cookie check (for browser access)
			if !isSafeMethod(r.Method) && !sameOrigin(r) {
				slog.Info("browser authentication failed", "reason", "cross_site_request", "path", r.URL.Path)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			cookie, err := r.Cookie(cookieName)
			if err != nil {
				slog.Info("browser authentication required", "path", r.URL.Path)
				redirectToLogin(w, r)
				return
			}

			if !constantTimeEqual(cookie.Value, apiToken) {
				// Invalid cookie, clear it and redirect
				slog.Info("browser authentication failed", "reason", "invalid_cookie", "path", r.URL.Path)
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					Secure:   secureCookies,
					SameSite: http.SameSiteLaxMode,
				})
				redirectToLogin(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// redirectToLogin redirects to login page, preserving the original URL
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	originalURL := r.URL.String()

	// Only add redirect param if not going to root
	loginURL := "/login"
	if originalURL != "/" {
		loginURL = "/login?redirect=" + url.QueryEscape(originalURL)
	}

	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}
