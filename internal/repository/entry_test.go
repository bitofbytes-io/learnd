package repository

import (
	"context"
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
