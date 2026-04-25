package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTMXCurrentPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/entries", nil)
	req.Header.Set("HX-Current-URL", "https://learnd.test/?page=2")

	if got := htmxCurrentPath(req); got != "/?page=2" {
		t.Fatalf("htmxCurrentPath() = %q, want %q", got, "/?page=2")
	}
}

func TestCaptureRedirectAfterCreate(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/entries", nil)
	req.Header.Set("HX-Current-URL", "https://learnd.test/?page=2")

	if got := captureRedirectAfterCreate(req); got != "/" {
		t.Fatalf("captureRedirectAfterCreate() = %q, want %q", got, "/")
	}
}

func TestSanitizeReturnTo(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty defaults home", raw: "", want: "/"},
		{name: "local dashboard page", raw: "/?page=2", want: "/?page=2"},
		{name: "relative path rejected", raw: "dashboard", want: "/"},
		{name: "absolute url rejected", raw: "https://evil.test/phish", want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeReturnTo(tt.raw); got != tt.want {
				t.Fatalf("sanitizeReturnTo(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestEntryEditURL(t *testing.T) {
	if got := entryEditURL("abc", "/?page=2"); got != "/entries/abc/edit?return_to=%2F%3Fpage%3D2" {
		t.Fatalf("entryEditURL() = %q", got)
	}
	if got := entryEditURL("abc", "/"); got != "/entries/abc/edit" {
		t.Fatalf("entryEditURL() home = %q", got)
	}
}
