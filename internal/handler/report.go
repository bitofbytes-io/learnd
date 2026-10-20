package handler

import (
	"cmp"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/middleware"
	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui/pages"
	"github.com/drywaters/learnd/internal/ui/partials"
)

// ReportHandler handles reporting
type ReportHandler struct {
	entryRepo *repository.EntryRepository
	loc       *time.Location
	now       func() time.Time // the clock "this month" is read from; tests replace it
}

// NewReportHandler creates a new ReportHandler. Report dates are calendar
// days in loc.
func NewReportHandler(entryRepo *repository.EntryRepository, loc *time.Location) *ReportHandler {
	return &ReportHandler{
		entryRepo: entryRepo,
		loc:       loc,
		now:       time.Now,
	}
}

// ReportsPage renders the reports page
func (h *ReportHandler) ReportsPage(w http.ResponseWriter, r *http.Request) {
	startDate, endDate := defaultReportDateStrings(time.Now().In(h.loc))
	if err := pages.ReportsPage(startDate, endDate, bookmarkletURL(middleware.RequestOrigin(r))).Render(r.Context(), w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}
}

// bookmarkletURL returns a javascript: URL that opens the capture page at
// origin with the current tab's URL prefilled, or "" without an origin. The
// origin is the one SameOrigin checks logins against, so it is the address
// the browser actually uses, even behind a proxy.
func bookmarkletURL(origin string) string {
	if origin == "" {
		return ""
	}
	target, _ := json.Marshal(origin + "/?url=") // a quoted JS string; strings always marshal
	// Browsers percent-decode a javascript: URL before running it.
	script := strings.ReplaceAll(string(target), "%", "%25") + "+encodeURIComponent(location.href)"
	return "javascript:location=" + script
}

func defaultReportDateStrings(today time.Time) (string, string) {
	return today.AddDate(0, 0, -30).Format(reportDateLayout), today.Format(reportDateLayout)
}

const reportDateLayout = "2006-01-02"

// reportRange is a span of whole calendar days.
type reportRange struct {
	StartDate, EndDate string    // inclusive, as YYYY-MM-DD
	First, Last        time.Time // StartDate and EndDate as UTC midnights
	Start, End         time.Time // End is exclusive: midnight after EndDate
}

// parseReportRange reads the start and end query dates as calendar days in
// today's location. They default to the dates the reports page pre-fills,
// 30 days ago through today. The returned error is safe to show to the user.
func parseReportRange(query url.Values, today time.Time) (reportRange, error) {
	defaultStart, defaultEnd := defaultReportDateStrings(today)
	rng := reportRange{StartDate: cmp.Or(query.Get("start"), defaultStart), EndDate: cmp.Or(query.Get("end"), defaultEnd)}

	// Parse as plain dates (UTC) so the day is exactly what was asked for,
	// then find where that day starts in the report's zone.
	first, err := time.Parse(reportDateLayout, rng.StartDate)
	if err != nil {
		return reportRange{}, errors.New("invalid start date")
	}
	last, err := time.Parse(reportDateLayout, rng.EndDate)
	if err != nil {
		return reportRange{}, errors.New("invalid end date")
	}
	rng.First, rng.Last = first, last
	rng.Start = startOfDay(first, today.Location())
	rng.End = startOfDay(last.AddDate(0, 0, 1), today.Location())
	return rng, nil
}

// maxChartWeeks caps the weekly chart; longer ranges chart their last weeks.
const maxChartWeeks = 104

// weekStart returns the Monday on or before date (a UTC midnight), matching
// PostgreSQL's date_trunc('week', ...).
func weekStart(date time.Time) time.Time {
	return date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
}

// weeklyChartRange returns rng trimmed to its last maxChartWeeks weeks, and
// whether it was trimmed.
func weeklyChartRange(rng reportRange, loc *time.Location) (reportRange, bool) {
	first := weekStart(rng.Last).AddDate(0, 0, -7*(maxChartWeeks-1))
	if !rng.First.Before(first) {
		return rng, false
	}
	rng.First = first
	rng.StartDate = first.Format(reportDateLayout)
	rng.Start = startOfDay(first, loc)
	return rng, true
}

// buildWeekReport returns one row per week from rng's first week through its
// last, including weeks without entries.
func buildWeekReport(rng reportRange, weekAggs []repository.WeekAggregation) []partials.WeekReport {
	byWeek := make(map[string]repository.WeekAggregation, len(weekAggs))
	for _, agg := range weekAggs {
		byWeek[agg.WeekStart.Format(reportDateLayout)] = agg
	}

	var weeks []partials.WeekReport
	if rng.Last.Before(rng.First) {
		return weeks
	}
	for week := weekStart(rng.First); !week.After(rng.Last); week = week.AddDate(0, 0, 7) {
		agg := byWeek[week.Format(reportDateLayout)]
		weeks = append(weeks, partials.WeekReport{
			Label: week.Format("Jan 2"),
			Count: agg.Count,
			Time:  minutesFromSeconds(agg.TimeSeconds),
		})
	}
	return weeks
}

