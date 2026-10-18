package partials

import (
	"context"
	"fmt"
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
}
