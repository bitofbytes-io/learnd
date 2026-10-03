package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

type reportSeed struct {
	createdAt      time.Time
	url            string
	title          string
	tag            string
	sourceType     string
	timeSpent      *int
	runtimeSeconds *int
	notes          string
}

func newReportTestHandler(t *testing.T) (*ReportHandler, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return NewReportHandler(repository.NewEntryRepository(pool), newYork(t)), pool
}

func seedReportEntries(t *testing.T, pool *pgxpool.Pool, seeds ...reportSeed) {
	t.Helper()
	for _, s := range seeds {
		sourceType := s.sourceType
		if sourceType == "" {
			sourceType = "article"
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO entries (created_at, source_url, normalized_url, title, tag, source_type,
			                     time_spent_seconds, runtime_seconds, notes)
			VALUES ($1, $2, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, NULLIF($8, ''))`,
			s.createdAt, s.url, s.title, s.tag, sourceType, s.timeSpent, s.runtimeSeconds, s.notes)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func serveReport(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// reportStat returns the number rendered above a summary card label.
func reportStat(t *testing.T, body, label string) string {
	t.Helper()
	re := regexp.MustCompile(`>([^<>]+)</div><div class="text-xs mt-1"[^>]*>` + regexp.QuoteMeta(label) + `</div>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("report body has no %q card: %s", label, body)
	}
	return strings.TrimSpace(m[1])
}

func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func midday(date string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", date+" 16:00")
	if err != nil {
		panic(err)
	}
	return t
}

func TestGetReportAggregatesEntriesInRange(t *testing.T) {
	h, pool := newReportTestHandler(t)
	seedReportEntries(t, pool,
		reportSeed{createdAt: midday("2026-03-09"), url: "https://example.test/before", tag: "outside", timeSpent: intPtr(600)},
		reportSeed{createdAt: midday("2026-03-10"), url: "https://example.test/a", tag: "go", timeSpent: intPtr(61)},
		reportSeed{createdAt: midday("2026-03-10"), url: "https://example.test/b", tag: "go", runtimeSeconds: intPtr(61), sourceType: "youtube"},
		reportSeed{createdAt: midday("2026-03-11"), url: "https://example.test/c", tag: "db", timeSpent: intPtr(120)},
		reportSeed{createdAt: midday("2026-03-11"), url: "https://example.test/d"},
		reportSeed{createdAt: midday("2026-03-12"), url: "https://example.test/after", tag: "outside", timeSpent: intPtr(600)},
	)

	rec := serveReport(h.GetReport, "/api/reports?start=2026-03-10&end=2026-03-11")

	if rec.Code != http.StatusOK {
		t.Fatalf("GetReport() status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := reportStat(t, body, "Total Entries"); got != "4" {
		t.Fatalf("Total Entries = %s, want 4", got)
	}
	// 61 + 61 + 120 seconds, rounded up once after summing.
	if got := reportStat(t, body, "Time Tracked"); got != "5 min" {
		t.Fatalf("Time Tracked = %s, want 5 min", got)
	}
	if got := reportStat(t, body, "Unique Tags"); got != "2" {
		t.Fatalf("Unique Tags = %s, want 2", got)
	}
	if got := reportStat(t, body, "Content Types"); got != "2" {
		t.Fatalf("Content Types = %s, want 2", got)
	}
	for _, want := range []string{`<span class="tag">go</span>`, `<span class="tag">db</span>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GetReport() body missing %q", want)
		}
	}
	if strings.Contains(body, "outside") {
		t.Fatal("GetReport() included entries outside the range")
	}
}

func TestReportEndpointsRejectInvalidDates(t *testing.T) {
	h := NewReportHandler(nil, time.UTC)
	for _, target := range []string{"?start=2026-13-01", "?end=yesterday", "?start=03/10/2026&end=2026-03-11"} {
		for name, fn := range map[string]http.HandlerFunc{"GetReport": h.GetReport, "ExportCSV": h.ExportCSV} {
			rec := serveReport(fn, "/api/reports"+target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s(%s) status = %d, want 400", name, target, rec.Code)
			}
		}
	}
}

func readCSV(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("ExportCSV() status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv" {
		t.Fatalf("Content-Type = %q, want text/csv", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename=learnd-export-") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	return records
}

func TestExportCSVWritesEntriesInRange(t *testing.T) {
	h, pool := newReportTestHandler(t)
	seedReportEntries(t, pool,
		reportSeed{createdAt: midday("2026-03-09"), url: "https://example.test/before"},
		reportSeed{createdAt: midday("2026-03-10"), url: "https://example.test/older", title: "=HYPERLINK(\"x\")",
			tag: "go", runtimeSeconds: intPtr(90), notes: "+1 note"},
		reportSeed{createdAt: midday("2026-03-11"), url: "https://example.test/newer", title: "Plain, with comma",
			tag: "db", sourceType: "podcast", timeSpent: intPtr(120), runtimeSeconds: intPtr(9999)},
		reportSeed{createdAt: midday("2026-03-12"), url: "https://example.test/after"},
	)

	records := readCSV(t, serveReport(h.ExportCSV, "/api/reports/export?start=2026-03-10&end=2026-03-11"))

	want := [][]string{
		{"Date", "URL", "Title", "Type", "Tags", "Time (min)", "Quantity", "Notes", "Summary"},
		{"2026-03-11", "https://example.test/newer", "Plain, with comma", "podcast", "db", "2", "", "", ""},
		{"2026-03-10", "https://example.test/older", "'=HYPERLINK(\"x\")", "article", "go", "2", "", "'+1 note", ""},
	}
	if fmt.Sprint(records) != fmt.Sprint(want) {
		t.Fatalf("CSV = %q\nwant %q", records, want)
	}
}

func TestExportCSVPagesThroughEveryEntry(t *testing.T) {
	h, pool := newReportTestHandler(t)
	const total = 2345
	_, err := pool.Exec(context.Background(), `
		INSERT INTO entries (created_at, source_url, normalized_url)
		SELECT TIMESTAMPTZ '2026-03-10 12:00:00Z' + make_interval(secs => n),
		       'https://example.test/' || n, 'https://example.test/' || n
		FROM generate_series(1, $1) AS n`, total)
	if err != nil {
		t.Fatal(err)
	}

	records := readCSV(t, serveReport(h.ExportCSV, "/api/reports/export?start=2026-03-10&end=2026-03-10"))

	if len(records) != total+1 {
		t.Fatalf("CSV rows = %d, want %d plus header", len(records)-1, total)
	}
	seen := make(map[string]bool, total)
	for _, record := range records[1:] {
		if seen[record[1]] {
			t.Fatalf("URL %s exported twice", record[1])
		}
		seen[record[1]] = true
	}
	if records[1][1] != fmt.Sprintf("https://example.test/%d", total) {
		t.Fatalf("first row = %s, want newest entry first", records[1][1])
	}
}

// LRN-8: report days are calendar days in the configured zone, and the end
// day runs up to, not including, the following midnight.
func TestReportsUseConfiguredTimeZoneDays(t *testing.T) {
	h, pool := newReportTestHandler(t)
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	seedReportEntries(t, pool,
		reportSeed{createdAt: at("2026-03-10T03:30:00Z"), url: "https://example.test/previous-evening"}, // Mar 9, 23:30 EDT
		reportSeed{createdAt: at("2026-03-10T04:30:00Z"), url: "https://example.test/first-day"},        // Mar 10, 00:30 EDT
		reportSeed{createdAt: at("2026-03-12T03:59:59.5Z"), url: "https://example.test/last-moment"},    // Mar 11, 23:59:59.5 EDT
		reportSeed{createdAt: at("2026-03-12T04:00:00Z"), url: "https://example.test/next-midnight"},    // Mar 12, 00:00 EDT
	)
	const query = "?start=2026-03-10&end=2026-03-11"

	rec := serveReport(h.GetReport, "/api/reports"+query)
	if got := reportStat(t, rec.Body.String(), "Total Entries"); got != "2" {
		t.Fatalf("Total Entries = %s, want 2", got)
	}

	records := readCSV(t, serveReport(h.ExportCSV, "/api/reports/export"+query))
	want := [][]string{
		{"Date", "URL", "Title", "Type", "Tags", "Time (min)", "Quantity", "Notes", "Summary"},
		{"2026-03-11", "https://example.test/last-moment", "", "article", "", "", "", "", ""},
		{"2026-03-10", "https://example.test/first-day", "", "article", "", "", "", "", ""},
	}
	if fmt.Sprint(records) != fmt.Sprint(want) {
		t.Fatalf("CSV = %q\nwant %q", records, want)
	}
}

// LRN-9: entries sharing a created_at must not be skipped or repeated at a
// page boundary.
func TestExportCSVPagesThroughTiedTimestamps(t *testing.T) {
	h, pool := newReportTestHandler(t)
	const total = csvPageSize + 500
	_, err := pool.Exec(context.Background(), `
		INSERT INTO entries (created_at, source_url, normalized_url)
		SELECT TIMESTAMPTZ '2026-03-10 16:00:00Z', 'https://example.test/' || n, 'https://example.test/' || n
		FROM generate_series(1, $1) AS n`, total)
	if err != nil {
		t.Fatal(err)
	}

	records := readCSV(t, serveReport(h.ExportCSV, "/api/reports/export?start=2026-03-10&end=2026-03-10"))

	seen := make(map[string]bool, total)
	for _, record := range records[1:] {
		if seen[record[1]] {
			t.Fatalf("URL %s exported twice", record[1])
		}
		seen[record[1]] = true
	}
	if len(seen) != total {
		t.Fatalf("exported %d entries, want %d", len(seen), total)
	}
}
