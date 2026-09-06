package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EntryRepository handles database operations for entries
type EntryRepository struct {
	pool *pgxpool.Pool
}

// NewEntryRepository creates a new EntryRepository
func NewEntryRepository(pool *pgxpool.Pool) *EntryRepository {
	return &EntryRepository{pool: pool}
}

// Create inserts a new entry
func (r *EntryRepository) Create(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
	query := `
		INSERT INTO entries (source_url, normalized_url, tag, time_spent_seconds, quantity, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
		          canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
		          enrichment_status, enrichment_error, enriched_at,
		          summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at
	`

	var entry model.Entry
	err := r.pool.QueryRow(ctx, query,
		input.SourceURL,
		input.NormalizedURL,
		input.Tag,
		input.TimeSpentSeconds,
		input.Quantity,
		input.Notes,
	).Scan(
		&entry.ID, &entry.CreatedAt, &entry.UpdatedAt, &entry.SourceURL, &entry.NormalizedURL, &entry.Tag,
		&entry.TimeSpentSeconds, &entry.Quantity, &entry.Notes,
		&entry.CanonicalURL, &entry.Domain, &entry.SourceType, &entry.Title, &entry.Description,
		&entry.PublishedAt, &entry.RuntimeSeconds, &entry.MetadataJSON,
		&entry.EnrichmentStatus, &entry.EnrichmentError, &entry.EnrichedAt,
		&entry.SummaryText, &entry.SummaryStatus, &entry.SummaryError,
		&entry.SummaryProvider, &entry.SummaryModel, &entry.SummaryVersion, &entry.SummaryGeneratedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create entry: %w", err)
	}

	return &entry, nil
}

// GetByID retrieves an entry by ID
func (r *EntryRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	query := `
		SELECT id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
		       canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
		       enrichment_status, enrichment_error, enriched_at,
		       summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at
		FROM entries
		WHERE id = $1
	`

	var entry model.Entry
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&entry.ID, &entry.CreatedAt, &entry.UpdatedAt, &entry.SourceURL, &entry.NormalizedURL, &entry.Tag,
		&entry.TimeSpentSeconds, &entry.Quantity, &entry.Notes,
		&entry.CanonicalURL, &entry.Domain, &entry.SourceType, &entry.Title, &entry.Description,
		&entry.PublishedAt, &entry.RuntimeSeconds, &entry.MetadataJSON,
		&entry.EnrichmentStatus, &entry.EnrichmentError, &entry.EnrichedAt,
		&entry.SummaryText, &entry.SummaryStatus, &entry.SummaryError,
		&entry.SummaryProvider, &entry.SummaryModel, &entry.SummaryVersion, &entry.SummaryGeneratedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}

	return &entry, nil
}

// ListOptions contains options for listing entries
type ListOptions struct {
	Limit  int
	Offset int
	Start  *time.Time
	End    *time.Time
}

