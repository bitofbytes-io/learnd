package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/ui/partials"
	"github.com/drywaters/learnd/internal/urlutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// EntryHandler handles entry CRUD operations
type EntryHandler struct {
	entryRepo      EntryRepo
	summaryEnabled bool
}

// NewEntryHandler creates a new EntryHandler
func NewEntryHandler(entryRepo EntryRepo, summaryEnabled bool) *EntryHandler {
	return &EntryHandler{
		entryRepo:      entryRepo,
		summaryEnabled: summaryEnabled,
	}
}

// Create handles creating a new entry
func (h *EntryHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		h.htmxError(w, "Invalid form data")
		return
	}

	sourceURL, err := urlutil.ValidateSourceURL(r.FormValue("url"))
	if err != nil {
		h.htmxError(w, err.Error())
		return
	}

	allowDuplicate := r.FormValue("allow_duplicate") == "1"

	normalizedURL, err := urlutil.NormalizeURL(sourceURL)
	if err != nil {
		h.htmxError(w, "Invalid URL")
		return
	}

	if !allowDuplicate {
		existing, err := h.entryRepo.GetLatestByNormalizedURL(ctx, normalizedURL)
		if err != nil {
			h.htmxError(w, "Failed to check duplicates")
			return
		}
		if existing != nil {
			title := ""
			if existing.Title != nil && *existing.Title != "" {
				title = *existing.Title
			}

			if err := partials.DuplicateWarning(true, title, existing.CreatedAt).Render(ctx, w); err != nil {
				slog.Warn("response render stopped", "error", err)
			}
			return
		}
	}

	// Parse optional fields
	tag, err := parseTag(r.FormValue("tag"))
	if err != nil {
		h.htmxError(w, err.Error())
		return
	}
	timeSpent := parseTimeSpentMinutes(r.FormValue("time_spent"))
	quantity := parseQuantity(r.FormValue("quantity"))
	notes := parseOptionalString(r.FormValue("notes"))

	input := &model.CreateEntryInput{
		SourceURL:        sourceURL,
		NormalizedURL:    normalizedURL,
		Tag:              tag,
		TimeSpentSeconds: timeSpent,
		Quantity:         quantity,
		Notes:            notes,
	}

	entry, err := h.entryRepo.Create(ctx, input)
	if err != nil {
		h.htmxError(w, "Failed to create entry")
		return
	}

	slog.Info("entry created", "entry_id", entry.ID, "allow_duplicate", allowDuplicate)
	if redirectURL := captureRedirectAfterCreate(r); redirectURL != "" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("X-Entry-Created", "true")

	// Trigger toast and return the new entry row
	h.htmxToast(w, "Entry saved", &entry.ID, "")

	h.renderRow(w, r, entry)
	h.renderDuplicateRows(w, r, entry.NormalizedURL, entry.ID)

	// Render OOB swap to remove empty state
	if err := partials.EmptyState(false, true).Render(ctx, w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}

	// Clear duplicate warning
	if err := partials.DuplicateWarning(false, "", time.Time{}).Render(ctx, w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}

	// Clear form error
	if _, err := fmt.Fprint(w, `<div id="form-error" hx-swap-oob="true"></div>`); err != nil {
		slog.Warn("response write stopped", "error", err)
	}
}

