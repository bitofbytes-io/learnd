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
	"github.com/google/uuid"
)

func TestUpdate(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		formData       url.Values
		mockSetup      func(*mockEntryRepo)
		expectedStatus int
		expectedBody   string
		verifyInput    func(*testing.T, *model.UpdateEntryInput)
	}{
		{
			name: "successful update with all fields",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{
				"tag":         {"go"},
				"time_spent":  {"30"},
				"quantity":    {"1"},
				"notes":       {"Test notes"},
				"title":       {"Updated Title"},
				"description": {"Updated Description"},
				"summary":     {"Updated Summary"},
				"source_type": {"youtube"},
			},
			mockSetup: func(m *mockEntryRepo) {
				id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
				m.updateFn = func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					entry := createTestEntry(id)
					return entry, nil
				}
				m.countByNormalizedURLFn = func(ctx context.Context, url string) (int, error) {
					return 1, nil
				}
			},
			expectedStatus: http.StatusOK,
			verifyInput: func(t *testing.T, input *model.UpdateEntryInput) {
				if !input.Tag.Set || input.Tag.Value == nil || *input.Tag.Value != "go" {
					t.Errorf("Update() tag = %v, want \"go\"", input.Tag)
				}
				if !input.Title.Set || input.Title.Value == nil || *input.Title.Value != "Updated Title" {
					t.Errorf("Update() title = %v, want 'Updated Title'", input.Title)
				}
				if !input.Description.Set || input.Description.Value == nil || *input.Description.Value != "Updated Description" {
					t.Errorf("Update() description = %v, want 'Updated Description'", input.Description)
				}
				if !input.SummaryText.Set || input.SummaryText.Value == nil || *input.SummaryText.Value != "Updated Summary" {
					t.Errorf("Update() summary = %v, want 'Updated Summary'", input.SummaryText)
				}
				if input.SourceType == nil || *input.SourceType != model.SourceTypeYouTube {
					t.Errorf("Update() sourceType = %v, want 'youtube'", input.SourceType)
				}
			},
		},
		{
			name: "update with empty optional fields clears them",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{
				"title":       {""},
				"description": {"   "},
				"summary":     {""},
				"source_type": {""},
			},
			mockSetup: func(m *mockEntryRepo) {
				id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
				m.updateFn = func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return createTestEntry(id), nil
				}
				m.countByNormalizedURLFn = func(ctx context.Context, url string) (int, error) {
					return 1, nil
				}
			},
			expectedStatus: http.StatusOK,
			verifyInput: func(t *testing.T, input *model.UpdateEntryInput) {
				if !input.Title.Set || input.Title.Value != nil {
					t.Errorf("Update() title = %+v, want set to nil for empty input", input.Title)
				}
				if !input.Description.Set || input.Description.Value != nil {
					t.Errorf("Update() description = %+v, want set to nil for whitespace input", input.Description)
				}
				if !input.SummaryText.Set || input.SummaryText.Value != nil {
					t.Errorf("Update() summary = %+v, want set to nil for empty input", input.SummaryText)
				}
				if input.SourceType != nil {
					t.Errorf("Update() sourceType = %v, want nil for empty input", input.SourceType)
				}
			},
		},
		{
			name: "update with case-insensitive source types",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{
				"source_type": {"PODCAST"},
			},
			mockSetup: func(m *mockEntryRepo) {
				id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
				m.updateFn = func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return createTestEntry(id), nil
				}
				m.countByNormalizedURLFn = func(ctx context.Context, url string) (int, error) {
					return 1, nil
				}
			},
			expectedStatus: http.StatusOK,
			verifyInput: func(t *testing.T, input *model.UpdateEntryInput) {
				if input.SourceType == nil || *input.SourceType != model.SourceTypePodcast {
					t.Errorf("Update() sourceType = %v, want 'podcast'", input.SourceType)
				}
			},
		},
		{
			name: "entry not found returns 404",
			id:   "550e8400-e29b-41d4-a716-446655440001",
			formData: url.Values{
				"title": {"New Title"},
			},
			mockSetup: func(m *mockEntryRepo) {
				m.updateFn = func(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return nil, nil // Entry not found
				}
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Entry not found",
		},
		{
			name:           "invalid UUID returns 400",
			id:             "not-a-uuid",
			formData:       url.Values{},
			mockSetup:      func(m *mockEntryRepo) {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid ID",
		},
		{
			name: "repository error returns 500",
			id:   "550e8400-e29b-41d4-a716-446655440002",
			formData: url.Values{
				"title": {"New Title"},
			},
			mockSetup: func(m *mockEntryRepo) {
				m.updateFn = func(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return nil, errors.New("database error")
				}
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Failed to update entry",
		},
		{
			name: "invalid source type is ignored",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{
				"title":       {"Valid Title"},
				"source_type": {"invalid_type"},
			},
			mockSetup: func(m *mockEntryRepo) {
				id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
				m.updateFn = func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return createTestEntry(id), nil
				}
				m.countByNormalizedURLFn = func(ctx context.Context, url string) (int, error) {
					return 1, nil
				}
			},
			expectedStatus: http.StatusOK,
			verifyInput: func(t *testing.T, input *model.UpdateEntryInput) {
				if input.SourceType != nil {
					t.Errorf("Update() sourceType = %v, want nil for invalid type", input.SourceType)
				}
				if !input.Title.Set || input.Title.Value == nil || *input.Title.Value != "Valid Title" {
					t.Errorf("Update() title = %v, want 'Valid Title'", input.Title)
				}
			},
		},
		{
			name:     "omitted fields are left unchanged",
			id:       "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{"notes": {"Only notes"}},
			mockSetup: func(m *mockEntryRepo) {
				m.updateFn = func(ctx context.Context, reqID uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
					return createTestEntry(reqID), nil
				}
			},
			expectedStatus: http.StatusOK,
			verifyInput: func(t *testing.T, input *model.UpdateEntryInput) {
				if !input.Notes.Set || input.Notes.Value == nil || *input.Notes.Value != "Only notes" {
					t.Errorf("Update() notes = %+v, want set to 'Only notes'", input.Notes)
				}
				for name, set := range map[string]bool{
					"tag": input.Tag.Set, "time_spent": input.TimeSpentSeconds.Set, "quantity": input.Quantity.Set,
					"title": input.Title.Set, "description": input.Description.Set, "summary": input.SummaryText.Set,
				} {
					if set {
						t.Errorf("Update() %s is set, want unchanged when omitted", name)
					}
				}
				if input.SourceType != nil {
					t.Errorf("Update() sourceType = %v, want nil when omitted", *input.SourceType)
				}
			},
		},
		{
			name: "invalid tag returns 422",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			formData: url.Values{
				"tag": {"has spaces"},
			},
			mockSetup:      func(m *mockEntryRepo) {},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedBody:   "invalid tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEntryRepo{}
			tt.mockSetup(mock)

			router := setupTestHandler(mock)
			path := "/entries/" + tt.id
			req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(tt.formData.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Update() status = %d, want %d", rec.Code, tt.expectedStatus)
			}
			if tt.expectedBody != "" && !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("Update() body = %q, want to contain %q", rec.Body.String(), tt.expectedBody)
			}
			if tt.verifyInput != nil && mock.updateCalledWith != nil {
				tt.verifyInput(t, mock.updateCalledWith)
			}
		})
	}
}
