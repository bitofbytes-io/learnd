package handler

import (
	"context"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/urlutil"
)

func buildEntryView(entry *model.Entry, duplicateCount int) ui.EntryView {
	sourceHref, hasSourceHref := urlutil.SafeLinkURL(entry.SourceURL)
	return ui.EntryView{
		Entry:          *entry,
		DuplicateCount: duplicateCount,
		SwapOOB:        false,
		SourceHref:     sourceHref,
		HasSourceHref:  hasSourceHref,
	}
}

// getDuplicateCount returns the number of entries sharing the same normalized URL.
func getDuplicateCount(ctx context.Context, repo EntryRepo, entry *model.Entry) int {
	normalizedURL := entry.NormalizedURL
	if normalizedURL == "" {
		normalizedURL = entry.SourceURL
	}
	if normalizedURL != "" {
		if count, err := repo.CountByNormalizedURL(ctx, normalizedURL); err == nil && count > 0 {
			return count
		}
	}
	return 1
}

func buildEntryViews(ctx context.Context, repo EntryRepo, entries []model.Entry) []ui.EntryView {
	views := make([]ui.EntryView, 0, len(entries))
	if len(entries) == 0 {
		return views
	}

	normalizedURLs := make([]string, 0, len(entries))
	for _, entry := range entries {
		normalizedURL := entry.NormalizedURL
		if normalizedURL == "" {
			normalizedURL = entry.SourceURL
		}
		if normalizedURL != "" {
			normalizedURLs = append(normalizedURLs, normalizedURL)
		}
	}

	counts := map[string]int{}
	if len(normalizedURLs) > 0 {
		if fetched, err := repo.GetDuplicateCountsByNormalizedURL(ctx, normalizedURLs); err == nil {
			counts = fetched
		}
	}

	for _, entry := range entries {
		normalizedURL := entry.NormalizedURL
		if normalizedURL == "" {
			normalizedURL = entry.SourceURL
		}

		duplicateCount := 1
		if normalizedURL != "" {
			if count, ok := counts[normalizedURL]; ok && count > 0 {
				duplicateCount = count
			}
		}

		sourceHref, hasSourceHref := urlutil.SafeLinkURL(entry.SourceURL)
		views = append(views, ui.EntryView{
			Entry:          entry,
			DuplicateCount: duplicateCount,
			SwapOOB:        false,
			SourceHref:     sourceHref,
			HasSourceHref:  hasSourceHref,
		})
	}

	return views
}
