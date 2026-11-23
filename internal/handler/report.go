package handler

import (
	"cmp"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/middleware"
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
		UniqueTags:       uniqueTags(tagReport),
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
