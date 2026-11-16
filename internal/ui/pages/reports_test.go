package pages

import "testing"

func TestReportsPage(t *testing.T) {
	tests := []struct {
		name        string
		start, end  string
		bookmarklet string
		want        []string
		absent      []string
	}{
		{
			name:  "date range",
			start: "2025-01-01",
			end:   "2025-01-31",
			want: []string{
				"<title>Reports - learnd</title>",
				`id="report-form"`,
				`hx-get="/api/reports"`,
				`hx-target="#report-results"`,
				`name="start"`,
				`value="2025-01-01"`,
				`name="end"`,
				`value="2025-01-31"`,
				`href="/api/reports/export?start=2025-01-01&amp;end=2025-01-31"`,
				`<div id="report-results">`,
				"Select a date range to generate a report.",
				`action="/logout"`,
				"Generate Report",
			},
			absent: []string{`id="bookmarklet"`},
		},
		{
			name:        "bookmarklet",
			start:       "2025-01-01",
			end:         "2025-01-31",
			bookmarklet: "javascript:void(0)",
			want:        []string{`id="bookmarklet"`, `href="javascript:void(0)"`, "Save to learnd"},
		},
		{
			name:   "empty range",
			want:   []string{`href="/api/reports/export?start=&amp;end="`},
			absent: []string{`id="bookmarklet"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRendered(t, renderPage(t, ReportsPage(tt.start, tt.end, tt.bookmarklet)), tt.want, tt.absent)
		})
	}
}
