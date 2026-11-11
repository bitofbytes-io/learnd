package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestEntryRoutesRejectBadIDs(t *testing.T) {
	missing := uuid.MustParse("550e8400-e29b-41d4-a716-446655440099")
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Entry, error) { return nil, nil },
	}
	handler := NewEntryHandler(mock, false)
	router := chi.NewRouter()
	router.Get("/entries/{id}/status", handler.Status)
	router.Delete("/entries/{id}", handler.Delete)
	router.Post("/entries/{id}/refresh-enrichment", handler.RefreshEnrichment)
	router.Post("/entries/{id}/refresh-summary", handler.RefreshSummary)

	for _, route := range []struct{ method, suffix string }{
		{http.MethodGet, "/status"},
		{http.MethodDelete, ""},
		{http.MethodPost, "/refresh-enrichment"},
		{http.MethodPost, "/refresh-summary"},
	} {
		for id, want := range map[string]int{"not-a-uuid": http.StatusBadRequest, missing.String(): http.StatusNotFound} {
			req := httptest.NewRequest(route.method, "/entries/"+id+route.suffix, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("%s /entries/%s%s status = %d, want %d", route.method, id, route.suffix, rec.Code, want)
			}
		}
	}
}

func TestRetryFailed(t *testing.T) {
	tests := []struct {
		name         string
		summaries    bool
		currentURL   string
		requeued     repository.FailedJobs
		repoErr      error
		wantStatus   int
		wantRedirect string
		wantToast    string
	}{
		{name: "requeued returns to the dashboard", summaries: true, currentURL: "https://learnd.test/?tag=go&status=failed",
			requeued: repository.FailedJobs{Enrichment: 2, Summary: 1}, wantStatus: http.StatusOK, wantRedirect: "/?tag=go&status=failed"},
		{name: "not on the dashboard", requeued: repository.FailedJobs{Enrichment: 1}, currentURL: "https://learnd.test/reports",
			wantStatus: http.StatusOK, wantRedirect: "/"},
		{name: "nothing failed", wantStatus: http.StatusOK, wantToast: "No failed entries to retry"},
		{name: "repository error", repoErr: errors.New("db down"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotSummaries bool
			mock := &mockEntryRepo{retryFailedJobsFn: func(_ context.Context, includeSummaries bool) (repository.FailedJobs, error) {
				gotSummaries = includeSummaries
				return tt.requeued, tt.repoErr
			}}
			router := chi.NewRouter()
			router.Post("/api/entries/retry-failed", NewEntryHandler(mock, tt.summaries).RetryFailed)

			req := httptest.NewRequest(http.MethodPost, "/api/entries/retry-failed", nil)
			if tt.currentURL != "" {
				req.Header.Set("HX-Current-URL", tt.currentURL)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if gotSummaries != tt.summaries {
				t.Fatalf("includeSummaries = %t, want %t", gotSummaries, tt.summaries)
			}
			if got := rec.Header().Get("HX-Redirect"); got != tt.wantRedirect {
				t.Fatalf("HX-Redirect = %q, want %q", got, tt.wantRedirect)
			}
			if !strings.Contains(rec.Header().Get("HX-Trigger"), tt.wantToast) {
				t.Fatalf("HX-Trigger = %q, want %q", rec.Header().Get("HX-Trigger"), tt.wantToast)
			}
		})
	}
}

func TestStatusRendersPollingRow(t *testing.T) {
	id := uuid.New()
	entry := createTestEntry(id)
	entry.EnrichmentStatus = model.StatusProcessing
	router := setupTestHandler(&mockEntryRepo{
		getByIDFn: func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) { return entry, nil },
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/entries/"+id.String()+"/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status() status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`id="entry-` + id.String() + `"`, `hx-trigger="every 5s"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Status() body missing %s: %s", want, body)
		}
	}
}

// Row polling and per-row retries refresh the failed-jobs banner out of band so
// its count follows jobs as they fail or are retried, and it disappears at zero.
func TestRowResponsesRefreshFailedBanner(t *testing.T) {
	id := uuid.New()
	entry := createTestEntry(id)
	entry.EnrichmentStatus = model.StatusProcessing

	tests := []struct {
		name        string
		method      string
		path        string
		failed      repository.FailedJobs
		countErr    error
		currentURL  string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "status shows the new count", method: http.MethodGet, path: "/entries/" + id.String() + "/status",
			failed:      repository.FailedJobs{Enrichment: 2, Entries: 2},
			wantContain: []string{`id="failed-jobs-banner"`, `hx-swap-oob="outerHTML"`, "2 entries failed to process", `hx-post="/api/entries/retry-failed"`, `href="/?status=failed"`},
		},
		{
			name: "status singular", method: http.MethodGet, path: "/entries/" + id.String() + "/status",
			failed:      repository.FailedJobs{Summary: 1, Entries: 1},
			wantContain: []string{"1 entry failed to process"},
		},
		{
			name: "status hides the banner at zero", method: http.MethodGet, path: "/entries/" + id.String() + "/status",
			wantContain: []string{`id="failed-jobs-banner"`, `hx-swap-oob="outerHTML"`},
			wantAbsent:  []string{"failed to process", "retry-failed"},
		},
		{
			name: "status keeps the failed filter link", method: http.MethodGet, path: "/entries/" + id.String() + "/status",
			failed: repository.FailedJobs{Enrichment: 1, Entries: 1}, currentURL: "https://learnd.test/?status=failed",
			wantContain: []string{`href="/"`, "Show all"},
			wantAbsent:  []string{"Show failed"},
		},
		{
			name: "status ignores a count error", method: http.MethodGet, path: "/entries/" + id.String() + "/status",
			countErr:    errors.New("database error"),
			wantContain: []string{`id="failed-jobs-banner"`},
			wantAbsent:  []string{"failed to process"},
		},
		{
			name: "refresh enrichment shows the remaining count", method: http.MethodPost, path: "/entries/" + id.String() + "/refresh-enrichment",
			failed:      repository.FailedJobs{Enrichment: 1, Entries: 1},
			wantContain: []string{"1 entry failed to process", `hx-swap-oob="outerHTML"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEntryRepo{
				getByIDFn:       func(context.Context, uuid.UUID) (*model.Entry, error) { return entry, nil },
				countFilteredFn: func(context.Context, repository.ListOptions) (int, error) { return 1, nil },
				countFailedJobsFn: func(context.Context) (repository.FailedJobs, error) {
					return tt.failed, tt.countErr
				},
			}
			router := chi.NewRouter()
			handler := NewEntryHandler(mock, false)
			router.Get("/entries/{id}/status", handler.Status)
			router.Post("/entries/{id}/refresh-enrichment", handler.RefreshEnrichment)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.currentURL != "" {
				req.Header.Set("HX-Current-URL", tt.currentURL)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `id="entry-`+id.String()+`"`) {
				t.Fatalf("body missing the entry row: %s", body)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q: %s", want, body)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(body, absent) {
					t.Errorf("body contains %q: %s", absent, body)
				}
			}
		})
	}
}

