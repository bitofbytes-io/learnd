package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginRedirectValidation(t *testing.T) {
	valid := []string{"/", "/entries/123", "/entries?tag=go#summary", "/search?q=https%3A%2F%2Fexample.test", "/my%20entry"}
	invalid := []string{"", "https://example.test", "//example.test", `/\example.test`, `/%5cexample.test`, "/%2fexample.test", "/%0d%0aLocation:evil", "/\t/evil", "relative", "javascript:alert(1)"}
	for _, path := range valid {
		if !isValidRedirect(path) {
			t.Errorf("rejected %q", path)
		}
	}
	for _, path := range invalid {
		if isValidRedirect(path) {
			t.Errorf("accepted %q", path)
		}
		form := url.Values{"api_key": {"secret"}, "redirect": {path}}
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		NewAuthHandler("secret", false).Login(recorder, req)
		if got := recorder.Header().Get("Location"); got != "/" {
			t.Errorf("unsafe redirect %q became %q", path, got)
		}
	}
}
