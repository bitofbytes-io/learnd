package handler

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/ui/partials"
)

// ReportHandler handles reporting
type ReportHandler struct {
	entryRepo *repository.EntryRepository
}

// NewReportHandler creates a new ReportHandler
func NewReportHandler(entryRepo *repository.EntryRepository) *ReportHandler {
	return &ReportHandler{
		entryRepo: entryRepo,
	}
}

// ReportsPage renders the reports page
func (h *ReportHandler) ReportsPage(w http.ResponseWriter, r *http.Request) {
	startDate, endDate := defaultReportDateStrings(time.Now())
	pages.ReportsPage(startDate, endDate).Render(r.Context(), w)
}

func defaultReportDateRange(now time.Time) (time.Time, time.Time) {
	return now.AddDate(0, 0, -30), now
}

func defaultReportDateStrings(now time.Time) (string, string) {
	start, end := defaultReportDateRange(now)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// GetReport generates a report for the specified date range
func (h *ReportHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	defaultStart, defaultEnd := defaultReportDateRange(time.Now())

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse("2006-01-02", startStr)
		if err != nil {
			http.Error(w, "Invalid start date", http.StatusBadRequest)
			return
		}
	} else {
		start = defaultStart
	}

	if endStr != "" {
		end, err = time.Parse("2006-01-02", endStr)
		if err != nil {
			http.Error(w, "Invalid end date", http.StatusBadRequest)
			return
		}
		// Include the full end day
		end = end.Add(24*time.Hour - time.Second)
	} else {
		end = defaultEnd
	}

	// Get totals from database
	totals, err := h.entryRepo.GetReportTotals(ctx, start, end)
	if err != nil {
		slog.Error("failed to get report totals", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Get aggregations by tag from database
	tagAggs, err := h.entryRepo.AggregateByTag(ctx, start, end)
	if err != nil {
		slog.Error("failed to aggregate by tag", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Get aggregations by type from database
	typeAggs, err := h.entryRepo.AggregateByType(ctx, start, end)
	if err != nil {
		slog.Error("failed to aggregate by type", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Build report data
	var tagReport []partials.TagReport
	totalTagEntries := 0
	totalTagTime := 0
	for _, agg := range tagAggs {
		minutes := minutesFromSeconds(agg.TimeSeconds)
		totalTagEntries += agg.Count
		totalTagTime += minutes
		tagReport = append(tagReport, partials.TagReport{
			Tag:   agg.Tag,
			Count: agg.Count,
			Time:  minutes,
		})
	}

	var typeReport []partials.TypeReport
	totalTypeEntries := 0
	totalTypeTime := 0
	for _, agg := range typeAggs {
		minutes := minutesFromSeconds(agg.TimeSeconds)
		totalTypeEntries += agg.Count
		totalTypeTime += minutes
		typeReport = append(typeReport, partials.TypeReport{
			Type:  agg.Type,
			Count: agg.Count,
			Time:  minutes,
		})
	}

	data := partials.ReportData{
		Start:            start.Format("2006-01-02"),
		End:              end.Format("2006-01-02"),
		TotalEntries:     totals.TotalEntries,
		TotalTime:        minutesFromSeconds(totals.TotalTimeSeconds),
		TotalTagEntries:  totalTagEntries,
		TotalTagTime:     totalTagTime,
		TotalTypeEntries: totalTypeEntries,
		TotalTypeTime:    totalTypeTime,
		ByTag:            tagReport,
		ByType:           typeReport,
	}

	partials.ReportResults(data).Render(ctx, w)
}

// ExportCSV exports entries as CSV
func (h *ReportHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	defaultStart, defaultEnd := defaultReportDateRange(time.Now())

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse("2006-01-02", startStr)
		if err != nil {
			http.Error(w, "Invalid start date", http.StatusBadRequest)
			return
		}
	} else {
		start = defaultStart
	}

	if endStr != "" {
		end, err = time.Parse("2006-01-02", endStr)
		if err != nil {
			http.Error(w, "Invalid end date", http.StatusBadRequest)
			return
		}
		end = end.Add(24*time.Hour - time.Second)
	} else {
		end = defaultEnd
	}

	const pageSize = 1000

	opts := repository.ListOptions{
		Limit:  pageSize,
		Offset: 0,
		Start:  &start,
		End:    &end,
	}

	// Fetch first page before writing headers to allow clean error response
	entries, err := h.entryRepo.List(ctx, opts)
	if err != nil {
		slog.Error("failed to list entries", "handler", "ExportCSV", "offset", opts.Offset, "error", err)
		http.Error(w, "Failed to get entries", http.StatusInternalServerError)
		return
	}

	// Now safe to write headers and begin streaming
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=learnd-export-%s.csv", time.Now().Format("2006-01-02")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	writer.Write([]string{
		"Date", "URL", "Title", "Type", "Tags", "Time (min)", "Quantity", "Notes", "Summary",
	})

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

			writer.Write([]string{
				sanitizeCSVField(entry.CreatedAt.Format("2006-01-02")),
				sanitizeCSVField(entry.SourceURL),
				sanitizeCSVField(title),
				sanitizeCSVField(string(entry.SourceType)),
				sanitizeCSVField(tags),
				sanitizeCSVField(timeSpent),
				sanitizeCSVField(quantity),
				sanitizeCSVField(notes),
				sanitizeCSVField(summary),
			})
		}

		// Check if this was the last page
		if len(entries) < pageSize {
			break
		}

		// Fetch next page
		opts.Offset += len(entries)
		entries, err = h.entryRepo.List(ctx, opts)
		if err != nil {
			// Headers already sent, can only log and stop
			slog.Error("failed to list entries", "handler", "ExportCSV", "offset", opts.Offset, "error", err)
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

func minutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
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
