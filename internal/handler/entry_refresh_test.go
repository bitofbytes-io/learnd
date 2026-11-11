package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/google/uuid"
)

func TestStatusRemovesRowsThatLeaveTheDashboardFilter(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440020")
	mock := &mockEntryRepo{
		getByIDFn: func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) {
			return createTestEntry(id), nil // enriched as an article
		},
		countFilteredFn: func(_ context.Context, opts repository.ListOptions) (int, error) {
			if opts.ID == nil || *opts.ID != id {
				t.Fatalf("CountFiltered() id = %v, want %s", opts.ID, id)
			}
			if opts.Query != nil && *opts.Query == "broken" {
				return 0, errors.New("database error")
			}
			if opts.SourceType != nil && *opts.SourceType != model.SourceTypeArticle {
				return 0, nil
			}
			return 1, nil
		},
	}
	router := setupTestHandler(mock)

	for currentURL, wantRow := range map[string]bool{
		"https://learnd.test/?type=other":           false,
		"https://learnd.test/?type=article":         true,
		"https://learnd.test/?q=test":               true,
		"https://learnd.test/?q=broken":             true, // a failed check keeps the row
		"https://learnd.test/":                      true,
		"https://learnd.test/entries/x/edit?type=x": true,
	} {
		req := httptest.NewRequest(http.MethodGet, "/entries/"+id.String()+"/status", nil)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", currentURL)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Status(%s) status = %d, want 200", currentURL, rec.Code)
		}
		if gotRow := strings.Contains(rec.Body.String(), `id="entry-`+id.String()+`"`); gotRow != wantRow {
			t.Fatalf("Status(%s) rendered row = %v, want %v", currentURL, gotRow, wantRow)
		}
	}
}

func TestDashboardRowRerendersPreserveReturnTo(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440010")

	tests := []struct {
		name   string
		method string
		path   string
		setup  func(*testing.T, *mockEntryRepo)
	}{
		{
			name:   "status polling",
			method: http.MethodGet,
			path:   "/entries/" + id.String() + "/status",
		},
		{
			name:   "refresh enrichment",
			method: http.MethodPost,
			path:   "/entries/" + id.String() + "/refresh-enrichment",
			setup: func(t *testing.T, m *mockEntryRepo) {
				m.resetEnrichmentFn = func(ctx context.Context, gotID uuid.UUID) error {
					if gotID != id {
						t.Fatalf("ResetEnrichment() id = %s, want %s", gotID, id)
					}
					return nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEntryRepo{
				getByIDFn: func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) {
					if reqID != id {
						t.Fatalf("GetByID() id = %s, want %s", reqID, id)
					}
					return createTestEntry(id), nil
				},
				countByNormalizedURLFn: func(ctx context.Context, normalizedURL string) (int, error) {
					return 1, nil
				},
			}
			if tt.setup != nil {
				tt.setup(t, mock)
			}

			router := setupTestHandler(mock)
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("HX-Current-URL", "https://learnd.test/?page=2")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d", tt.name, rec.Code, http.StatusOK)
			}
			want := "/entries/" + id.String() + "/edit?return_to=%2F%3Fpage%3D2"
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("%s body = %q, want to contain %q", tt.name, rec.Body.String(), want)
			}
		})
	}
}
