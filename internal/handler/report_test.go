package handler

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui/partials"
)

func TestReportTypeDisplay(t *testing.T) {
	tests := []struct {
		name      string
		rawType   string
		wantLabel string
		wantBadge string
	}{
		{name: "known type", rawType: "podcast", wantLabel: "podcast", wantBadge: "podcast"},
		{name: "blank type", rawType: "", wantLabel: "No Type", wantBadge: "other"},
		{name: "whitespace type", rawType: "   ", wantLabel: "No Type", wantBadge: "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLabel, gotBadge := reportTypeDisplay(tt.rawType)
			if gotLabel != tt.wantLabel || gotBadge != tt.wantBadge {
				t.Fatalf("reportTypeDisplay(%q) = (%q, %q), want (%q, %q)", tt.rawType, gotLabel, gotBadge, tt.wantLabel, tt.wantBadge)
			}
		})
	}
}

func TestBuildTypeReport(t *testing.T) {
	t.Run("formats display rows and totals", func(t *testing.T) {
		aggs := []repository.TypeAggregation{
			{Type: "youtube", Count: 2, TimeSeconds: 120},
			{Type: "", Count: 3, TimeSeconds: 61},
		}

		report, totalEntries, totalTime := buildTypeReport(aggs)
		if totalEntries != 5 {
			t.Fatalf("totalEntries = %d, want 5", totalEntries)
		}
		if totalTime != 4 {
			t.Fatalf("totalTime = %d, want 4", totalTime)
		}
		if len(report) != 2 {
			t.Fatalf("len(report) = %d, want 2", len(report))
		}

		if report[0].Type != "youtube" || report[0].BadgeType != "youtube" || report[0].Count != 2 || report[0].Time != 2 {
			t.Fatalf("first report row = %+v", report[0])
		}
		if report[1].Type != "No Type" || report[1].BadgeType != "other" || report[1].Count != 3 || report[1].Time != 2 {
			t.Fatalf("second report row = %+v", report[1])
		}
	})

	t.Run("rounds total after summing seconds", func(t *testing.T) {
		aggs := []repository.TypeAggregation{
			{Type: "article", Count: 1, TimeSeconds: 61},
			{Type: "video", Count: 1, TimeSeconds: 61},
		}

		_, totalEntries, totalTime := buildTypeReport(aggs)
		if totalEntries != 2 {
			t.Fatalf("totalEntries = %d, want 2", totalEntries)
		}
		if totalTime != 3 {
			t.Fatalf("totalTime = %d, want 3", totalTime)
		}
	})
}

