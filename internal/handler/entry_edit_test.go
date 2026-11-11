package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
)

func TestEditPage(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		query          string
		mockSetup      func(*mockEntryRepo)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:  "valid entry returns 200",
			id:    "550e8400-e29b-41d4-a716-446655440000",
			query: "?return_to=%2F%3Fpage%3D2%26q%3Dgo%26tag%3Dai",
			mockSetup: func(m *mockEntryRepo) {
				id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
				m.getByIDFn = func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) {
					if reqID == id {
						return createTestEntry(id), nil
					}
					return nil, nil
				}
				m.countByNormalizedURLFn = func(ctx context.Context, url string) (int, error) {
					return 1, nil
				}
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "entry not found returns 404",
			id:   "550e8400-e29b-41d4-a716-446655440001",
			mockSetup: func(m *mockEntryRepo) {
				m.getByIDFn = func(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
					return nil, nil // Entry not found
				}
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Entry not found",
		},
		{
			name:           "invalid UUID returns 400",
			id:             "not-a-uuid",
			mockSetup:      func(m *mockEntryRepo) {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid ID",
		},
		{
			name:           "empty ID returns 400",
			id:             "",
			mockSetup:      func(m *mockEntryRepo) {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid ID",
		},
		{
			name: "repository error returns 500",
			id:   "550e8400-e29b-41d4-a716-446655440002",
			mockSetup: func(m *mockEntryRepo) {
				m.getByIDFn = func(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
					return nil, errors.New("database error")
				}
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Failed to get entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEntryRepo{}
			tt.mockSetup(mock)

			router := setupTestHandler(mock)
			path := "/entries/" + tt.id + "/edit" + tt.query
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("EditPage() status = %d, want %d", rec.Code, tt.expectedStatus)
			}
			if tt.expectedBody != "" && !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("EditPage() body = %q, want to contain %q", rec.Body.String(), tt.expectedBody)
			}
			if tt.expectedStatus == http.StatusOK {
				body := rec.Body.String()
				if tt.query != "" {
					for _, want := range []string{
						`href="/?page=2&amp;q=go&amp;tag=ai" hx-boost="false"`,
						`data-return-to="/?page=2&amp;q=go&amp;tag=ai"`,
						`evt.detail.elt === editForm && evt.detail.successful`,
					} {
						if !strings.Contains(body, want) {
							t.Errorf("EditPage() body = %q, want to contain %q", body, want)
						}
					}
				}
			}
		})
	}
}

func TestEditPageSuggestsTagsInUse(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440010")
	tests := []struct {
		name    string
		tags    []string
		tagsErr error
		want    string
	}{
		{
			name: "tags listed",
			tags: []string{"ai", "test"},
			want: `list="tag-suggestions"><datalist id="tag-suggestions"><option value="ai"></option><option value="test"></option></datalist>`,
		},
		{
			name:    "tag list failure still renders the form",
			tagsErr: errors.New("database error"),
			want:    `list="tag-suggestions"><datalist id="tag-suggestions"></datalist>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEntryRepo{
				getByIDFn: func(ctx context.Context, reqID uuid.UUID) (*model.Entry, error) {
					return createTestEntry(reqID), nil
				},
				listTagsFn: func(ctx context.Context) ([]string, error) {
					return tt.tags, tt.tagsErr
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/entries/"+id.String()+"/edit", nil)
			rec := httptest.NewRecorder()
			setupTestHandler(mock).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("EditPage() status = %d, want %d", rec.Code, http.StatusOK)
			}
			if body := rec.Body.String(); !strings.Contains(body, tt.want) {
				t.Fatalf("EditPage() body missing %q: %q", tt.want, body)
			}
		})
	}
}
