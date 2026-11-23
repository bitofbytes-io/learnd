package handler

import (
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
)

const (
	csvPageSize = 1000
	// csvPageWriteTimeout replaces the server's WriteTimeout for each page, so
	// a long export is not cut off while a stalled client still times out.
	csvPageWriteTimeout = 30 * time.Second
)

// ExportCSV exports entries as CSV
func (h *ReportHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().In(h.loc)

	rng, err := parseReportRange(r.URL.Query(), now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rc := http.NewResponseController(w)
	extendDeadline := func() {
		if err := rc.SetWriteDeadline(time.Now().Add(csvPageWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			slog.Warn("failed to extend CSV write deadline", "handler", "ExportCSV", "error", err)
		}
	}
	extendDeadline()

	// Fetch first page before writing headers to allow clean error response
	entries, err := h.entryRepo.ListCreatedBetween(ctx, rng.Start, rng.End, nil, csvPageSize)
	if err != nil {
		slog.Error("failed to list entries", "handler", "ExportCSV", "error", err)
		http.Error(w, "Failed to get entries", http.StatusInternalServerError)
		return
	}
	slog.Info("csv export started", "start", rng.StartDate, "end", rng.EndDate, "first_page_count", len(entries))

	// Now safe to write headers and begin streaming
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=learnd-export-%s.csv", now.Format(reportDateLayout)))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	if err := writer.Write([]string{
		"Date", "URL", "Title", "Type", "Tags", "Time (min)", "Quantity", "Notes", "Summary",
	}); err != nil {
		slog.Warn("csv export stopped", "handler", "ExportCSV", "error", err)
		return
	}

	for {
		// Process current page
		for _, entry := range entries {
			title := ""
			if entry.Title != nil {
				title = *entry.Title
			}

			tags := ""
			if entry.Tag != nil {
				tags = *entry.Tag
			}

			timeSpent := ""
			if trackedSeconds := reportTrackedSeconds(entry); trackedSeconds > 0 {
				timeSpent = fmt.Sprintf("%d", minutesFromSeconds(trackedSeconds))
			}

			quantity := ""
			if entry.Quantity != nil {
				quantity = fmt.Sprintf("%d", *entry.Quantity)
			}

			notes := ""
			if entry.Notes != nil {
				notes = *entry.Notes
			}

			summary := ""
			if entry.SummaryText != nil {
				summary = *entry.SummaryText
			}

			if err := writer.Write([]string{
				sanitizeCSVField(entry.CreatedAt.In(h.loc).Format(reportDateLayout)),
				sanitizeCSVField(entry.SourceURL),
				sanitizeCSVField(title),
				sanitizeCSVField(string(entry.SourceType)),
				sanitizeCSVField(tags),
				sanitizeCSVField(timeSpent),
				sanitizeCSVField(quantity),
				sanitizeCSVField(notes),
				sanitizeCSVField(summary),
			}); err != nil {
				slog.Warn("csv export stopped", "handler", "ExportCSV", "error", err)
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			slog.Warn("csv export stopped", "handler", "ExportCSV", "error", err)
			return
		}

		// Check if this was the last page
		if len(entries) < csvPageSize {
			break
		}

		// Fetch the page after the last entry written
		extendDeadline()
		last := entries[len(entries)-1]
		entries, err = h.entryRepo.ListCreatedBetween(ctx, rng.Start, rng.End, &last, csvPageSize)
		if err != nil {
			// Headers already sent, can only log and stop
			slog.Error("failed to list entries", "handler", "ExportCSV", "after", last.ID, "error", err)
			return
		}
	}
}

func reportTrackedSeconds(entry model.Entry) int {
	if entry.TimeSpentSeconds != nil && *entry.TimeSpentSeconds > 0 {
		return *entry.TimeSpentSeconds
	}
	if entry.RuntimeSeconds != nil && *entry.RuntimeSeconds > 0 {
		return *entry.RuntimeSeconds
	}
	return 0
}

func sanitizeCSVField(value string) string {
	if value == "" {
		return value
	}

	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}

	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}
