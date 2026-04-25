package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    *string
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid tag",
			input:    "golang",
			expected: strPtr("golang"),
		},
		{
			name:     "uppercase lowercased",
			input:    "GoLang",
			expected: strPtr("golang"),
		},
		{
			name:     "hyphenated tag",
			input:    "machine-learning",
			expected: strPtr("machine-learning"),
		},
		{
			name:        "comma separated rejected",
			input:       "ai, tech",
			expectError: true,
		},
		{
			name:        "space rejected",
			input:       "hello world",
			expectError: true,
		},
		{
			name:     "trimmed whitespace",
			input:    "  ai  ",
			expected: strPtr("ai"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTag(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("parseTag(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTag(%q) unexpected error: %v", tt.input, err)
				return
			}
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseTag(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseTag(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseTag(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseTimeSpentMinutes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid positive minutes",
			input:    "5",
			expected: intPtr(300), // 5 minutes = 300 seconds
		},
		{
			name:     "zero",
			input:    "0",
			expected: nil,
		},
		{
			name:     "negative",
			input:    "-5",
			expected: nil,
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "large value",
			input:    "120",
			expected: intPtr(7200), // 120 minutes = 7200 seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimeSpentMinutes(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseTimeSpentMinutes(%q) = %v, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseTimeSpentMinutes(%q) = nil, want %v", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseTimeSpentMinutes(%q) = %v, want %v", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid positive",
			input:    "5",
			expected: intPtr(5),
		},
		{
			name:     "zero",
			input:    "0",
			expected: nil,
		},
		{
			name:     "negative",
			input:    "-5",
			expected: nil,
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "large value",
			input:    "1000",
			expected: intPtr(1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseQuantity(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseQuantity(%q) = %v, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseQuantity(%q) = nil, want %v", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseQuantity(%q) = %v, want %v", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseOptionalString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "tabs and spaces only",
			input:    " \t \t ",
			expected: nil,
		},
		{
			name:     "newlines only",
			input:    "\n\n",
			expected: nil,
		},
		{
			name:     "valid string",
			input:    "These are my notes",
			expected: strPtr("These are my notes"),
		},
		{
			name:     "string with leading/trailing whitespace",
			input:    "  Some notes here  ",
			expected: strPtr("Some notes here"),
		},
		{
			name:     "string with internal whitespace",
			input:    "Notes with   multiple   spaces",
			expected: strPtr("Notes with   multiple   spaces"),
		},
		{
			name:     "single character",
			input:    "a",
			expected: strPtr("a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOptionalString(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseOptionalString(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseOptionalString(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseOptionalString(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseSourceType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *model.SourceType
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "youtube lowercase",
			input:    "youtube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "youtube uppercase",
			input:    "YOUTUBE",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "youtube mixed case",
			input:    "YouTube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "podcast lowercase",
			input:    "podcast",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "podcast uppercase",
			input:    "PODCAST",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "article lowercase",
			input:    "article",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
		{
			name:     "article uppercase",
			input:    "ARTICLE",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
		{
			name:     "doc lowercase",
			input:    "doc",
			expected: sourceTypePtr(model.SourceTypeDoc),
		},
		{
			name:     "doc uppercase",
			input:    "DOC",
			expected: sourceTypePtr(model.SourceTypeDoc),
		},
		{
			name:     "other lowercase",
			input:    "other",
			expected: sourceTypePtr(model.SourceTypeOther),
		},
		{
			name:     "other uppercase",
			input:    "OTHER",
			expected: sourceTypePtr(model.SourceTypeOther),
		},
		{
			name:     "invalid type",
			input:    "video",
			expected: nil,
		},
		{
			name:     "invalid type blog",
			input:    "blog",
			expected: nil,
		},
		{
			name:     "with leading whitespace",
			input:    "  youtube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "with trailing whitespace",
			input:    "podcast  ",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "with surrounding whitespace",
			input:    "  article  ",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSourceType(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseSourceType(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseSourceType(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseSourceType(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

// Helper functions for creating pointers
func intPtr(v int) *int {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func sourceTypePtr(v model.SourceType) *model.SourceType {
	return &v
}

// mockEntryRepo is a mock implementation of EntryRepo for testing
type mockEntryRepo struct {
	getByIDFn                         func(ctx context.Context, id uuid.UUID) (*model.Entry, error)
	createFn                          func(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error)
	updateFn                          func(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error)
	deleteFn                          func(ctx context.Context, id uuid.UUID) error
	listFn                            func(ctx context.Context, opts repository.ListOptions) ([]model.Entry, error)
	countFn                           func(ctx context.Context) (int, error)
	getLatestByNormalizedURLFn        func(ctx context.Context, normalizedURL string) (*repository.DuplicateEntry, error)
	countByNormalizedURLFn            func(ctx context.Context, normalizedURL string) (int, error)
	getDuplicateCountsByNormalizedURL func(ctx context.Context, normalizedURLs []string) (map[string]int, error)
	listByNormalizedURLFn             func(ctx context.Context, normalizedURL string) ([]model.Entry, error)
	resetEnrichmentFn                 func(ctx context.Context, id uuid.UUID) error
	resetSummaryFn                    func(ctx context.Context, id uuid.UUID) error

	// Track calls for assertions
	updateCalledWith *model.UpdateEntryInput
}

func (m *mockEntryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockEntryRepo) Create(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
	if m.createFn != nil {
		return m.createFn(ctx, input)
	}
	return nil, nil
}

func (m *mockEntryRepo) Update(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
	m.updateCalledWith = input
	if m.updateFn != nil {
		return m.updateFn(ctx, id, input)
	}
	return nil, nil
}

func (m *mockEntryRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockEntryRepo) List(ctx context.Context, opts repository.ListOptions) ([]model.Entry, error) {
	if m.listFn != nil {
		return m.listFn(ctx, opts)
	}
	return nil, nil
}

func (m *mockEntryRepo) Count(ctx context.Context) (int, error) {
	if m.countFn != nil {
		return m.countFn(ctx)
	}
	return 0, nil
}

func (m *mockEntryRepo) GetLatestByNormalizedURL(ctx context.Context, normalizedURL string) (*repository.DuplicateEntry, error) {
	if m.getLatestByNormalizedURLFn != nil {
		return m.getLatestByNormalizedURLFn(ctx, normalizedURL)
	}
	return nil, nil
}

func (m *mockEntryRepo) CountByNormalizedURL(ctx context.Context, normalizedURL string) (int, error) {
	if m.countByNormalizedURLFn != nil {
		return m.countByNormalizedURLFn(ctx, normalizedURL)
	}
	return 1, nil
}

func (m *mockEntryRepo) GetDuplicateCountsByNormalizedURL(ctx context.Context, normalizedURLs []string) (map[string]int, error) {
	if m.getDuplicateCountsByNormalizedURL != nil {
		return m.getDuplicateCountsByNormalizedURL(ctx, normalizedURLs)
	}
	return nil, nil
}

func (m *mockEntryRepo) ListByNormalizedURL(ctx context.Context, normalizedURL string) ([]model.Entry, error) {
	if m.listByNormalizedURLFn != nil {
		return m.listByNormalizedURLFn(ctx, normalizedURL)
	}
	return nil, nil
}

func (m *mockEntryRepo) ResetEnrichment(ctx context.Context, id uuid.UUID) error {
	if m.resetEnrichmentFn != nil {
		return m.resetEnrichmentFn(ctx, id)
	}
	return nil
}

func (m *mockEntryRepo) ResetSummary(ctx context.Context, id uuid.UUID) error {
	if m.resetSummaryFn != nil {
		return m.resetSummaryFn(ctx, id)
	}
	return nil
}

// createTestEntry creates a sample entry for testing
func createTestEntry(id uuid.UUID) *model.Entry {
	title := "Test Entry"
	description := "Test Description"
	summary := "Test Summary"
	return &model.Entry{
		ID:               id,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		SourceURL:        "https://example.com/test",
		NormalizedURL:    "example.com/test",
		Tag:              strPtr("test"),
		SourceType:       model.SourceTypeArticle,
		Title:            &title,
		Description:      &description,
		SummaryText:      &summary,
		EnrichmentStatus: model.StatusOK,
		SummaryStatus:    model.StatusOK,
	}
}

// setupTestHandler creates a chi router with the entry handler for testing
func setupTestHandler(repo EntryRepo) *chi.Mux {
	handler := NewEntryHandler(repo)
	r := chi.NewRouter()
	r.Get("/entries/{id}/edit", handler.EditPage)
	r.Put("/entries/{id}", handler.Update)
	r.Get("/entries/{id}/status", handler.Status)
	r.Post("/entries/{id}/refresh-enrichment", handler.RefreshEnrichment)
	r.Post("/entries/{id}/refresh-summary", handler.RefreshSummary)
	return r
}

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
			query: "?return_to=%2F%3Fpage%3D2",
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
				if tt.query == "?return_to=%2F%3Fpage%3D2" {
					for _, want := range []string{`href="/?page=2"`, `data-return-to="/?page=2"`, `evt.detail.elt === editForm && evt.detail.successful`} {
						if !strings.Contains(body, want) {
							t.Errorf("EditPage() body = %q, want to contain %q", body, want)
						}
					}
				}
			}
		})
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
		{
			name:   "refresh summary",
			method: http.MethodPost,
			path:   "/entries/" + id.String() + "/refresh-summary",
			setup: func(t *testing.T, m *mockEntryRepo) {
				m.resetSummaryFn = func(ctx context.Context, gotID uuid.UUID) error {
					if gotID != id {
						t.Fatalf("ResetSummary() id = %s, want %s", gotID, id)
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
				if input.Tag == nil || *input.Tag != "go" {
					t.Errorf("Update() tag = %v, want \"go\"", input.Tag)
				}
				if input.Title == nil || *input.Title != "Updated Title" {
					t.Errorf("Update() title = %v, want 'Updated Title'", input.Title)
				}
				if input.Description == nil || *input.Description != "Updated Description" {
					t.Errorf("Update() description = %v, want 'Updated Description'", input.Description)
				}
				if input.SummaryText == nil || *input.SummaryText != "Updated Summary" {
					t.Errorf("Update() summary = %v, want 'Updated Summary'", input.SummaryText)
				}
				if input.SourceType == nil || *input.SourceType != model.SourceTypeYouTube {
					t.Errorf("Update() sourceType = %v, want 'youtube'", input.SourceType)
				}
			},
		},
		{
			name: "update with empty optional fields sets nil",
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
				if input.Title != nil {
					t.Errorf("Update() title = %v, want nil for empty input", input.Title)
				}
				if input.Description != nil {
					t.Errorf("Update() description = %v, want nil for whitespace input", input.Description)
				}
				if input.SummaryText != nil {
					t.Errorf("Update() summary = %v, want nil for empty input", input.SummaryText)
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
				if input.Title == nil || *input.Title != "Valid Title" {
					t.Errorf("Update() title = %v, want 'Valid Title'", input.Title)
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
			expectedBody:   "Invalid tag",
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
