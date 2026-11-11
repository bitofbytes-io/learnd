package handler

import (
	"context"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
	countFilteredFn                   func(ctx context.Context, opts repository.ListOptions) (int, error)
	listTagsFn                        func(ctx context.Context) ([]string, error)
	getLatestByNormalizedURLFn        func(ctx context.Context, normalizedURL string) (*repository.DuplicateEntry, error)
	countByNormalizedURLFn            func(ctx context.Context, normalizedURL string) (int, error)
	getDuplicateCountsByNormalizedURL func(ctx context.Context, normalizedURLs []string) (map[string]int, error)
	listByNormalizedURLFn             func(ctx context.Context, normalizedURL string) ([]model.Entry, error)
	resetEnrichmentFn                 func(ctx context.Context, id uuid.UUID) error
	resetSummaryFn                    func(ctx context.Context, id uuid.UUID) error
	countFailedJobsFn                 func(ctx context.Context) (repository.FailedJobs, error)
	retryFailedJobsFn                 func(ctx context.Context, includeSummaries bool) (repository.FailedJobs, error)

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

func (m *mockEntryRepo) CountFiltered(ctx context.Context, opts repository.ListOptions) (int, error) {
	if m.countFilteredFn != nil {
		return m.countFilteredFn(ctx, opts)
	}
	return 0, nil
}

func (m *mockEntryRepo) ListTags(ctx context.Context) ([]string, error) {
	if m.listTagsFn != nil {
		return m.listTagsFn(ctx)
	}
	return nil, nil
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

func (m *mockEntryRepo) CountFailedJobs(ctx context.Context) (repository.FailedJobs, error) {
	if m.countFailedJobsFn != nil {
		return m.countFailedJobsFn(ctx)
	}
	return repository.FailedJobs{}, nil
}

func (m *mockEntryRepo) RetryFailedJobs(ctx context.Context, includeSummaries bool) (repository.FailedJobs, error) {
	if m.retryFailedJobsFn != nil {
		return m.retryFailedJobsFn(ctx, includeSummaries)
	}
	return repository.FailedJobs{}, nil
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
	handler := NewEntryHandler(repo, false)
	r := chi.NewRouter()
	r.Get("/entries/{id}/edit", handler.EditPage)
	r.Put("/entries/{id}", handler.Update)
	r.Get("/entries/{id}/status", handler.Status)
	r.Post("/entries/{id}/refresh-enrichment", handler.RefreshEnrichment)
	r.Post("/entries/{id}/refresh-summary", handler.RefreshSummary)
	return r
}
