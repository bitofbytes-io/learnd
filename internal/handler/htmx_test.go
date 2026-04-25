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