// monthToDate returns the span from the first of today's month through the
// end of today, in today's location.
func monthToDate(today time.Time) (time.Time, time.Time) {
	year, month, day := today.Date()
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	tomorrow := time.Date(year, month, day+1, 0, 0, 0, 0, time.UTC)
	return startOfDay(first, today.Location()), startOfDay(tomorrow, today.Location())
}

// startOfDay returns the first instant in loc whose calendar date is date's
// (a UTC midnight). Where a DST change skips midnight, time.Date normalizes
// to 01:00 or back into the previous day; the latter steps forward an hour.
func startOfDay(date time.Time, loc *time.Location) time.Time {
	year, month, day := date.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, loc)
	for {
		y, m, d := start.Date()
		if !time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Before(date) {
			return start
		}
		start = start.Add(time.Hour)
	}
}

// GetReport generates a report for the specified date range
func (h *ReportHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := h.now().In(h.loc)

	rng, err := parseReportRange(r.URL.Query(), today)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get totals from database
	totals, err := h.entryRepo.GetReportTotals(ctx, rng.Start, rng.End)
	if err != nil {
		slog.Error("failed to get report totals", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Get aggregations by tag from database
	tagAggs, err := h.entryRepo.AggregateByTag(ctx, rng.Start, rng.End)
	if err != nil {
		slog.Error("failed to aggregate by tag", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Get aggregations by type from database
	typeAggs, err := h.entryRepo.AggregateByType(ctx, rng.Start, rng.End)
	if err != nil {
		slog.Error("failed to aggregate by type", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Get weekly time for the chart, in the report's zone
	chartRange, chartTrimmed := weeklyChartRange(rng, h.loc)
	weekAggs, err := h.entryRepo.AggregateByWeek(ctx, chartRange.Start, chartRange.End, h.loc.String())
	if err != nil {
		slog.Error("failed to aggregate by week", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	// Count the days learned so far this month, whatever the range
	monthStart, monthEnd := monthToDate(today)
	daysLearned, err := h.entryRepo.CountActiveDays(ctx, monthStart, monthEnd, h.loc.String())
	if err != nil {
		slog.Error("failed to count active days", "handler", "GetReport", "error", err)
		http.Error(w, "Failed to get report", http.StatusInternalServerError)
		return
	}

	tagReport, totalTagEntries, totalTagTime := buildTagReport(tagAggs)
	typeReport, totalTypeEntries, totalTypeTime := buildTypeReport(typeAggs)

	data := partials.ReportData{
		Start:            rng.StartDate,
		End:              rng.EndDate,
		TotalEntries:     totals.TotalEntries,
		TotalTime:        minutesFromSeconds(totals.TotalTimeSeconds),
		TotalTagEntries:  totalTagEntries,
		TotalTagTime:     totalTagTime,
		TotalTypeEntries: totalTypeEntries,
		TotalTypeTime:    totalTypeTime,
		ByTag:            tagReport,
		ByType:           typeReport,
		ByWeek:           buildWeekReport(chartRange, weekAggs),
		ByWeekTrimmed:    chartTrimmed,
		DaysLearned:      daysLearned,
		DaysThisMonth:    today.Day(),
	}

	slog.Info("report generated", "start", data.Start, "end", data.End, "total_entries", data.TotalEntries)
	if err := partials.ReportResults(data).Render(ctx, w); err != nil {
		slog.Warn("response render stopped", "error", err)
		return
	}
}

// buildTagReport returns the tag rows plus their total entries and minutes.
// Minutes are rounded once, after summing seconds.
func buildTagReport(tagAggs []repository.TagAggregation) ([]partials.TagReport, int, int) {
	tagReport := make([]partials.TagReport, 0, len(tagAggs))
	totalTagEntries := 0
	totalTagSeconds := 0

	for _, agg := range tagAggs {
		totalTagEntries += agg.Count
		totalTagSeconds += agg.TimeSeconds
		tagReport = append(tagReport, partials.TagReport{
			Tag:   agg.Tag,
			Count: agg.Count,
			Time:  minutesFromSeconds(agg.TimeSeconds),
		})
	}

	return tagReport, totalTagEntries, minutesFromSeconds(totalTagSeconds)
}

// buildTypeReport returns the type rows plus their total entries and minutes.
// Minutes are rounded once, after summing seconds.
func buildTypeReport(typeAggs []repository.TypeAggregation) ([]partials.TypeReport, int, int) {
	typeReport := make([]partials.TypeReport, 0, len(typeAggs))
	totalTypeEntries := 0
	totalTypeSeconds := 0

	for _, agg := range typeAggs {
		totalTypeEntries += agg.Count
		totalTypeSeconds += agg.TimeSeconds

		displayType, badgeType := reportTypeDisplay(agg.Type)
		typeReport = append(typeReport, partials.TypeReport{
			Type:      displayType,
			BadgeType: badgeType,
			Count:     agg.Count,
			Time:      minutesFromSeconds(agg.TimeSeconds),
		})
	}

	return typeReport, totalTypeEntries, minutesFromSeconds(totalTypeSeconds)
}

func reportTypeDisplay(rawType string) (displayType string, badgeType string) {
	normalized := strings.TrimSpace(rawType)
	if normalized == "" {
		return "No Type", string(model.SourceTypeOther)
	}
	return normalized, normalized
}

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
