package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetByID retrieves an entry by ID
func (r *EntryRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	query := `SELECT ` + entryColumns + ` FROM entries WHERE id = $1`

	entry, err := scanEntry(r.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}

	return entry, nil
}

// ListOptions contains options for listing entries. Nil filters match every
// entry.
type ListOptions struct {
	Limit  int
	Offset int
	// Query is a websearch-style full-text query over title, notes,
	// description, and summary.
	Query      *string
	Tag        *string
	SourceType *model.SourceType
	// Failed keeps only entries whose enrichment or summary failed.
	Failed bool
	// ID narrows the filters to one entry, to check whether it still matches.
	ID *uuid.UUID
}

// filter returns the WHERE clause for the options' filters and its arguments,
// numbered from $1.
func (opts ListOptions) filter() (string, []any) {
	var conditions []string
	var args []any
	add := func(condition string, arg any) {
		args = append(args, arg)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if opts.Query != nil {
		add(`search @@ websearch_to_tsquery('english', $%d)`, *opts.Query)
	}
	if opts.Tag != nil {
		add(`tag = $%d`, *opts.Tag)
	}
	if opts.SourceType != nil {
		add(`source_type = $%d`, string(*opts.SourceType))
	}
	if opts.Failed {
		conditions = append(conditions, `(enrichment_status = 'failed' OR summary_status = 'failed')`)
	}
	if opts.ID != nil {
		add(`id = $%d`, *opts.ID)
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return ` WHERE ` + strings.Join(conditions, ` AND `), args
}

// List retrieves entries matching the options' filters with pagination
func (r *EntryRepository) List(ctx context.Context, opts ListOptions) ([]model.Entry, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	where, args := opts.filter()
	query := fmt.Sprintf(`SELECT %s FROM entries%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		entryColumns, where, len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, query, append(args, opts.Limit, opts.Offset)...)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

// CountFiltered returns the number of entries matching the options' filters;
// Limit and Offset are ignored.
func (r *EntryRepository) CountFiltered(ctx context.Context, opts ListOptions) (int, error) {
	where, args := opts.filter()
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM entries`+where, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}
	return count, nil
}

// ListTags returns every tag in use, alphabetically.
func (r *EntryRepository) ListTags(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT tag FROM entries WHERE tag IS NOT NULL AND tag != '' ORDER BY tag`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	tags, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	return tags, nil
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
	query := `SELECT ` + entryColumns + ` FROM entries WHERE normalized_url = $1 ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, normalizedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list duplicates: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}