// List retrieves entries with pagination
func (r *EntryRepository) List(ctx context.Context, opts ListOptions) ([]model.Entry, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	query := `
		SELECT id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
		       canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
		       enrichment_status, enrichment_error, enriched_at,
		       summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at
		FROM entries
	`

	var where []string
	var args []interface{}
	argPos := 1

	if opts.Start != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", argPos))
		args = append(args, *opts.Start)
		argPos++
	}
	if opts.End != nil {
		where = append(where, fmt.Sprintf("created_at <= $%d", argPos))
		args = append(args, *opts.End)
		argPos++
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	limitPos := argPos
	offsetPos := argPos + 1
	query += fmt.Sprintf("\n\t\tORDER BY created_at DESC\n\t\tLIMIT $%d OFFSET $%d\n\t", limitPos, offsetPos)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

// Update updates an entry's user-editable fields
func (r *EntryRepository) Update(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
	query := `
		UPDATE entries
		SET tag = $2, time_spent_seconds = $3, quantity = $4, notes = $5,
		    title = $6, description = $7, summary_text = $8, source_type = $9,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
		          canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
		          enrichment_status, enrichment_error, enriched_at,
		          summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at
	`

	var entry model.Entry
	err := r.pool.QueryRow(ctx, query, id, input.Tag, input.TimeSpentSeconds, input.Quantity, input.Notes,
		input.Title, input.Description, input.SummaryText, input.SourceType).Scan(
		&entry.ID, &entry.CreatedAt, &entry.UpdatedAt, &entry.SourceURL, &entry.NormalizedURL, &entry.Tag,
		&entry.TimeSpentSeconds, &entry.Quantity, &entry.Notes,
		&entry.CanonicalURL, &entry.Domain, &entry.SourceType, &entry.Title, &entry.Description,
		&entry.PublishedAt, &entry.RuntimeSeconds, &entry.MetadataJSON,
		&entry.EnrichmentStatus, &entry.EnrichmentError, &entry.EnrichedAt,
		&entry.SummaryText, &entry.SummaryStatus, &entry.SummaryError,
		&entry.SummaryProvider, &entry.SummaryModel, &entry.SummaryVersion, &entry.SummaryGeneratedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update entry: %w", err)
	}

	return &entry, nil
}

// Delete removes an entry
func (r *EntryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM entries WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete entry: %w", err)
	}
	return nil
}

// Count returns the total number of entries
func (r *EntryRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}
	return count, nil
}

// DuplicateEntry holds basic info about a duplicate match.
type DuplicateEntry struct {
	ID        uuid.UUID
	CreatedAt time.Time
	SourceURL string
	Title     *string
}

// GetLatestByNormalizedURL returns the most recent entry matching a normalized URL.
func (r *EntryRepository) GetLatestByNormalizedURL(ctx context.Context, normalizedURL string) (*DuplicateEntry, error) {
	query := `
		SELECT id, created_at, source_url, title
		FROM entries
		WHERE normalized_url = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	var entry DuplicateEntry
	err := r.pool.QueryRow(ctx, query, normalizedURL).Scan(
		&entry.ID, &entry.CreatedAt, &entry.SourceURL, &entry.Title,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get duplicate entry: %w", err)
	}

	return &entry, nil
}

// CountByNormalizedURL returns how many entries share the normalized URL.
func (r *EntryRepository) CountByNormalizedURL(ctx context.Context, normalizedURL string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM entries WHERE normalized_url = $1`, normalizedURL).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count duplicates: %w", err)
	}
	return count, nil
}

