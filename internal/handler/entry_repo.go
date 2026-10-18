package handler

import (
	"context"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/google/uuid"
)

// EntryRepo defines the interface for entry repository operations.
// This interface allows for easier testing with mock implementations.
type EntryRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error)
	Create(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error)
	Update(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts repository.ListOptions) ([]model.Entry, error)
	Count(ctx context.Context) (int, error)
	CountFiltered(ctx context.Context, opts repository.ListOptions) (int, error)
	ListTags(ctx context.Context) ([]string, error)
	GetLatestByNormalizedURL(ctx context.Context, normalizedURL string) (*repository.DuplicateEntry, error)
	CountByNormalizedURL(ctx context.Context, normalizedURL string) (int, error)
	GetDuplicateCountsByNormalizedURL(ctx context.Context, normalizedURLs []string) (map[string]int, error)
	ListByNormalizedURL(ctx context.Context, normalizedURL string) ([]model.Entry, error)
	ResetEnrichment(ctx context.Context, id uuid.UUID) error
	ResetSummary(ctx context.Context, id uuid.UUID) error
}
