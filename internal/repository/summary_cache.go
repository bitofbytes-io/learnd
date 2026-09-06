package repository

import (
	"context"
	"fmt"

	"github.com/drywaters/learnd/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SummaryCacheRepository handles database operations for summary cache
type SummaryCacheRepository struct {
	pool *pgxpool.Pool
}

// NewSummaryCacheRepository creates a new SummaryCacheRepository
func NewSummaryCacheRepository(pool *pgxpool.Pool) *SummaryCacheRepository {
	return &SummaryCacheRepository{pool: pool}
}

// GetByURLHash retrieves a cached summary by URL hash
func (r *SummaryCacheRepository) GetByURLHash(ctx context.Context, urlHash string) (*model.SummaryCache, error) {
	query := `
		SELECT id, url_hash, canonical_url, summary_text, provider, model, version, created_at
		FROM summary_cache
		WHERE url_hash = $1
	`

	var cache model.SummaryCache
	err := r.pool.QueryRow(ctx, query, urlHash).Scan(
		&cache.ID, &cache.URLHash, &cache.CanonicalURL, &cache.SummaryText,
		&cache.Provider, &cache.Model, &cache.Version, &cache.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get summary cache: %w", err)
	}

	return &cache, nil
}

// Store saves a summary to the cache
func (r *SummaryCacheRepository) Store(ctx context.Context, cache *model.SummaryCache) error {

	_, err := r.pool.Exec(ctx, storeSummaryCacheSQL,
		cache.URLHash, cache.CanonicalURL, cache.SummaryText,
		cache.Provider, cache.Model, cache.Version,
	)
	if err != nil {
		return fmt.Errorf("failed to store summary cache: %w", err)
	}

	return nil
}

const storeSummaryCacheSQL = `
		INSERT INTO summary_cache (url_hash, canonical_url, summary_text, provider, model, version)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (url_hash) DO UPDATE SET
			summary_text = EXCLUDED.summary_text,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			version = EXCLUDED.version,
			created_at = NOW()
	`
