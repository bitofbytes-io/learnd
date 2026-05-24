package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthAllowsValidBearerWithoutOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/entries", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	called := false

	Auth("secret", false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("called=%t status=%d", called, rec.Code)
	}
}

func TestAuthRejectsInvalidBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/entries", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()

	Auth("secret", false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthRejectsCookieUnsafeMethodWithoutOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://learnd.example/api/entries", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
	rec := httptest.NewRecorder()

	Auth("secret", false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAuthAllowsCookieUnsafeMethodWithSameOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://learnd.example/api/entries", nil)
	req.Header.Set("Origin", "http://learnd.example")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
	rec := httptest.NewRecorder()
	called := false

	Auth("secret", false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("called=%t status=%d", called, rec.Code)
	}
}

func TestSameOriginAllowsForwardedHTTPSOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://learnd.example/login", nil)
	req.Host = "learnd.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", "https://learnd.example")
	rec := httptest.NewRecorder()

	SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestSameOriginRejectsCrossSiteOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://learnd.example/login", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
