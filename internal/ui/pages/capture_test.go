package pages

import (
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/google/uuid"
)

func captureEntry(url string) ui.EntryView {
	return ui.EntryView{Entry: model.Entry{
		ID:               uuid.New(),
		SourceURL:        url,
		EnrichmentStatus: model.StatusOK,
		SummaryStatus:    model.StatusOK,
	}}
}

func TestCapturePage(t *testing.T) {
	one := ui.PaginationView{CurrentPage: 1, TotalPages: 1}
	entries := []ui.EntryView{captureEntry("https://one.example.test"), captureEntry("https://two.example.test")}

	tests := []struct {
		name         string
		entries      []ui.EntryView
		pagination   ui.PaginationView
		filter       ui.DashboardFilter
		failed       ui.FailedJobsView
		tags         []string
		totalEntries int
		prefillURL   string
		want         []string
		absent       []string
	}{
		{
			name:       "empty journal",
			pagination: one,
			want: []string{
				"<title>Capture - learnd</title>",
				`id="capture-form"`,
				`hx-post="/api/entries"`,
				`hx-target="#entry-list"`,
				`id="url" name="url" value=""`,
				`id="tag" name="tag"`,
				`list="tag-suggestions"`,
				`<datalist id="tag-suggestions"></datalist>`,
				`name="time_spent"`,
				`name="quantity"`,
				`name="notes"`,
				"Save Entry",
				`<div id="form-error"`,
				`<div id="duplicate-warning"`,
				`<form id="dashboard-filters" action="/" method="get" role="search"`,
				`<div id="failed-jobs-banner"></div>`,
				"Recent Entries",
				"Page 1 of 1",
				"No entries yet. Start capturing!",
				`href="/reports"`,
				`action="/logout"`,
			},
			absent: []string{"Retry all failed", "No entries match.", "matching entr"},
		},
		{
			name:         "entries, tags and prefilled URL",
			entries:      entries,
			pagination:   one,
			tags:         []string{"ai", "go-lang"},
			totalEntries: 2,
			prefillURL:   "https://prefill.example.test/page",
			want: []string{
				`value="https://prefill.example.test/page"`,
				`<option value="ai"></option>`,
				`<option value="go-lang"></option>`,
				"https://one.example.test",
				"https://two.example.test",
				"Recent Entries",
			},
			absent: []string{"No entries yet"},
		},
		{
			name: "filters keep their selections",
			filter: ui.DashboardFilter{
				Query:      "rust",
				Tag:        "go-lang",
				SourceType: "article",
				Tags:       []string{"ai", "go-lang"},
			},
			entries:      entries[:1],
			pagination:   one,
			totalEntries: 1,
			want: []string{
				`<input type="hidden" name="status" value="">`,
				`id="dashboard-search" name="q" value="rust"`,
				`<option value="">All tags</option>`,
				`<option value="go-lang" selected>go-lang</option>`,
				`<option value="ai">ai</option>`,
				`<option value="article" selected>Article</option>`,
				`<option value="youtube">YouTube</option>`,
				"1 matching entry",
				"https://one.example.test",
			},
			absent: []string{"Recent Entries", "No entries match."},
		},
		{
			name:         "filters without results",
			filter:       ui.DashboardFilter{Query: "nothing"},
			pagination:   one,
			totalEntries: 0,
			want:         []string{"0 matching entries", "No entries match.", `<a href="/"`, "Clear filters"},
			absent:       []string{"No entries yet"},
		},
		{
			name:       "failed jobs banner",
			pagination: one,
			failed:     ui.FailedJobsView{Entries: 3, Enrichment: 2, Summary: 1},
			want: []string{
				`role="status"`,
				"3 entries failed to process",
				`<a href="/?status=failed"`,
				"Show failed",
				`hx-post="/api/entries/retry-failed"`,
				`hx-confirm="Retry all failed entries?"`,
				"Retry all failed",
			},
			absent: []string{"Show all"},
		},
		{
			name:         "failed filter",
			filter:       ui.DashboardFilter{Failed: true},
			pagination:   one,
			failed:       ui.FailedJobsView{Entries: 1, Summary: 1},
			entries:      entries[:1],
			totalEntries: 1,
			want: []string{
				"1 entry failed to process",
				`<input type="hidden" name="status" value="failed">`,
				"Show all",
				"1 matching entry",
			},
			absent: []string{"Show failed"},
		},
		{
			name:         "middle page links both ways",
			entries:      entries,
			pagination:   ui.PaginationView{CurrentPage: 2, TotalPages: 3, HasPrevious: true, HasNext: true, PreviousURL: "/?page=1", NextURL: "/?page=3"},
			totalEntries: 25,
			want: []string{
				`href="/?page=1" hx-get="/?page=1"`,
				`href="/?page=3" hx-get="/?page=3"`,
				`hx-target="#dashboard-entries-region"`,
				"Page 2 of 3",
			},
			absent: []string{`<span class="text-xs opacity-50">Previous</span>`, `<span class="text-xs opacity-50">Next</span>`},
		},
		{
			name:         "first page disables previous",
			entries:      entries,
			pagination:   ui.PaginationView{CurrentPage: 1, TotalPages: 2, HasNext: true, NextURL: "/?page=2"},
			totalEntries: 12,
			want:         []string{`<span class="text-xs opacity-50">Previous</span>`, `href="/?page=2"`},
			absent:       []string{`<span class="text-xs opacity-50">Next</span>`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderPage(t, CapturePage(tt.entries, tt.pagination, tt.filter, tt.failed, tt.tags, tt.totalEntries, tt.prefillURL))
			assertRendered(t, body, tt.want, tt.absent)
		})
	}
}

func TestFailedJobsBannerSwapsOutOfBand(t *testing.T) {
	failed := ui.FailedJobsView{Entries: 2}

	inline := renderPage(t, FailedJobsBanner(ui.DashboardFilter{}, failed, false))
	assertRendered(t, inline, []string{`<div id="failed-jobs-banner">`, "2 entries failed to process"}, []string{"hx-swap-oob"})

	oob := renderPage(t, FailedJobsBanner(ui.DashboardFilter{}, failed, true))
	assertRendered(t, oob, []string{`id="failed-jobs-banner" hx-swap-oob="outerHTML"`, "2 entries failed to process"}, nil)

	// The wrapper stays so polling can fill it in later, but it holds no card.
	cleared := renderPage(t, FailedJobsBanner(ui.DashboardFilter{}, ui.FailedJobsView{}, true))
	assertRendered(t, cleared, []string{`id="failed-jobs-banner" hx-swap-oob="outerHTML"`}, []string{"role=\"status\"", "Retry all failed"})
	if strings.Contains(cleared, "<button") {
		t.Fatalf("cleared banner holds controls: %s", cleared)
	}
}

func TestCaptureEntriesSectionIsTheSwapTarget(t *testing.T) {
	body := renderPage(t, CaptureEntriesSection(nil, ui.PaginationView{CurrentPage: 1, TotalPages: 1}, ui.DashboardFilter{}, 0))

	assertRendered(t, body, []string{
		`<div id="dashboard-entries-region">`,
		`<div id="entry-list"`,
		"Recent Entries",
		"No entries yet. Start capturing!",
	}, []string{"<html", "capture-form"})
	if got := strings.Count(body, "Page 1 of 1"); got != 2 {
		t.Fatalf("pagination rendered %d times, want above and below the list: %s", got, body)
	}
}
