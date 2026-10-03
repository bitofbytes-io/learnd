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
