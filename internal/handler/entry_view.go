package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/ui/partials"
	"github.com/drywaters/learnd/internal/urlutil"
	"github.com/google/uuid"
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

// renderRow writes the entry's row, keeping the dashboard page in its edit link.
func (h *EntryHandler) renderRow(w http.ResponseWriter, r *http.Request, entry *model.Entry) {
	entryView := buildEntryView(entry, getDuplicateCount(r.Context(), h.entryRepo, entry), h.summaryEnabled)
	stampDashboardEntryViewFromRequest(r, &entryView)
	if err := partials.EntryRow(entryView).Render(r.Context(), w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}
}

// renderFailedBanner re-renders the dashboard's failed-jobs banner out of band,
// so its count follows the rows as they fail or are retried.
func (h *EntryHandler) renderFailedBanner(w http.ResponseWriter, r *http.Request) {
	var filter ui.DashboardFilter
	if parsed, err := url.Parse(dashboardReturnTo(r)); err == nil {
		filter = parseDashboardFilter(parsed.Query())
	}
	failed := loadFailedJobs(r.Context(), h.entryRepo)
	if err := pages.FailedJobsBanner(filter, failed, true).Render(r.Context(), w); err != nil {
		slog.Warn("response render stopped", "error", err)
	}
}

// renderDuplicateRows re-renders, out of band, every entry sharing
// normalizedURL except skipID, so their duplicate counts stay current.
func (h *EntryHandler) renderDuplicateRows(w http.ResponseWriter, r *http.Request, normalizedURL string, skipID uuid.UUID) {
	if normalizedURL == "" {
		return
	}
	duplicates, err := h.entryRepo.ListByNormalizedURL(r.Context(), normalizedURL)
	if err != nil {
		return
	}
	for _, duplicate := range duplicates {
		if duplicate.ID == skipID {
			continue
		}
		duplicateView := buildEntryView(&duplicate, len(duplicates), h.summaryEnabled)
		duplicateView.SwapOOB = true
		if err := partials.EntryRow(duplicateView).Render(r.Context(), w); err != nil {
			slog.Warn("response render stopped", "error", err)
			return
		}
	}
}