// EditPage renders the edit form for an entry
func (h *EntryHandler) EditPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil {
		slog.Error("failed to get entry", "handler", "EditPage", "id", id, "error", err)
		http.Error(w, "Failed to get entry", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	entryView := buildEntryView(entry, getDuplicateCount(ctx, h.entryRepo, entry), h.summaryEnabled)
	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"))
	entryView.EditURL = entryEditURL(entry.ID.String(), returnTo)
	// Tag suggestions are a convenience; the form still works without them.
	tags, err := h.entryRepo.ListTags(ctx)
	if err != nil {
		slog.Warn("failed to list tags", "handler", "EditPage", "id", id, "error", err)
	}
	if err := pages.EditPage(entryView, returnTo, tags).Render(ctx, w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}
}

// Update updates an entry. Form fields the request omits keep their stored
// values; a field sent empty is cleared. The edit form discards the response
// body (hx-swap="none"), so only the toast trigger is sent.
func (h *EntryHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	input := &model.UpdateEntryInput{
		TimeSpentSeconds: formOptional(r, "time_spent", parseTimeSpentMinutes),
		Quantity:         formOptional(r, "quantity", parseQuantity),
		Notes:            formOptional(r, "notes", parseOptionalString),
		Title:            formOptional(r, "title", parseOptionalString),
		Description:      formOptional(r, "description", parseOptionalString),
		SummaryText:      formOptional(r, "summary", parseOptionalString),
		SourceType:       parseSourceType(r.FormValue("source_type")),
	}
	if _, sent := r.Form["tag"]; sent {
		tag, err := parseTag(r.Form.Get("tag"))
		if err != nil {
			h.htmxError(w, err.Error())
			return
		}
		input.Tag = model.Some(tag)
	}

	entry, err := h.entryRepo.Update(ctx, id, input)
	if err != nil {
		slog.Error("failed to update entry", "handler", "Update", "id", id, "error", err)
		http.Error(w, "Failed to update entry", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	slog.Info("entry updated", "entry_id", entry.ID)
	h.htmxToast(w, "Entry updated", &entry.ID, "")
	w.WriteHeader(http.StatusOK)
}

// Delete removes an entry
func (h *EntryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil {
		slog.Error("failed to get entry", "handler", "Delete", "id", id, "error", err)
		http.Error(w, "Failed to get entry", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	normalizedURL := entry.NormalizedURL
	if normalizedURL == "" {
		if normalized, err := urlutil.NormalizeURL(entry.SourceURL); err == nil {
			normalizedURL = normalized
		}
	}

	if err := h.entryRepo.Delete(ctx, id); err != nil {
		slog.Error("failed to delete entry", "handler", "Delete", "id", id, "error", err)
		http.Error(w, "Failed to delete entry", http.StatusInternalServerError)
		return
	}
	slog.Info("entry deleted", "entry_id", id)

	// Get updated entry count
	count, err := h.entryRepo.Count(ctx)
	if err != nil {
		count = 0
	}

	if redirectURL := htmxCurrentPath(r); redirectURL != "" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	h.htmxToast(w, "Entry deleted", &id, "")

	// Render OOB swap for empty state (show if no entries left)
	if err := partials.EmptyState(count == 0, true).Render(ctx, w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}

	h.renderDuplicateRows(w, r, normalizedURL, id)
}

// RefreshEnrichment resets enrichment status to pending
func (h *EntryHandler) RefreshEnrichment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	if err := h.entryRepo.ResetEnrichment(ctx, id); err != nil {
		slog.Error("failed to reset enrichment", "handler", "RefreshEnrichment", "id", id, "error", err)
		http.Error(w, "Failed to reset enrichment", http.StatusInternalServerError)
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil || entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	slog.Info("entry enrichment queued", "entry_id", id)
	h.htmxToast(w, "Enrichment queued", &id, "")
	h.renderRow(w, r, entry)
	h.renderFailedBanner(w, r)
}

// RefreshSummary resets summary status to pending. Its only caller, the edit
// page, discards the response body (hx-swap="none"), so only the toast
// trigger is sent.
func (h *EntryHandler) RefreshSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	if err := h.entryRepo.ResetSummary(ctx, id); err != nil {
		slog.Error("failed to reset summary", "handler", "RefreshSummary", "id", id, "error", err)
		http.Error(w, "Failed to reset summary", http.StatusInternalServerError)
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil || entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	slog.Info("entry summary queued", "entry_id", id)
	h.htmxToast(w, "Summary queued", &id, "")
	w.WriteHeader(http.StatusOK)
}

// RetryFailed requeues failed enrichments and summaries in bulk, up to
// repository.MaxBulkRetry of each per request. The dashboard reloads afterward
// so the banner count and rows reflect the new state.
func (h *EntryHandler) RetryFailed(w http.ResponseWriter, r *http.Request) {
	requeued, err := h.entryRepo.RetryFailedJobs(r.Context(), h.summaryEnabled)
	if err != nil {
		slog.Error("failed to retry failed jobs", "handler", "RetryFailed", "error", err)
		http.Error(w, "Failed to retry failed entries", http.StatusInternalServerError)
		return
	}
	slog.Info("failed jobs requeued", "enrichment", requeued.Enrichment, "summary", requeued.Summary)

	if requeued.Enrichment+requeued.Summary == 0 {
		h.htmxToast(w, "No failed entries to retry", nil, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	redirectURL := dashboardReturnTo(r)
	if redirectURL == "" {
		redirectURL = "/"
	}
	w.Header().Set("HX-Redirect", redirectURL)
	w.WriteHeader(http.StatusOK)
}

// Status returns the status partial for an entry (for polling)
func (h *EntryHandler) Status(w http.ResponseWriter, r *http.Request) {
	id, ok := parseEntryID(w, r)
	if !ok {
		return
	}

	entry, err := h.entryRepo.GetByID(r.Context(), id)
	if err != nil || entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	// Enrichment or an edit elsewhere can move an entry out of the dashboard's
	// filters; an empty body removes its row.
	if !h.matchesDashboardFilter(r, id) {
		w.WriteHeader(http.StatusOK)
		h.renderFailedBanner(w, r)
		return
	}

	h.renderRow(w, r, entry)
	h.renderFailedBanner(w, r)
}

// matchesDashboardFilter reports whether entry id still matches the filters of
// the dashboard that sent the request. It reports true when the request is not
// from a filtered dashboard or the check fails, so the row keeps updating.
func (h *EntryHandler) matchesDashboardFilter(r *http.Request, id uuid.UUID) bool {
	parsed, err := url.Parse(dashboardReturnTo(r))
	if err != nil {
		return true
	}
	filter := parseDashboardFilter(parsed.Query())
	if !filter.Active() {
		return true
	}
	opts := dashboardListOptions(filter)
	opts.ID = &id
	count, err := h.entryRepo.CountFiltered(r.Context(), opts)
	if err != nil {
		slog.Error("failed to check dashboard filter", "handler", "Status", "id", id, "error", err)
		return true
	}
	return count > 0
}

// parseEntryID reads the {id} route parameter. When it is not a UUID it writes
// a 400 response and returns false.
func parseEntryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
