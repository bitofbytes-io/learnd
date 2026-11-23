package repository

import (
	"fmt"

	"github.com/drywaters/learnd/internal/model"
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

// entryColumns lists the entry columns in the order scanEntry reads them.
const entryColumns = `id, created_at, updated_at, source_url, normalized_url, tag, time_spent_seconds, quantity, notes,
	canonical_url, domain, source_type, title, description, published_at, runtime_seconds, metadata_json,
	enrichment_status, enrichment_error, enriched_at,
	summary_text, summary_status, summary_error, summary_provider, summary_model, summary_version, summary_generated_at`

// scanEntry scans one row selected with entryColumns.
func scanEntry(row pgx.Row) (*model.Entry, error) {
	var entry model.Entry
	err := row.Scan(
		&entry.ID, &entry.CreatedAt, &entry.UpdatedAt, &entry.SourceURL, &entry.NormalizedURL, &entry.Tag,
		&entry.TimeSpentSeconds, &entry.Quantity, &entry.Notes,
		&entry.CanonicalURL, &entry.Domain, &entry.SourceType, &entry.Title, &entry.Description,
		&entry.PublishedAt, &entry.RuntimeSeconds, &entry.MetadataJSON,
		&entry.EnrichmentStatus, &entry.EnrichmentError, &entry.EnrichedAt,
		&entry.SummaryText, &entry.SummaryStatus, &entry.SummaryError,
		&entry.SummaryProvider, &entry.SummaryModel, &entry.SummaryVersion, &entry.SummaryGeneratedAt,
	)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// scanEntries scans rows selected with entryColumns into a slice.
func scanEntries(rows pgx.Rows) ([]model.Entry, error) {
	var entries []model.Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan entry: %w", err)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return entries, nil
}
