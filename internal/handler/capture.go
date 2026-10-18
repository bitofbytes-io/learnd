package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/urlutil"
)

const dashboardPageSize = 20

// CaptureHandler handles the main capture UI
type CaptureHandler struct {
	entryRepo      EntryRepo
	summaryEnabled bool
}

// NewCaptureHandler creates a new CaptureHandler
func NewCaptureHandler(entryRepo EntryRepo, summaryEnabled bool) *CaptureHandler {
	return &CaptureHandler{
		entryRepo:      entryRepo,
		summaryEnabled: summaryEnabled,
	}
}

// CapturePage renders the main capture page
func (h *CaptureHandler) CapturePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := parseDashboardPage(r)
	filter := parseDashboardFilter(r.URL.Query())
	opts := dashboardListOptions(filter)

	totalEntries, err := h.entryRepo.CountFiltered(ctx, opts)
	if err != nil {
		slog.Error("failed to count entries", "handler", "CapturePage", "error", err)
		http.Error(w, "Failed to load entries", http.StatusInternalServerError)
		return
	}

	totalPages := totalDashboardPages(totalEntries)
	if page > totalPages {
		page = totalPages
	}
	opts.Limit = dashboardPageSize
	opts.Offset = dashboardOffset(page)

	entries, err := h.entryRepo.List(ctx, opts)
	if err != nil {
		slog.Error("failed to list entries", "handler", "CapturePage", "page", page, "offset", opts.Offset, "error", err)
		http.Error(w, "Failed to load entries", http.StatusInternalServerError)
		return
	}

	entryViews := buildEntryViews(ctx, h.entryRepo, entries, h.summaryEnabled)
	stampDashboardEditURLs(entryViews, dashboardPagePath(page, filter))
	pagination := buildDashboardPagination(page, totalPages, filter)

	if isDashboardEntriesRequest(r) {
		// The filter form sends empty fields too; push the canonical URL instead.
		w.Header().Set("HX-Push-Url", dashboardPagePath(page, filter))
		if err := pages.CaptureEntriesSection(entryViews, pagination, filter, totalEntries).Render(ctx, w); err != nil {
			slog.Error("failed to render dashboard entries section", "handler", "CapturePage", "error", err)
		}
		return
	}

	// The tag select is a convenience; without it the dashboard still works.
	if filter.Tags, err = h.entryRepo.ListTags(ctx); err != nil {
		slog.Warn("failed to list tags", "handler", "CapturePage", "error", err)
	}
	if filter.Tag != "" && !slices.Contains(filter.Tags, filter.Tag) {
		filter.Tags = append(filter.Tags, filter.Tag)
	}

	prefillURL := sharedPrefillURL(r.URL.Query())

	if err := pages.CapturePage(entryViews, pagination, filter, totalEntries, prefillURL).Render(ctx, w); err != nil {
		// Log only - response may already be partially written, can't send clean http.Error
		slog.Error("failed to render page", "handler", "CapturePage", "error", err)
	}
}

// sharedPrefillURL picks the URL to prefill from ?url= or, failing that, the
// first link in ?text= (Android share targets usually send the link there).
// Invalid URLs are dropped silently so a bad share just leaves the field empty.
func sharedPrefillURL(q url.Values) string {
	if valid, err := urlutil.ValidateSourceURL(q.Get("url")); err == nil {
		return valid
	}
	for _, token := range strings.Fields(q.Get("text")) {
		start := strings.Index(strings.ToLower(token), "http")
		if start < 0 {
			continue
		}
		if valid, err := urlutil.ValidateSourceURL(trimSharedLinkSuffix(token[start:])); err == nil {
			return valid
		}
	}
	return ""
}

// trimSharedLinkSuffix strips sentence punctuation and closing wrappers from a
// link found in shared text, keeping a ")" that balances one inside the URL.
func trimSharedLinkSuffix(link string) string {
	for {
		trimmed := strings.TrimRight(link, ".,;:!?\"'>]”’")
		if strings.HasSuffix(trimmed, ")") && strings.Count(trimmed, ")") > strings.Count(trimmed, "(") {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if trimmed == link {
			return link
		}
		link = trimmed
	}
}

func parseDashboardPage(r *http.Request) int {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	return page
}

// maxDashboardQueryLength caps the search text, in runes, sent to Postgres.
const maxDashboardQueryLength = 200

// parseDashboardFilter reads ?q=, ?tag=, and ?type=. An invalid tag or type is
// ignored rather than rejected, so a stale link still shows the dashboard.
func parseDashboardFilter(q url.Values) ui.DashboardFilter {
	query := []rune(strings.TrimSpace(q.Get("q")))
	if len(query) > maxDashboardQueryLength {
		query = query[:maxDashboardQueryLength]
	}
	filter := ui.DashboardFilter{Query: strings.TrimSpace(string(query))}
	if tag, err := parseTag(q.Get("tag")); err == nil && tag != nil {
		filter.Tag = *tag
	}
	if sourceType := parseSourceType(q.Get("type")); sourceType != nil {
		filter.SourceType = string(*sourceType)
	}
	return filter
}

func dashboardListOptions(filter ui.DashboardFilter) repository.ListOptions {
	var opts repository.ListOptions
	if filter.Query != "" {
		opts.Query = &filter.Query
	}
	if filter.Tag != "" {
		opts.Tag = &filter.Tag
	}
	if filter.SourceType != "" {
		sourceType := model.SourceType(filter.SourceType)
		opts.SourceType = &sourceType
	}
	return opts
}

func totalDashboardPages(totalEntries int) int {
	if totalEntries <= 0 {
		return 1
	}

	totalPages := totalEntries / dashboardPageSize
	if totalEntries%dashboardPageSize != 0 {
		totalPages++
	}
	return totalPages
}

func dashboardOffset(page int) int {
	if page <= 1 {
		return 0
	}
	return (page - 1) * dashboardPageSize
}

func buildDashboardPagination(page, totalPages int, filter ui.DashboardFilter) ui.PaginationView {
	pagination := ui.PaginationView{
		CurrentPage: page,
		TotalPages:  totalPages,
		HasPrevious: page > 1,
		HasNext:     page < totalPages,
	}
	if pagination.HasPrevious {
		pagination.PreviousURL = dashboardPagePath(page-1, filter)
	}
	if pagination.HasNext {
		pagination.NextURL = dashboardPagePath(page+1, filter)
	}
	return pagination
}

// dashboardPagePath returns the dashboard URL for page with filter applied.
func dashboardPagePath(page int, filter ui.DashboardFilter) string {
	values := url.Values{}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if filter.Query != "" {
		values.Set("q", filter.Query)
	}
	if filter.Tag != "" {
		values.Set("tag", filter.Tag)
	}
	if filter.SourceType != "" {
		values.Set("type", filter.SourceType)
	}
	if len(values) == 0 {
		return "/"
	}
	return "/?" + values.Encode()
}

func stampDashboardEditURL(entry *ui.EntryView, returnTo string) {
	if entry == nil {
		return
	}
	entry.EditURL = entryEditURL(entry.ID.String(), returnTo)
}

func stampDashboardEditURLs(entries []ui.EntryView, returnTo string) {
	for i := range entries {
		stampDashboardEditURL(&entries[i], returnTo)
	}
}

func isDashboardEntriesRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-Target") == "dashboard-entries-region" &&
		r.Header.Get("HX-Boosted") != "true"
}