// GetDuplicateCountsByNormalizedURL returns counts for a list of normalized URLs.
func (r *EntryRepository) GetDuplicateCountsByNormalizedURL(ctx context.Context, normalizedURLs []string) (map[string]int, error) {
	counts := make(map[string]int)
	if len(normalizedURLs) == 0 {
		return counts, nil
	}

	query := `
		SELECT normalized_url, COUNT(*)
		FROM entries
		WHERE normalized_url = ANY($1)
		GROUP BY normalized_url
	`

	rows, err := r.pool.Query(ctx, query, normalizedURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to count duplicates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var normalizedURL string
		var count int
		if err := rows.Scan(&normalizedURL, &count); err != nil {
			return nil, fmt.Errorf("failed to scan duplicate counts: %w", err)
		}
		counts[normalizedURL] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return counts, nil
}

// ListByNormalizedURL retrieves entries matching the normalized URL.
func (r *EntryRepository) ListByNormalizedURL(ctx context.Context, normalizedURL string) ([]model.Entry, error) {
	query := `
		SELECT id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
		       canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
		       enrichment_status, enrichment_error, enriched_at,
		       summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at
		FROM entries
		WHERE normalized_url = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, normalizedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list duplicates: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

// EnrichmentResult holds the result of URL enrichment
type EnrichmentResult struct {
	CanonicalURL   string
	Domain         string
	SourceType     model.SourceType
	Title          string
	Description    string
	PublishedAt    *time.Time
	RuntimeSeconds *int
	MetadataJSON   []byte
}

// SummaryResult holds the result of summarization
type SummaryResult struct {
	Text        string
	Provider    string
	Model       string
	Version     string
	GeneratedAt time.Time
}

// ResetEnrichment resets enrichment status to pending
func (r *EntryRepository) ResetEnrichment(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE entries
		SET enrichment_status = 'pending', enrichment_error = NULL, enriched_at = NULL,
            enrichment_claim_token = NULL, enrichment_lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to reset enrichment: %w", err)
	}
	return nil
}

// TagAggregation represents aggregated stats for a tag
type TagAggregation struct {
	Tag         string
	Count       int
	TimeSeconds int
}

// TypeAggregation represents aggregated stats for a source type
type TypeAggregation struct {
	Type        string
	Count       int
	TimeSeconds int
}

// ReportTotals represents total counts for a date range
type ReportTotals struct {
	TotalEntries     int
	TotalTimeSeconds int
}

// AggregateByTag returns entry counts and time aggregated by tag for a date range
func (r *EntryRepository) AggregateByTag(ctx context.Context, start, end time.Time) ([]TagAggregation, error) {
	query := `
		SELECT tag, COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at <= $2 AND tag IS NOT NULL AND tag != ''
		GROUP BY tag
		ORDER BY COUNT(*) DESC
	`

	rows, err := r.pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by tag: %w", err)
	}
	defer rows.Close()

	var results []TagAggregation
	for rows.Next() {
		var agg TagAggregation
		if err := rows.Scan(&agg.Tag, &agg.Count, &agg.TimeSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan tag aggregation: %w", err)
		}
		results = append(results, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return results, nil
}

// AggregateByType returns entry counts and time aggregated by source type for a date range
func (r *EntryRepository) AggregateByType(ctx context.Context, start, end time.Time) ([]TypeAggregation, error) {
	query := `
		SELECT COALESCE(NULLIF(BTRIM(source_type), ''), ''), COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at <= $2
		GROUP BY COALESCE(NULLIF(BTRIM(source_type), ''), '')
		ORDER BY COUNT(*) DESC
	`

	rows, err := r.pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by type: %w", err)
	}
	defer rows.Close()

	var results []TypeAggregation
	for rows.Next() {
		var agg TypeAggregation
		if err := rows.Scan(&agg.Type, &agg.Count, &agg.TimeSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan type aggregation: %w", err)
		}
		results = append(results, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return results, nil
}

// GetReportTotals returns total entry count and time for a date range
func (r *EntryRepository) GetReportTotals(ctx context.Context, start, end time.Time) (*ReportTotals, error) {
	query := `
		SELECT COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at <= $2
	`

	var totals ReportTotals
	err := r.pool.QueryRow(ctx, query, start, end).Scan(&totals.TotalEntries, &totals.TotalTimeSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to get report totals: %w", err)
	}

	return &totals, nil
}

// scanEntries scans rows into entries slice
func scanEntries(rows pgx.Rows) ([]model.Entry, error) {
	var entries []model.Entry
	for rows.Next() {
		var entry model.Entry
		err := rows.Scan(
			&entry.ID, &entry.CreatedAt, &entry.UpdatedAt, &entry.SourceURL, &entry.NormalizedURL, &entry.Tag,
			&entry.TimeSpentSeconds, &entry.Quantity, &entry.Notes,
			&entry.CanonicalURL, &entry.Domain, &entry.SourceType, &entry.Title, &entry.Description,
			&entry.PublishedAt, &entry.RuntimeSeconds, &entry.MetadataJSON,
			&entry.EnrichmentStatus, &entry.EnrichmentError, &entry.EnrichedAt,
			&entry.SummaryText, &entry.SummaryStatus, &entry.SummaryError,
			&entry.SummaryProvider, &entry.SummaryModel, &entry.SummaryVersion, &entry.SummaryGeneratedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan entry: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
