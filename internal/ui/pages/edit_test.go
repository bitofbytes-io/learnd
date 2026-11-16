package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/google/uuid"
)

func TestEditPage(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }

	full := ui.EntryView{
		Entry: model.Entry{
			ID:               id,
			CreatedAt:        time.Date(2025, time.March, 4, 10, 0, 0, 0, time.UTC),
			SourceURL:        "https://example.test/articles/go",
			Domain:           str("example.test"),
			Tag:              str("go-lang"),
			TimeSpentSeconds: num(1800),
			Quantity:         num(12),
			Notes:            str("read twice"),
			Title:            str("Go Tips"),
			Description:      str("A short intro"),
			SummaryText:      str("Summary body"),
			SourceType:       model.SourceTypePodcast,
		},
		SourceHref:    "https://example.test/articles/go",
		HasSourceHref: true,
	}
	bare := ui.EntryView{Entry: model.Entry{
		ID:         id,
		SourceURL:  "not a link",
		SourceType: model.SourceTypeOther,
	}}
	long := ui.EntryView{Entry: model.Entry{
		ID:         id,
		SourceURL:  "https://example.test/" + strings.Repeat("a", 60),
		SourceType: model.SourceTypeArticle,
	}}

	tests := []struct {
		name     string
		entry    ui.EntryView
		returnTo string
		tags     []string
		want     []string
		absent   []string
	}{
		{
			name:     "fully populated entry",
			entry:    full,
			returnTo: "/?page=2",
			tags:     []string{"ai", "go-lang"},
			want: []string{
				"<title>Edit Entry - learnd</title>",
				`hx-put="/api/entries/` + id.String() + `"`,
				`data-return-to="/?page=2"`,
				`hx-post="/api/entries/` + id.String() + `/refresh-enrichment"`,
				`hx-post="/api/entries/` + id.String() + `/refresh-summary"`,
				`<a href="https://example.test/articles/go" target="_blank" rel="noopener noreferrer"`,
				"Mar 4, 2025",
				"example.test</span>",
				`id="tag" name="tag" value="go-lang"`,
				`list="tag-suggestions"`,
				`<option value="ai"></option>`,
				`id="time_spent" name="time_spent" value="30"`,
				`id="quantity" name="quantity" value="12"`,
				`>read twice</textarea>`,
				`id="title" name="title" value="Go Tips"`,
				`id="description" name="description" value="A short intro"`,
				`>Summary body</textarea>`,
				`<option value="podcast" selected>Podcast</option>`,
				`<a href="/?page=2" hx-boost="false"`,
				"Save Changes",
				"Re-fetch",
				"Re-summarize",
			},
			absent: []string{`<option value="youtube" selected`, `<option value="other" selected`},
		},
		{
			name:     "entry without optional fields",
			entry:    bare,
			returnTo: "/",
			want: []string{
				`data-return-to="/"`,
				`id="tag" name="tag" value=""`,
				`id="time_spent" name="time_spent" value=""`,
				`id="quantity" name="quantity" value=""`,
				`id="title" name="title" value=""`,
				`<option value="other" selected>Other</option>`,
				"not a link</span>",
				`<datalist id="tag-suggestions"></datalist>`,
			},
			absent: []string{"target=\"_blank\"", ">Domain<"},
		},
		{
			name:     "long source URL is truncated",
			entry:    long,
			returnTo: "/",
			want:     []string{"https://example.test/" + strings.Repeat("a", 26) + "...", `<option value="article" selected>Article</option>`},
			absent:   []string{strings.Repeat("a", 40)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRendered(t, renderPage(t, EditPage(tt.entry, tt.returnTo, tt.tags)), tt.want, tt.absent)
		})
	}
}