func TestBuildTagReport(t *testing.T) {
	aggs := []repository.TagAggregation{
		{Tag: "go", Count: 2, TimeSeconds: 61},
		{Tag: "db", Count: 1, TimeSeconds: 61},
		{Tag: "untimed", Count: 4, TimeSeconds: 0},
	}

	report, totalEntries, totalTime := buildTagReport(aggs)

	if totalEntries != 7 {
		t.Fatalf("totalEntries = %d, want 7", totalEntries)
	}
	// 122 seconds round up to 3 minutes; rounding each row first would give 4.
	if totalTime != 3 {
		t.Fatalf("totalTime = %d, want 3", totalTime)
	}
	want := []partials.TagReport{{Tag: "go", Count: 2, Time: 2}, {Tag: "db", Count: 1, Time: 2}, {Tag: "untimed", Count: 4, Time: 0}}
	if len(report) != len(want) {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	for i := range want {
		if report[i] != want[i] {
			t.Fatalf("report[%d] = %+v, want %+v", i, report[i], want[i])
		}
	}
}

func TestBuildWeekReportFillsEmptyWeeks(t *testing.T) {
	rng, err := parseReportRange(url.Values{"start": {"2026-09-17"}, "end": {"2026-10-07"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	aggs := []repository.WeekAggregation{
		{WeekStart: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), Count: 2, TimeSeconds: 61},
		{WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), Count: 1, TimeSeconds: 3600},
	}

	got := buildWeekReport(rng, aggs)

	// Sep 17 is a Thursday, so the first week starts on Monday, Sep 14.
	want := []partials.WeekReport{
		{Label: "Sep 14", Count: 2, Time: 2},
		{Label: "Sep 21"},
		{Label: "Sep 28"},
		{Label: "Oct 5", Count: 1, Time: 60},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("buildWeekReport() = %+v, want %+v", got, want)
	}

	rng.First, rng.Last = rng.Last, rng.First
	if got := buildWeekReport(rng, nil); len(got) != 0 {
		t.Fatalf("buildWeekReport() for an inverted range = %+v, want no weeks", got)
	}
}

func TestWeeklyChartRangeKeepsTheLastWeeks(t *testing.T) {
	today := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	short, err := parseReportRange(url.Values{"start": {"2025-01-01"}, "end": {"2026-10-18"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if got, trimmed := weeklyChartRange(short, time.UTC); trimmed || got != short {
		t.Fatalf("weeklyChartRange() trimmed a %d-week range", len(buildWeekReport(short, nil)))
	}

	long, err := parseReportRange(url.Values{"start": {"2000-01-01"}, "end": {"2026-10-18"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	got, trimmed := weeklyChartRange(long, time.UTC)
	if !trimmed {
		t.Fatal("weeklyChartRange() did not trim a 26-year range")
	}
	if weeks := buildWeekReport(got, nil); len(weeks) != maxChartWeeks || weeks[len(weeks)-1].Label != "Oct 12" {
		t.Fatalf("trimmed chart has %d weeks ending %+v, want %d ending Oct 12", len(weeks), weeks[len(weeks)-1], maxChartWeeks)
	}
	if !got.Start.Equal(got.First) || got.StartDate != got.First.Format(reportDateLayout) || got.End != long.End {
		t.Fatalf("trimmed range = %+v", got)
	}
}

func TestMonthToDate(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	start, end := monthToDate(time.Date(2026, 10, 31, 23, 30, 0, 0, loc))

	if want := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Fatalf("start = %s, want %s", start.UTC(), want)
	}
	// The month's last day runs to midnight on November 1, still EDT.
	if want := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("end = %s, want %s", end.UTC(), want)
	}
}

func TestParseReportRange(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 2, 21, 30, 0, 0, loc)
	cairo, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		t.Fatal(err)
	}
	cairoToday := time.Date(2026, 5, 1, 12, 0, 0, 0, cairo)
	havana, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatal(err)
	}
	havanaToday := time.Date(2020, 3, 20, 12, 0, 0, 0, havana)

	tests := []struct {
		name               string
		query              string
		wantStartDate      string
		wantEndDate        string
		wantStart, wantEnd time.Time
		wantErr            string
		today              time.Time
	}{
		{
			name: "defaults to the pre-filled dates, 30 days ago through today", query: "",
			wantStartDate: "2026-10-03", wantEndDate: "2026-11-02",
			wantStart: time.Date(2026, 10, 3, 4, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC),
		},
		{
			name: "end bound is midnight after the last day across a DST change", query: "start=2026-10-31&end=2026-11-01",
			wantStartDate: "2026-10-31", wantEndDate: "2026-11-01",
			wantStart: time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC),
		},
		{
			name: "end bound is midnight when DST skips the end day's midnight", query: "start=2026-04-24&end=2026-04-24", today: cairoToday,
			wantStartDate: "2026-04-24", wantEndDate: "2026-04-24",
			// Cairo skips from 00:00 to 01:00 on April 24, 2026, so that day starts at 01:00 (UTC+3).
			wantStart: time.Date(2026, 4, 23, 22, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 4, 24, 21, 0, 0, 0, time.UTC),
		},
		{
			name: "start bound is the day's first hour when DST skips midnight backward", query: "start=2020-03-08&end=2020-03-08", today: havanaToday,
			wantStartDate: "2020-03-08", wantEndDate: "2020-03-08",
			// Havana skips from 00:00 to 01:00 on March 8, 2020; time.Date normalizes that midnight to 23:00 on March 7.
			wantStart: time.Date(2020, 3, 8, 5, 0, 0, 0, time.UTC), wantEnd: time.Date(2020, 3, 9, 4, 0, 0, 0, time.UTC),
		},
		{name: "invalid start", query: "start=2026-02-30", wantErr: "invalid start date"},
		{name: "invalid end", query: "end=11/02/2026", wantErr: "invalid end date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			day := today
			if !tt.today.IsZero() {
				day = tt.today
			}
			rng, err := parseReportRange(query, day)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parseReportRange() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rng.StartDate != tt.wantStartDate || rng.EndDate != tt.wantEndDate {
				t.Fatalf("dates = %s..%s, want %s..%s", rng.StartDate, rng.EndDate, tt.wantStartDate, tt.wantEndDate)
			}
			if !rng.Start.Equal(tt.wantStart) || !rng.End.Equal(tt.wantEnd) {
				t.Fatalf("range = [%s, %s), want [%s, %s)", rng.Start.UTC(), rng.End.UTC(), tt.wantStart, tt.wantEnd)
			}
		})
	}
}
