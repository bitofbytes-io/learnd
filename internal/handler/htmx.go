package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/drywaters/learnd/internal/ui"
	"github.com/google/uuid"
)

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
	if _, err := fmt.Fprintf(w, `<p class="text-sm" style="color: var(--color-error);">%s</p>`, html.EscapeString(msg)); err != nil {
		slog.Warn("response write stopped", "error", err)
	}
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
