package repository

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

type searchSeed struct {
	url, title, notes, description, summary, tag string
	sourceType                                   model.SourceType
}

func seedSearchEntries(t *testing.T, pool *pgxpool.Pool, seeds ...searchSeed) {
	t.Helper()
	start := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i, s := range seeds {
		sourceType := s.sourceType
		if sourceType == "" {
			sourceType = model.SourceTypeArticle
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO entries (created_at, source_url, normalized_url, title, notes, description, summary_text, tag, source_type)
			VALUES ($1, $2, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8)`,
			start.Add(time.Duration(i)*time.Minute), s.url, s.title, s.notes, s.description, s.summary, s.tag, sourceType)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func listURLs(t *testing.T, repo *EntryRepository, opts ListOptions) []string {
	t.Helper()
	entries, err := repo.List(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	count, err := repo.CountFiltered(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Offset == 0 && count != len(entries) {
		t.Fatalf("CountFiltered() = %d, List() returned %d", count, len(entries))
	}
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.SourceURL)
	}
	return urls
}

func TestListFiltersBySearchTagAndType(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	seedSearchEntries(t, pool,
		searchSeed{url: "https://example.test/title", title: "Indexing Postgres databases", tag: "db"},
		searchSeed{url: "https://example.test/notes", notes: "Caleb asked about database indexes", tag: "db", sourceType: model.SourceTypeYouTube},
		searchSeed{url: "https://example.test/description", description: "A gentle tour of Rust ownership", tag: "rust"},
		searchSeed{url: "https://example.test/summary", summary: "Explains how the Go scheduler works", sourceType: model.SourceTypePodcast},
		searchSeed{url: "https://example.test/untagged", title: "Weekend bread baking"},
	)
	ptr := func(s string) *string { return &s }
	youtube := model.SourceTypeYouTube

	tests := []struct {
		name string
		opts ListOptions
		want []string
	}{
		{name: "no filters newest first", opts: ListOptions{}, want: []string{
			"https://example.test/untagged", "https://example.test/summary", "https://example.test/description",
			"https://example.test/notes", "https://example.test/title"}},
		{name: "stemmed search over title and notes", opts: ListOptions{Query: ptr("database")}, want: []string{
			"https://example.test/notes", "https://example.test/title"}},
		{name: "search description", opts: ListOptions{Query: ptr("ownership")}, want: []string{"https://example.test/description"}},
		{name: "search summary", opts: ListOptions{Query: ptr("scheduler")}, want: []string{"https://example.test/summary"}},
		{name: "websearch phrase", opts: ListOptions{Query: ptr(`"go scheduler"`)}, want: []string{"https://example.test/summary"}},
		{name: "websearch exclusion", opts: ListOptions{Query: ptr("database -caleb")}, want: []string{"https://example.test/title"}},
		{name: "tag", opts: ListOptions{Tag: ptr("db")}, want: []string{"https://example.test/notes", "https://example.test/title"}},
		{name: "type", opts: ListOptions{SourceType: &youtube}, want: []string{"https://example.test/notes"}},
		{name: "combined", opts: ListOptions{Query: ptr("indexes"), Tag: ptr("db"), SourceType: &youtube}, want: []string{"https://example.test/notes"}},
		{name: "no match", opts: ListOptions{Query: ptr("kubernetes")}, want: []string{}},
		{name: "punctuation only", opts: ListOptions{Query: ptr("&|!")}, want: []string{}},
		{name: "filtered page", opts: ListOptions{Tag: ptr("db"), Limit: 1, Offset: 1}, want: []string{"https://example.test/title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listURLs(t, repo, tt.opts); !slices.Equal(got, tt.want) {
				t.Fatalf("List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchFollowsEdits(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	seedSearchEntries(t, pool, searchSeed{url: "https://example.test/a", title: "Old title"})
	entries, err := repo.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	title := "Sourdough starter guide"
	if _, err := repo.Update(context.Background(), entries[0].ID, &model.UpdateEntryInput{Title: model.Some(&title)}); err != nil {
		t.Fatal(err)
	}
	query := "sourdough"
	if got := listURLs(t, repo, ListOptions{Query: &query}); !slices.Equal(got, []string{"https://example.test/a"}) {
		t.Fatalf("List() after edit = %v", got)
	}
}

func TestCountFilteredScopedToEntry(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	seedSearchEntries(t, pool,
		searchSeed{url: "https://example.test/a", title: "Rust ownership", tag: "rust"},
		searchSeed{url: "https://example.test/b", title: "Rust lifetimes", tag: "rust"},
	)
	entries, err := repo.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := entries[0].ID // https://example.test/b
	ptr := func(s string) *string { return &s }

	for _, tt := range []struct {
		opts ListOptions
		want int
	}{
		{ListOptions{ID: &id}, 1},
		{ListOptions{ID: &id, Tag: ptr("rust")}, 1},
		{ListOptions{ID: &id, Query: ptr("lifetimes")}, 1},
		{ListOptions{ID: &id, Query: ptr("ownership")}, 0},
		{ListOptions{ID: &id, Query: ptr("rust -lifetimes")}, 0},
	} {
		got, err := repo.CountFiltered(context.Background(), tt.opts)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("CountFiltered(%+v) = %d, want %d", tt.opts, got, tt.want)
		}
	}
}

func TestListTagsReturnsDistinctSortedTags(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	seedSearchEntries(t, pool,
		searchSeed{url: "https://example.test/1", tag: "rust"},
		searchSeed{url: "https://example.test/2", tag: "ai"},
		searchSeed{url: "https://example.test/3", tag: "rust"},
		searchSeed{url: "https://example.test/4"},
	)

	tags, err := repo.ListTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ai", "rust"}; !slices.Equal(tags, want) {
		t.Fatalf("ListTags() = %v, want %v", tags, want)
	}
}

func intPtr(n int) *int { return &n }

func TestAggregateByTagIncludesUntaggedAndSumsToTotals(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		url, tag       string
		spent, runtime *int
		createdAt      time.Time
	}{
		{url: "https://example.test/1", tag: "go", spent: intPtr(600)},
		{url: "https://example.test/2", tag: "go", runtime: intPtr(300)},
		// time_spent_seconds wins over runtime_seconds.
		{url: "https://example.test/3", tag: "rust", spent: intPtr(1200), runtime: intPtr(60)},
		{url: "https://example.test/4", tag: "ai"},
		{url: "https://example.test/5", spent: intPtr(60)},
		{url: "https://example.test/6", runtime: intPtr(60)},
		// Outside the range.
		{url: "https://example.test/7", tag: "go", spent: intPtr(9000), createdAt: day.AddDate(0, 0, 10)},
	}
	for _, s := range seeds {
		createdAt := s.createdAt
		if createdAt.IsZero() {
			createdAt = day
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO entries (created_at, source_url, normalized_url, tag, time_spent_seconds, runtime_seconds, source_type)
			VALUES ($1, $2, $2, NULLIF($3, ''), $4, $5, 'article')`,
			createdAt, s.url, s.tag, s.spent, s.runtime)
		if err != nil {
			t.Fatal(err)
		}
	}
	start, end := day.Add(-time.Hour), day.AddDate(0, 0, 1)

	got, err := repo.AggregateByTag(ctx, start, end)
	if err != nil {
		t.Fatal(err)
	}
	// Longest time first; ties fall back to count, then name.
	want := []TagAggregation{
		{Tag: "rust", Count: 1, TimeSeconds: 1200},
		{Tag: "go", Count: 2, TimeSeconds: 900},
		{Tag: "", Count: 2, TimeSeconds: 120},
		{Tag: "ai", Count: 1, TimeSeconds: 0},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("AggregateByTag() = %+v, want %+v", got, want)
	}

	totals, err := repo.GetReportTotals(ctx, start, end)
	if err != nil {
		t.Fatal(err)
	}
	var count, seconds int
	for _, agg := range got {
		count += agg.Count
		seconds += agg.TimeSeconds
	}
	if count != totals.TotalEntries || seconds != totals.TotalTimeSeconds {
		t.Fatalf("tag rows sum to %d entries / %ds, totals are %d / %ds", count, seconds, totals.TotalEntries, totals.TotalTimeSeconds)
	}
}

// A tag stored before normalization (e.g. copied by migration 007) must group
// under its stored value and be found by filtering on exactly that value.
func TestAggregateByTagPaddedTagMatchesListFilter(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i, tag := range []string{" go ", " go ", "go"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO entries (created_at, source_url, normalized_url, tag, time_spent_seconds, source_type)
			VALUES ($1, $2, $2, $3, 60, 'article')`,
			day, fmt.Sprintf("https://example.test/%d", i), tag)
		if err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.AggregateByTag(ctx, day.Add(-time.Hour), day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := []TagAggregation{
		{Tag: " go ", Count: 2, TimeSeconds: 120},
		{Tag: "go", Count: 1, TimeSeconds: 60},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("AggregateByTag() = %+v, want %+v", got, want)
	}
	for _, agg := range got {
		count, err := repo.CountFiltered(ctx, ListOptions{Tag: &agg.Tag})
		if err != nil {
			t.Fatal(err)
		}
		if count != agg.Count {
			t.Fatalf("CountFiltered(tag %q) = %d, report counts %d", agg.Tag, count, agg.Count)
		}
	}
}
