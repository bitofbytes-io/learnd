package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
)

// ListCreatedBetween returns up to limit entries created in [start, end),
// newest first, ordered by (created_at, id) so pages never skip or repeat an
// entry. Pass the last entry of the previous page as after, or nil to start.
func (r *EntryRepository) ListCreatedBetween(ctx context.Context, start, end time.Time, after *model.Entry, limit int) ([]model.Entry, error) {
	var afterCreatedAt *time.Time
	var afterID *uuid.UUID
	if after != nil {
		afterCreatedAt, afterID = &after.CreatedAt, &after.ID
	}

	query := `SELECT ` + entryColumns + ` FROM entries
		WHERE created_at >= $1 AND created_at < $2
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $5`

	rows, err := r.pool.Query(ctx, query, start, end, afterCreatedAt, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
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

// AggregateByTag returns entry counts and time aggregated by tag for entries
// created in [start, end), longest time first. Untagged entries form one row
// with an empty Tag, so the rows sum to GetReportTotals. Each entry carries a
// single tag, so no entry is counted twice.
func (r *EntryRepository) AggregateByTag(ctx context.Context, start, end time.Time) ([]TagAggregation, error) {
	query := `
		SELECT COALESCE(tag, ''), COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY COALESCE(tag, '')
		ORDER BY 3 DESC, 2 DESC, 1
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

// AggregateByType returns entry counts and time aggregated by source type for
// entries created in [start, end)
func (r *EntryRepository) AggregateByType(ctx context.Context, start, end time.Time) ([]TypeAggregation, error) {
	query := `
		SELECT COALESCE(NULLIF(BTRIM(source_type), ''), ''), COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at < $2
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

// GetReportTotals returns total entry count and time for entries created in
// [start, end)
func (r *EntryRepository) GetReportTotals(ctx context.Context, start, end time.Time) (*ReportTotals, error) {
	query := `
		SELECT COUNT(*), COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at < $2
	`

	var totals ReportTotals
	err := r.pool.QueryRow(ctx, query, start, end).Scan(&totals.TotalEntries, &totals.TotalTimeSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to get report totals: %w", err)
	}

	return &totals, nil
}

// WeekAggregation represents aggregated stats for one calendar week
type WeekAggregation struct {
	WeekStart   time.Time // the week's Monday, as a UTC midnight
	Count       int
	TimeSeconds int
}

// AggregateByWeek returns entry counts and time per Monday-start week, in
// the IANA zone tz, for entries created in [start, end). Weeks without
// entries are omitted.
func (r *EntryRepository) AggregateByWeek(ctx context.Context, start, end time.Time, tz string) ([]WeekAggregation, error) {
	query := `
		SELECT date_trunc('week', created_at AT TIME ZONE $3)::date AS week, COUNT(*),
		       COALESCE(SUM(COALESCE(time_spent_seconds, runtime_seconds, 0)), 0)::int
		FROM entries
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY week
		ORDER BY week
	`

	rows, err := r.pool.Query(ctx, query, start, end, tz)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by week: %w", err)
	}
	defer rows.Close()

	var results []WeekAggregation
	for rows.Next() {
		var agg WeekAggregation
		if err := rows.Scan(&agg.WeekStart, &agg.Count, &agg.TimeSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan week aggregation: %w", err)
		}
		results = append(results, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return results, nil
}

// CountActiveDays returns how many calendar days, in the IANA zone tz, have
// at least one entry created in [start, end)
func (r *EntryRepository) CountActiveDays(ctx context.Context, start, end time.Time, tz string) (int, error) {
	query := `
		SELECT COUNT(DISTINCT (created_at AT TIME ZONE $3)::date)
		FROM entries
		WHERE created_at >= $1 AND created_at < $2
	`

	var days int
	if err := r.pool.QueryRow(ctx, query, start, end, tz).Scan(&days); err != nil {
		return 0, fmt.Errorf("failed to count active days: %w", err)
	}

	return days, nil
}
