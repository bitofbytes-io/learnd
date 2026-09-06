package handler

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/drywaters/learnd/internal/ui/pages"
)

const cookieName = "learnd_session"

// AuthHandler handles authentication
type AuthHandler struct {
	apiToken      string
	secureCookies bool
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(apiToken string, secureCookies bool) *AuthHandler {
	return &AuthHandler{
		apiToken:      apiToken,
		secureCookies: secureCookies,
	}
}

// LoginPage renders the login page
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated via valid cookie, redirect to home
	if cookie, err := r.Cookie(cookieName); err == nil {
		if constantTimeEqual(cookie.Value, h.apiToken) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	errorType := r.URL.Query().Get("error")
	redirectURL := r.URL.Query().Get("redirect")
	pages.LoginPage(errorType, redirectURL).Render(r.Context(), w)
}

// Login handles the login form submission
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		slog.Info("login failed", "reason", "invalid_request")
		http.Redirect(w, r, "/login?error=invalid_request", http.StatusSeeOther)
		return
	}

	apiKey := r.FormValue("api_key")
	if apiKey == "" {
		slog.Info("login failed", "reason", "missing_key")
		http.Redirect(w, r, "/login?error=missing_key", http.StatusSeeOther)
		return
	}

	// Validate the API token with constant-time comparison
	if !constantTimeEqual(apiKey, h.apiToken) {
		slog.Info("login failed", "reason", "invalid_key")
		http.Redirect(w, r, "/login?error=invalid_key", http.StatusSeeOther)
		return
	}

	// Set the session cookie with the token value
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.apiToken,
		Path:     "/",
		MaxAge:   90 * 24 * 60 * 60, // 90 days
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to the original URL if provided, otherwise home
	redirectURL := r.FormValue("redirect")
	if redirectURL == "" || !isValidRedirect(redirectURL) {
		redirectURL = "/"
	}
	slog.Info("login successful", "redirect_to", sanitizeRedirectForLog(redirectURL))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// isValidRedirect checks that the redirect URL is safe (relative path only)
func isValidRedirect(rawURL string) bool {
	if strings.ContainsAny(rawURL, "\\") || strings.IndexFunc(rawURL, unicode.IsControl) >= 0 {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" {
		return false
	}
	path := parsed.Path
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") &&
		!strings.Contains(path, "\\") && strings.IndexFunc(path, unicode.IsControl) < 0
}

func sanitizeRedirectForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}

// Logout clears the session cookie
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	slog.Info("logout")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
