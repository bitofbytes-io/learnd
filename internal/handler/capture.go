package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/ui/pages"
)

const dashboardPageSize = 20

// CaptureHandler handles the main capture UI
type CaptureHandler struct {
	entryRepo EntryRepo
}

// NewCaptureHandler creates a new CaptureHandler
func NewCaptureHandler(entryRepo EntryRepo) *CaptureHandler {
	return &CaptureHandler{
		entryRepo: entryRepo,
	}
}

// CapturePage renders the main capture page
func (h *CaptureHandler) CapturePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := parseDashboardPage(r)

	totalEntries, err := h.entryRepo.Count(ctx)
	if err != nil {
		slog.Error("failed to count entries", "handler", "CapturePage", "error", err)
		http.Error(w, "Failed to load entries", http.StatusInternalServerError)
		return
	}

	totalPages := totalDashboardPages(totalEntries)
	if page > totalPages {
		page = totalPages
	}
	offset := dashboardOffset(page)

	entries, err := h.entryRepo.List(ctx, repository.ListOptions{Limit: dashboardPageSize, Offset: offset})
	if err != nil {
		slog.Error("failed to list entries", "handler", "CapturePage", "page", page, "offset", offset, "error", err)
		http.Error(w, "Failed to load entries", http.StatusInternalServerError)
		return
	}

	entryViews := buildEntryViews(ctx, h.entryRepo, entries)
	stampDashboardEditURLs(entryViews, dashboardPagePath(page))
	pagination := buildDashboardPagination(page, totalPages)

	if isHTMXRequest(r) {
		if err := pages.CaptureEntriesSection(entryViews, pagination).Render(ctx, w); err != nil {
			slog.Error("failed to render dashboard entries section", "handler", "CapturePage", "error", err)
		}
		return
	}

	// Check for URL prefill from query param
	prefillURL := r.URL.Query().Get("url")

	if err := pages.CapturePage(entryViews, pagination, prefillURL).Render(ctx, w); err != nil {
		// Log only - response may already be partially written, can't send clean http.Error
		slog.Error("failed to render page", "handler", "CapturePage", "error", err)
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

func buildDashboardPagination(page, totalPages int) ui.PaginationView {
	pagination := ui.PaginationView{
		CurrentPage: page,
		TotalPages:  totalPages,
		HasPrevious: page > 1,
		HasNext:     page < totalPages,
	}
	if pagination.HasPrevious {
		pagination.PreviousURL = dashboardPagePath(page - 1)
	}
	if pagination.HasNext {
		pagination.NextURL = dashboardPagePath(page + 1)
	}
	return pagination
}

func dashboardPagePath(page int) string {
	if page <= 1 {
		return "/"
	}
	return fmt.Sprintf("/?page=%d", page)
}

func stampDashboardEditURLs(entries []ui.EntryView, returnTo string) {
	for i := range entries {
		entries[i].EditURL = entryEditURL(entries[i].ID.String(), returnTo)
	}
}

func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
