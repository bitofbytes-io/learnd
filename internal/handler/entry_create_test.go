package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
)

func TestBuildEntryViewMarksSafeSourceLinks(t *testing.T) {
	entry := createTestEntry(uuid.New())
	entry.SourceURL = "https://Example.com/path"

	view := buildEntryView(entry, 1, false)

	if !view.HasSourceHref || view.SourceHref != "https://example.com/path" {
		t.Fatalf("SourceHref = %q/%t, want safe link", view.SourceHref, view.HasSourceHref)
	}
}

func TestBuildEntryViewLeavesUnsafeLegacyURLAsText(t *testing.T) {
	entry := createTestEntry(uuid.New())
	entry.SourceURL = "javascript:alert(1)"

	view := buildEntryView(entry, 1, false)

	if view.HasSourceHref || view.SourceHref != "" {
		t.Fatalf("SourceHref = %q/%t, want no safe link", view.SourceHref, view.HasSourceHref)
	}
}

func TestCreateRejectsUnsafeSourceURL(t *testing.T) {
	mock := &mockEntryRepo{
		createFn: func(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
			t.Fatal("Create should not be called for unsafe URL")
			return nil, nil
		},
	}
	handler := NewEntryHandler(mock, false)

	form := url.Values{"url": {"http://127.0.0.1/private"}}
	req := httptest.NewRequest(http.MethodPost, "/api/entries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Create() status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateStoresValidatedSourceURL(t *testing.T) {
	id := uuid.New()
	var gotInput *model.CreateEntryInput
	mock := &mockEntryRepo{
		createFn: func(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
			gotInput = input
			entry := createTestEntry(id)
			entry.SourceURL = input.SourceURL
			entry.NormalizedURL = input.NormalizedURL
			return entry, nil
		},
		countFn: func(ctx context.Context) (int, error) {
			return 1, nil
		},
	}
	handler := NewEntryHandler(mock, false)

	form := url.Values{"url": {"https://Example.com/path/?utm_source=x#section"}}
	req := httptest.NewRequest(http.MethodPost, "/api/entries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Create() status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotInput == nil {
		t.Fatal("Create was not called")
	}
	if gotInput.SourceURL != "https://example.com/path/?utm_source=x#section" {
		t.Fatalf("SourceURL = %q", gotInput.SourceURL)
	}
	if gotInput.NormalizedURL != "https://example.com/path" {
		t.Fatalf("NormalizedURL = %q", gotInput.NormalizedURL)
	}
}