// A row that leaves the dashboard filter is removed with an empty swap, but the
// banner still refreshes: that is how a filtered failed view sees the count drop.
func TestStatusRefreshesBannerWhenRowLeavesFilter(t *testing.T) {
	id := uuid.New()
	mock := &mockEntryRepo{
		getByIDFn:       func(context.Context, uuid.UUID) (*model.Entry, error) { return createTestEntry(id), nil },
		countFilteredFn: func(context.Context, repository.ListOptions) (int, error) { return 0, nil },
		countFailedJobsFn: func(context.Context) (repository.FailedJobs, error) {
			return repository.FailedJobs{Enrichment: 1, Entries: 1}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/entries/"+id.String()+"/status", nil)
	req.Header.Set("HX-Current-URL", "https://learnd.test/?status=failed")
	rec := httptest.NewRecorder()
	setupTestHandler(mock).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `id="entry-`) {
		t.Fatalf("Status() rendered the row: %s", body)
	}
	for _, want := range []string{`hx-swap-oob="outerHTML"`, "1 entry failed to process"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Status() body missing %q: %s", want, body)
		}
	}
}

func TestDeleteRerendersRemainingDuplicates(t *testing.T) {
	deleted := createTestEntry(uuid.New())
	remaining := []model.Entry{*createTestEntry(uuid.New()), *createTestEntry(uuid.New())}
	var deletedID uuid.UUID
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Entry, error) { return deleted, nil },
		deleteFn:  func(ctx context.Context, id uuid.UUID) error { deletedID = id; return nil },
		countFn:   func(ctx context.Context) (int, error) { return 2, nil },
		listByNormalizedURLFn: func(ctx context.Context, normalizedURL string) ([]model.Entry, error) {
			if normalizedURL != deleted.NormalizedURL {
				t.Errorf("ListByNormalizedURL(%q), want %q", normalizedURL, deleted.NormalizedURL)
			}
			return remaining, nil
		},
	}
	router := chi.NewRouter()
	router.Delete("/entries/{id}", NewEntryHandler(mock, false).Delete)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/entries/"+deleted.ID.String(), nil))

	if rec.Code != http.StatusOK || deletedID != deleted.ID {
		t.Fatalf("Delete() status = %d, deleted %s", rec.Code, deletedID)
	}
	body := rec.Body.String()
	if got := strings.Count(body, `hx-swap-oob="outerHTML"`); got != len(remaining) {
		t.Fatalf("Delete() rendered %d OOB rows, want %d: %s", got, len(remaining), body)
	}
	for _, entry := range remaining {
		if !strings.Contains(body, `id="entry-`+entry.ID.String()+`"`) {
			t.Fatalf("Delete() body missing duplicate %s", entry.ID)
		}
	}
	if !strings.Contains(rec.Header().Get("HX-Trigger"), "Entry deleted") {
		t.Fatalf("HX-Trigger = %q", rec.Header().Get("HX-Trigger"))
	}
}

