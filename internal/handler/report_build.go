package handler

import (
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/ui/partials"
)

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

// uniqueTags counts the named tags; the untagged row has an empty Tag.
func uniqueTags(tagReport []partials.TagReport) int {
	n := 0
	for _, tag := range tagReport {
		if tag.Tag != "" {
			n++
		}
	}
	return n
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

func minutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}
