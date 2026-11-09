package partials

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func renderWeeklyChart(t *testing.T, data ReportData) string {
	t.Helper()
	var b strings.Builder
	if err := weeklyChart(data).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWeeklyChartScalesBarsToThePeakWeek(t *testing.T) {
	body := renderWeeklyChart(t, ReportData{ByWeek: []WeekReport{
		{Label: "Sep 28", Count: 2, Time: 90},
		{Label: "Oct 5"},
		{Label: "Oct 12", Count: 1, Time: 45},
	}})

	for _, want := range []string{
		`viewBox="0 0 30 100"`,
		`<rect x="2" y="0" width="6" height="100"`,
		`<title>Week of Sep 28: 1 hr 30 min, 2 entries</title>`,
		`<rect x="22" y="50" width="6" height="50"`,
		`<title>Week of Oct 12: 45 min, 1 entry</title>`,
		`>1 hr 30 min</div>`, // the peak labels the top of the scale
		`>Oct 5</span>`,      // empty weeks keep their label but get no bar
		`<div aria-hidden="true">`,
		// Screen readers get every week, since the tooltips need a mouse.
		`<ul class="sr-only"><li>Week of Sep 28: 1 hr 30 min, 2 entries</li><li>Week of Oct 5: 0 min, 0 entries</li><li>Week of Oct 12: 45 min, 1 entry</li></ul>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("weekly chart missing %s: %s", want, body)
		}
	}
	if got := strings.Count(body, "<rect"); got != 2 {
		t.Fatalf("weekly chart has %d bars, want 2", got)
	}
}

func TestWeeklyChartLabelsOnlyTheEndsOfLongRanges(t *testing.T) {
	var weeks []WeekReport
	for i := range maxLabeledWeeks + 1 {
		weeks = append(weeks, WeekReport{Label: fmt.Sprintf("W%d", i), Count: 1, Time: 1})
	}

	body := renderWeeklyChart(t, ReportData{ByWeek: weeks, ByWeekTrimmed: true})

	if !strings.Contains(body, "<span>W0</span>") || !strings.Contains(body, fmt.Sprintf("<span>W%d</span>", maxLabeledWeeks)) {
		t.Fatalf("long chart missing first and last labels: %s", body)
	}
	if strings.Contains(body, ">W1<") {
		t.Fatalf("long chart labels every week: %s", body)
	}
	if want := fmt.Sprintf("Showing the last %d weeks.", maxLabeledWeeks+1); !strings.Contains(body, want) {
		t.Fatalf("trimmed chart missing %q: %s", want, body)
	}
}

func TestWeeklyChartWithoutTrackedTime(t *testing.T) {
	body := renderWeeklyChart(t, ReportData{ByWeek: []WeekReport{{Label: "Oct 12", Count: 3}}, DaysLearned: 4, DaysThisMonth: 18})

	if strings.Contains(body, "<svg") || !strings.Contains(body, "No time tracked in this range.") {
		t.Fatalf("chart without tracked time: %s", body)
	}
	if want := `Learned on <span class="font-medium" style="color: var(--color-ink);">4</span> of 18 days this month`; !strings.Contains(body, want) {
		t.Fatalf("chart missing days learned %q: %s", want, body)
	}

	// A trimmed range may hold older tracked time, so say which weeks were empty.
	trimmed := renderWeeklyChart(t, ReportData{ByWeek: []WeekReport{{Label: "Oct 5"}, {Label: "Oct 12"}}, ByWeekTrimmed: true})
	if !strings.Contains(trimmed, "No time tracked in the last 2 weeks.") || strings.Contains(trimmed, "in this range") {
		t.Fatalf("trimmed chart without tracked time: %s", trimmed)
	}
}

// tagBarStyles renders the report and returns the style of every tag bar's
// fill, in row order.
func tagBarStyles(t *testing.T, data ReportData) []string {
	t.Helper()
	var b strings.Builder
	if err := ReportResults(data).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	var styles []string
	for _, m := range regexp.MustCompile(`class="h-full rounded-full" style="([^"]*)"`).FindAllStringSubmatch(b.String(), -1) {
		styles = append(styles, m[1])
	}
	return styles
}

// barStyle is the style of a tag bar filled to width in the accent color.
func barStyle(width string) string {
	return "width: " + width + "; background: var(--color-accent);"
}

// TestTagBarsScaleToTheLongestTag checks that the longest tag fills its track,
// shorter tags are proportional, and tiny or empty tags stay visible or empty.
func TestTagBarsScaleToTheLongestTag(t *testing.T) {
	got := tagBarStyles(t, ReportData{ByTag: []TagReport{
		{Tag: "go", Count: 2, Time: 200},
		{Tag: "", Count: 1, Time: 50},
		{Tag: "rust", Count: 1, Time: 1},
		{Tag: "ai", Count: 1},
	}})
	want := []string{barStyle("100.0%"), barStyle("25.0%"), barStyle("1.0%"), barStyle("0")}
	if !slices.Equal(got, want) {
		t.Fatalf("tag bar styles = %q, want %q", got, want)
	}
}

// TestTagBarsWithoutTrackedTimeStayEmpty checks that a report with no tracked
// time renders empty accent bars instead of dividing by a zero peak.
func TestTagBarsWithoutTrackedTimeStayEmpty(t *testing.T) {
	got := tagBarStyles(t, ReportData{ByTag: []TagReport{{Tag: "go", Count: 2}, {Tag: "", Count: 1}}})
	if want := []string{barStyle("0"), barStyle("0")}; !slices.Equal(got, want) {
		t.Fatalf("tag bar styles = %q, want %q", got, want)
	}
}

// TestTagBarsTieForTheLongestTag checks that tags sharing the peak all render
// full width.
func TestTagBarsTieForTheLongestTag(t *testing.T) {
	got := tagBarStyles(t, ReportData{ByTag: []TagReport{{Tag: "go", Count: 1, Time: 60}, {Tag: "ai", Count: 1, Time: 60}}})
	if want := []string{barStyle("100.0%"), barStyle("100.0%")}; !slices.Equal(got, want) {
		t.Fatalf("tag bar styles = %q, want %q", got, want)
	}
}

// TestTagBarStyleHandlesDegenerateInput checks the bar calculation directly:
// a zero peak or non-positive minutes must not divide by zero or go negative.
func TestTagBarStyleHandlesDegenerateInput(t *testing.T) {
	for _, tc := range []struct{ minutes, peak int }{{0, 0}, {5, 0}, {0, 5}, {-3, 5}} {
		if got, want := tagBarStyle(tc.minutes, tc.peak), barStyle("0"); got != want {
			t.Errorf("tagBarStyle(%d, %d) = %q, want %q", tc.minutes, tc.peak, got, want)
		}
	}
}

// TestReportResultsWithoutTags checks that an empty report renders no tag bars.
func TestReportResultsWithoutTags(t *testing.T) {
	if got := tagBarStyles(t, ReportData{}); len(got) != 0 {
		t.Fatalf("empty report has tag bars: %q", got)
	}
}
