package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/config"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/testdb"
)

const testAPIToken = "test-api-token"

func newTestRouter(t *testing.T) (http.Handler, *repository.EntryRepository) {
	t.Helper()
	repo := repository.NewEntryRepository(testdb.New(t))
	cfg := &config.Config{APIToken: testAPIToken, SecureCookies: true}
	return New(cfg, repo, false).Router(), repo
}

// The iOS Shortcut posts a form to /api/entries with a Bearer token and no
// Origin header. This guards that path through the real router and auth
// middleware.
func TestBearerTokenCanCreateEntry(t *testing.T) {
	router, repo := newTestRouter(t)

	form := url.Values{"url": {"https://example.com/shortcut-article"}}
	req := httptest.NewRequest(http.MethodPost, "/api/entries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/entries status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Entry-Created"); got != "true" {
		t.Fatalf("X-Entry-Created = %q, want %q", got, "true")
	}
	count, err := repo.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("entry count = %d, want 1", count)
	}
}

func TestEntryJSONReadRoutesAreRemoved(t *testing.T) {
	router, _ := newTestRouter(t)

	for _, path := range []string{"/api/entries", "/api/entries/00000000-0000-0000-0000-000000000000"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}
