package handler

import (
	"context"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/urlutil"
)

func buildEntryView(entry *model.Entry, duplicateCount int, summaryEnabled bool) ui.EntryView {
	sourceHref, hasSourceHref := urlutil.SafeLinkURL(entry.SourceURL)
	return ui.EntryView{
		Entry:          *entry,
		DuplicateCount: duplicateCount,
		SwapOOB:        false,
		SourceHref:     sourceHref,
		HasSourceHref:  hasSourceHref,
		SummaryEnabled: summaryEnabled,
	}
}

// duplicateKey is the URL that entries are grouped by when counting duplicates.
func duplicateKey(entry *model.Entry) string {
	if entry.NormalizedURL != "" {
		return entry.NormalizedURL
	}
	return entry.SourceURL
}

// getDuplicateCount returns the number of entries sharing the same normalized URL.
func getDuplicateCount(ctx context.Context, repo EntryRepo, entry *model.Entry) int {
	if key := duplicateKey(entry); key != "" {
		if count, err := repo.CountByNormalizedURL(ctx, key); err == nil && count > 0 {
			return count
		}
	}
	return 1
}

// buildEntryViews builds views for a page of entries, counting duplicates in
// one query.
func buildEntryViews(ctx context.Context, repo EntryRepo, entries []model.Entry, summaryEnabled bool) []ui.EntryView {
	views := make([]ui.EntryView, 0, len(entries))
	if len(entries) == 0 {
		return views
	}

	keys := make([]string, 0, len(entries))
	for i := range entries {
		if key := duplicateKey(&entries[i]); key != "" {
			keys = append(keys, key)
		}
	}

	counts := map[string]int{}
	if len(keys) > 0 {
		if fetched, err := repo.GetDuplicateCountsByNormalizedURL(ctx, keys); err == nil {
			counts = fetched
		}
	}

	for i := range entries {
		duplicateCount := 1
		if count := counts[duplicateKey(&entries[i])]; count > 0 {
			duplicateCount = count
		}
		views = append(views, buildEntryView(&entries[i], duplicateCount, summaryEnabled))
	}

	return views
}
