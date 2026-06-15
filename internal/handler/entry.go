package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/ui/partials"
	"github.com/drywaters/learnd/internal/urlutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// EntryHandler handles entry CRUD operations
type EntryHandler struct {
	entryRepo EntryRepo
}

// NewEntryHandler creates a new EntryHandler
func NewEntryHandler(entryRepo EntryRepo) *EntryHandler {
	return &EntryHandler{
		entryRepo: entryRepo,
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

			partials.DuplicateWarning(true, title, existing.CreatedAt).Render(ctx, w)
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

	// Render entry row
	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	partials.EntryRow(entryView).Render(ctx, w)

	if duplicateCount > 1 {
		duplicates, err := h.entryRepo.ListByNormalizedURL(ctx, entry.NormalizedURL)
		if err == nil {
			for _, duplicate := range duplicates {
				if duplicate.ID == entry.ID {
					continue
				}
				duplicateView := buildEntryView(&duplicate, duplicateCount)
				duplicateView.SwapOOB = true
				partials.EntryRow(duplicateView).Render(ctx, w)
			}
		}
	}

	// Render OOB swap to remove empty state
	partials.EmptyState(false, true).Render(ctx, w)

	// Clear duplicate warning
	partials.DuplicateWarning(false, "", time.Time{}).Render(ctx, w)

	// Clear form error
	fmt.Fprint(w, `<div id="form-error" hx-swap-oob="true"></div>`)
}

// List returns entries as JSON
func (h *EntryHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	entries, err := h.entryRepo.List(ctx, repository.ListOptions{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		slog.Error("failed to list entries", "handler", "List", "error", err)
		http.Error(w, "Failed to list entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		slog.Error("failed to encode entries response", "handler", "List", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Get returns a single entry
func (h *EntryHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil {
		slog.Error("failed to get entry", "handler", "Get", "id", id, "error", err)
		http.Error(w, "Failed to get entry", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entry); err != nil {
		slog.Error("failed to encode entry response", "handler", "Get", "id", id, "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// EditPage renders the edit form for an entry
func (h *EntryHandler) EditPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
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

	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"))
	entryView.EditURL = entryEditURL(entry.ID.String(), returnTo)
	pages.EditPage(entryView, returnTo).Render(ctx, w)
}

// Update updates an entry
func (h *EntryHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Parse user fields
	tag, err := parseTag(r.FormValue("tag"))
	if err != nil {
		h.htmxError(w, err.Error())
		return
	}
	timeSpent := parseTimeSpentMinutes(r.FormValue("time_spent"))
	quantity := parseQuantity(r.FormValue("quantity"))
	notes := parseOptionalString(r.FormValue("notes"))

	// Parse content fields
	title := parseOptionalString(r.FormValue("title"))
	description := parseOptionalString(r.FormValue("description"))
	summary := parseOptionalString(r.FormValue("summary"))
	sourceType := parseSourceType(r.FormValue("source_type"))

	input := &model.UpdateEntryInput{
		Tag:              tag,
		TimeSpentSeconds: timeSpent,
		Quantity:         quantity,
		Notes:            notes,
		Title:            title,
		Description:      description,
		SummaryText:      summary,
		SourceType:       sourceType,
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

	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	stampDashboardEntryViewFromRequest(r, &entryView)
	partials.EntryRow(entryView).Render(ctx, w)
}

// Delete removes an entry
func (h *EntryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
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
	partials.EmptyState(count == 0, true).Render(ctx, w)

	if normalizedURL != "" {
		duplicates, err := h.entryRepo.ListByNormalizedURL(ctx, normalizedURL)
		if err == nil && len(duplicates) > 0 {
			duplicateCount := len(duplicates)
			for _, duplicate := range duplicates {
				duplicateView := buildEntryView(&duplicate, duplicateCount)
				duplicateView.SwapOOB = true
				partials.EntryRow(duplicateView).Render(ctx, w)
			}
		}
	}
}

// RefreshEnrichment resets enrichment status to pending
func (h *EntryHandler) RefreshEnrichment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
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

	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	stampDashboardEntryViewFromRequest(r, &entryView)
	partials.EntryRow(entryView).Render(ctx, w)
}

// RefreshSummary resets summary status to pending
func (h *EntryHandler) RefreshSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
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

	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	stampDashboardEntryViewFromRequest(r, &entryView)
	partials.EntryRow(entryView).Render(ctx, w)
}

// Status returns the status partial for an entry (for polling)
func (h *EntryHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, id)
	if err != nil || entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	duplicateCount := getDuplicateCount(ctx, h.entryRepo, entry)
	entryView := buildEntryView(entry, duplicateCount)
	stampDashboardEntryViewFromRequest(r, &entryView)
	partials.EntryRow(entryView).Render(ctx, w)
}

func captureRedirectAfterCreate(r *http.Request) string {
	if current := htmxCurrentPath(r); current != "" {
		if current == "/" || strings.HasPrefix(current, "/?") {
			return "/"
		}
	}
	return ""
}

func entryEditURL(id, returnTo string) string {
	path := fmt.Sprintf("/entries/%s/edit", id)
	if returnTo == "" || returnTo == "/" {
		return path
	}
	return path + "?return_to=" + url.QueryEscape(returnTo)
}

func sanitizeReturnTo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/"
	}

	path := parsed.EscapedPath()
	if path == "" {
		path = parsed.Path
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "/"
	}
	if parsed.RawQuery != "" {
		return path + "?" + parsed.RawQuery
	}
	return path
}

func dashboardReturnTo(r *http.Request) string {
	current := htmxCurrentPath(r)
	if current == "" {
		return ""
	}
	current = sanitizeReturnTo(current)
	if current == "/" || strings.HasPrefix(current, "/?") {
		return current
	}
	return ""
}

func stampDashboardEntryViewFromRequest(r *http.Request, entryView *ui.EntryView) {
	returnTo := dashboardReturnTo(r)
	if returnTo == "" {
		return
	}
	stampDashboardEditURL(entryView, returnTo)
}

func htmxCurrentPath(r *http.Request) string {
	current := strings.TrimSpace(r.Header.Get("HX-Current-URL"))
	if current == "" {
		return ""
	}
	parsed, err := url.Parse(current)
	if err != nil {
		return ""
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = parsed.Path
	}
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		return path + "?" + parsed.RawQuery
	}
	return path
}

func (h *EntryHandler) htmxError(w http.ResponseWriter, msg string) {
	w.Header().Set("HX-Retarget", "#form-error")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.WriteHeader(http.StatusUnprocessableEntity)
	fmt.Fprintf(w, `<p class="text-sm" style="color: var(--color-error);">%s</p>`, html.EscapeString(msg))
}

func (h *EntryHandler) htmxToast(w http.ResponseWriter, msg string, entryID *uuid.UUID, toastType string) {
	showToast := map[string]string{
		"message": msg,
	}
	if entryID != nil {
		showToast["id"] = entryID.String()
	}
	if toastType != "" {
		showToast["type"] = toastType
	}

	payload := map[string]map[string]string{
		"showToast": showToast,
	}
	if data, err := json.Marshal(payload); err == nil {
		w.Header().Set("HX-Trigger", string(data))
	}
}

var tagRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// parseTag trims whitespace, lowercases the input, and validates it as a single tag.
// Returns nil if empty, or an error if the tag contains invalid characters.
func parseTag(tagStr string) (*string, error) {
	tag := strings.ToLower(strings.TrimSpace(tagStr))
	if tag == "" {
		return nil, nil
	}
	if !tagRegex.MatchString(tag) {
		return nil, fmt.Errorf("Invalid tag: only lowercase letters, numbers, and hyphens are allowed")
	}
	return &tag, nil
}

// parseTimeSpentMinutes parses a time spent value in minutes and returns seconds.
// Returns nil if the input is empty, not a valid integer, or not positive.
func parseTimeSpentMinutes(ts string) *int {
	if ts == "" {
		return nil
	}
	if v, err := strconv.Atoi(ts); err == nil && v > 0 {
		seconds := v * 60
		return &seconds
	}
	return nil
}

// parseQuantity parses a quantity value and returns a pointer to the integer.
// Returns nil if the input is empty, not a valid integer, or not positive.
func parseQuantity(q string) *int {
	if q == "" {
		return nil
	}
	if v, err := strconv.Atoi(q); err == nil && v > 0 {
		return &v
	}
	return nil
}

// parseOptionalString trims a string and returns a pointer if non-empty.
// Returns nil if the trimmed result is empty.
func parseOptionalString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// parseSourceType parses a source type string and returns a pointer.
// Returns nil if the input is empty or not a valid source type.
func parseSourceType(s string) *model.SourceType {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil
	}
	var st model.SourceType
	switch s {
	case "youtube":
		st = model.SourceTypeYouTube
	case "podcast":
		st = model.SourceTypePodcast
	case "article":
		st = model.SourceTypeArticle
	case "doc":
		st = model.SourceTypeDoc
	case "other":
		st = model.SourceTypeOther
	default:
		return nil
	}
	return &st
}