func TestDeleteFromEditPageRedirects(t *testing.T) {
	entry := createTestEntry(uuid.New())
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Entry, error) { return entry, nil },
	}
	router := chi.NewRouter()
	router.Delete("/entries/{id}", NewEntryHandler(mock, false).Delete)

	req := httptest.NewRequest(http.MethodDelete, "/entries/"+entry.ID.String(), nil)
	req.Header.Set("HX-Current-URL", "https://learnd.test/?page=3")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("HX-Redirect"); got != "/?page=3" {
		t.Fatalf("HX-Redirect = %q, want /?page=3", got)
	}
}

func TestDeleteRepositoryError(t *testing.T) {
	entry := createTestEntry(uuid.New())
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Entry, error) { return entry, nil },
		deleteFn:  func(ctx context.Context, id uuid.UUID) error { return errors.New("db down") },
	}
	router := chi.NewRouter()
	router.Delete("/entries/{id}", NewEntryHandler(mock, false).Delete)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/entries/"+entry.ID.String(), nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Delete() status = %d, want 500", rec.Code)
	}
}

func TestCreateRerendersExistingDuplicates(t *testing.T) {
	created := createTestEntry(uuid.New())
	older := createTestEntry(uuid.New())
	mock := &mockEntryRepo{
		createFn: func(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
			created.SourceURL, created.NormalizedURL = input.SourceURL, input.NormalizedURL
			return created, nil
		},
		countByNormalizedURLFn: func(ctx context.Context, normalizedURL string) (int, error) { return 2, nil },
		listByNormalizedURLFn: func(ctx context.Context, normalizedURL string) ([]model.Entry, error) {
			return []model.Entry{*created, *older}, nil
		},
	}

	form := url.Values{"url": {"https://example.com/again"}, "allow_duplicate": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/api/entries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewEntryHandler(mock, false).Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Create() status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	newRow := strings.Index(body, `id="entry-`+created.ID.String()+`"`)
	oldRow := strings.Index(body, `id="entry-`+older.ID.String()+`"`)
	if newRow < 0 || oldRow < newRow {
		t.Fatalf("Create() body should render the new row, then the duplicate: %s", body)
	}
	if got := strings.Count(body, `hx-swap-oob="outerHTML"`); got != 1 {
		t.Fatalf("Create() rendered %d OOB rows, want 1 (the older duplicate)", got)
	}
}

// The edit page sends these with hx-swap="none", so they return only the toast.
func TestEditPageActionsSkipRowRendering(t *testing.T) {
	id := uuid.New()
	var countCalls int
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) { return createTestEntry(reqID), nil },
		updateFn: func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
			return createTestEntry(reqID), nil
		},
		countByNormalizedURLFn: func(ctx context.Context, normalizedURL string) (int, error) { countCalls++; return 1, nil },
	}
	router := setupTestHandler(mock)

	for _, tt := range []struct{ method, path, toast string }{
		{http.MethodPut, "/entries/" + id.String(), "Entry updated"},
		{http.MethodPost, "/entries/" + id.String() + "/refresh-summary", "Summary queued"},
	} {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("notes=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d", tt.method, tt.path, rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("%s %s body = %q, want empty", tt.method, tt.path, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("HX-Trigger"), tt.toast) {
			t.Fatalf("%s %s HX-Trigger = %q, want %q", tt.method, tt.path, rec.Header().Get("HX-Trigger"), tt.toast)
		}
	}
	if countCalls != 0 {
		t.Fatalf("CountByNormalizedURL called %d times, want 0", countCalls)
	}
}
