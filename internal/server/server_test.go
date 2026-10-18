package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/config"
	"github.com/drywaters/learnd/internal/handler"
	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testAPIToken = "test-api-token"

func newTestRouter(t *testing.T) (http.Handler, *repository.EntryRepository) {
	t.Helper()
	pool := testdb.New(t)
	repo := repository.NewEntryRepository(pool)
	cfg := &config.Config{APIToken: testAPIToken, SecureCookies: true, Location: time.UTC}
	return New(cfg, repo, pool, false).Router(), repo
}

// Probes run without credentials, so /health and /ready must stay outside
// the auth group. An unreachable database turns /ready into a 503 while
// /health keeps reporting that the process is up.
func TestProbesBypassAuth(t *testing.T) {
	unreachable, err := pgxpool.New(context.Background(), "postgres://learnd:secret@127.0.0.1:1/learnd?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unreachable.Close)

	cfg := &config.Config{APIToken: testAPIToken, SecureCookies: true, Location: time.UTC}
	tests := []struct {
		name     string
		db       handler.Pinger
		path     string
		wantCode int
		wantBody string
	}{
		{"health", unreachable, "/health", http.StatusOK, "ok"},
		{"ready ok", pingerFunc(func(context.Context) error { return nil }), "/ready", http.StatusOK, `{"status":"ok","db":"ok"}`},
		{"ready unreachable db", unreachable, "/ready", http.StatusServiceUnavailable, `{"status":"degraded","db":"error"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := New(cfg, nil, tt.db, false).Router()
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("GET %s status = %d, want %d: %s", tt.path, rec.Code, tt.wantCode, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Fatalf("GET %s body = %q, want %q", tt.path, got, tt.wantBody)
			}
		})
	}
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

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

// LRN-7: a PUT that omits fields used to write NULL into them, and a NULL
// source_type failed with a 500.
func TestUpdateLeavesOmittedFieldsUnchanged(t *testing.T) {
	router, repo := newTestRouter(t)
	ctx := context.Background()
	tag, notes, title := "go", "first notes", "Original title"
	created, err := repo.Create(ctx, &model.CreateEntryInput{
		SourceURL: "https://example.com/put", NormalizedURL: "https://example.com/put", Tag: &tag, Notes: &notes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Update(ctx, created.ID, &model.UpdateEntryInput{Title: model.Some(&title)}); err != nil {
		t.Fatal(err)
	}

	put := func(form url.Values) *model.Entry {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/entries/"+created.ID.String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %v status = %d: %s", form, rec.Code, rec.Body.String())
		}
		entry, err := repo.GetByID(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}

	entry := put(url.Values{"quantity": {"3"}})
	if entry.Quantity == nil || *entry.Quantity != 3 {
		t.Fatalf("quantity = %v, want 3", entry.Quantity)
	}
	if entry.Tag == nil || *entry.Tag != tag || entry.Notes == nil || *entry.Notes != notes ||
		entry.Title == nil || *entry.Title != title || entry.SourceType != model.SourceTypeOther {
		t.Fatalf("omitted fields changed: tag=%v notes=%v title=%v type=%s", entry.Tag, entry.Notes, entry.Title, entry.SourceType)
	}

	entry = put(url.Values{"title": {""}, "source_type": {""}})
	if entry.Title != nil {
		t.Fatalf("title = %q, want cleared", *entry.Title)
	}
	if entry.SourceType != model.SourceTypeOther || entry.Quantity == nil || *entry.Quantity != 3 {
		t.Fatalf("type=%s quantity=%v, want unchanged", entry.SourceType, entry.Quantity)
	}
}
