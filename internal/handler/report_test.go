package handler

import (
	"testing"

	"github.com/drywaters/learnd/internal/repository"
)

func TestReportTypeDisplay(t *testing.T) {
	tests := []struct {
		name        string
		rawType     string
		wantLabel   string
		wantBadge   string
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
}
