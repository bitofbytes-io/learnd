package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/google/uuid"
)

func TestCapturePageUsesPageOffsetAndRendersPagination(t *testing.T) {
	var gotOpts repository.ListOptions
	mock := &mockEntryRepo{
		listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
			gotOpts = opts
			return []model.Entry{*createDashboardEntry("Page 2 Entry")}, nil
		},
		countFn: func(_ context.Context) (int, error) {
			return 45, nil
		},
		getDuplicateCountsByNormalizedURL: func(_ context.Context, normalizedURLs []string) (map[string]int, error) {
			return map[string]int{"https://example.com/page-2-entry": 1}, nil
		},
	}

	handler := NewCaptureHandler(mock, false)
	req := httptest.NewRequest(http.MethodGet, "/?page=2&url=https://prefill.test", nil)
	rec := httptest.NewRecorder()

	handler.CapturePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CapturePage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotOpts.Limit != dashboardPageSize || gotOpts.Offset != dashboardPageSize {
		t.Fatalf("CapturePage() list opts = %+v, want limit=%d offset=%d", gotOpts, dashboardPageSize, dashboardPageSize)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`href="/"`,
		`href="/?page=3"`,
		`value="https://prefill.test"`,
		"Page 2 Entry",
		`href="/entries/`,
		`return_to=%2F%3Fpage%3D2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("CapturePage() body missing %q", want)
		}
	}
	if got := strings.Count(body, "Page 2 of 3"); got != 2 {
		t.Fatalf("CapturePage() page indicator count = %d, want 2", got)
	}
}

func TestCapturePagePrefillsURLFromSharedText(t *testing.T) {
	mock := &mockEntryRepo{
		listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
			return nil, nil
		},
		countFn: func(_ context.Context) (int, error) {
			return 0, nil
		},
	}

	handler := NewCaptureHandler(mock, false)
	req := httptest.NewRequest(http.MethodGet, "/?text=Check+this+https://x.com/a", nil)
	rec := httptest.NewRecorder()

	handler.CapturePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CapturePage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, `value="https://x.com/a"`) {
		t.Fatalf("CapturePage() body missing shared URL prefill")
	}
}

func TestSharedPrefillURL(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		want  string
	}{
		{name: "url param", query: url.Values{"url": {"https://example.com/a"}}, want: "https://example.com/a"},
		{name: "url param wins over text", query: url.Values{"url": {"https://example.com/a"}, "text": {"see https://example.com/b"}}, want: "https://example.com/a"},
		{name: "link in text", query: url.Values{"text": {"Check this https://x.com/a"}}, want: "https://x.com/a"},
		{name: "link on its own line", query: url.Values{"title": {"A post"}, "text": {"A post\nhttps://x.com/a"}}, want: "https://x.com/a"},
		{name: "trailing punctuation trimmed", query: url.Values{"text": {"Read https://x.com/a."}}, want: "https://x.com/a"},
		{name: "parenthesized link", query: url.Values{"text": {"Read (https://x.com/a)."}}, want: "https://x.com/a"},
		{name: "angle-bracketed link", query: url.Values{"text": {"<https://x.com/a>"}}, want: "https://x.com/a"},
		{name: "quoted link", query: url.Values{"text": {"“https://x.com/a”"}}, want: "https://x.com/a"},
		{name: "balanced paren in link kept", query: url.Values{"text": {"See https://en.wikipedia.org/wiki/Go_(programming_language)"}}, want: "https://en.wikipedia.org/wiki/Go_(programming_language)"},
		{name: "wrapped link with inner paren", query: url.Values{"text": {"(https://en.wikipedia.org/wiki/Go_(programming_language))"}}, want: "https://en.wikipedia.org/wiki/Go_(programming_language)"},
		{name: "invalid url falls back to text", query: url.Values{"url": {"not a url"}, "text": {"https://x.com/a"}}, want: "https://x.com/a"},
		{name: "skips unsafe link in text", query: url.Values{"text": {"http://localhost/x then https://x.com/a"}}, want: "https://x.com/a"},
		{name: "text without link", query: url.Values{"text": {"just words"}}, want: ""},
		{name: "unsafe url dropped", query: url.Values{"url": {"http://127.0.0.1/admin"}}, want: ""},
		{name: "non-http scheme dropped", query: url.Values{"text": {"httpfoo:bar"}}, want: ""},
		{name: "empty", query: url.Values{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sharedPrefillURL(tt.query); got != tt.want {
				t.Fatalf("sharedPrefillURL(%v) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestCapturePagePaginationRequestRendersEntriesRegionOnly(t *testing.T) {
	mock := &mockEntryRepo{
		listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
			return []model.Entry{*createDashboardEntry("HTMX Entry")}, nil
		},
		countFn: func(_ context.Context) (int, error) {
			return 25, nil
		},
		getDuplicateCountsByNormalizedURL: func(_ context.Context, normalizedURLs []string) (map[string]int, error) {
			return map[string]int{"https://example.com/htmx-entry": 1}, nil
		},
	}

	handler := NewCaptureHandler(mock, false)
	req := httptest.NewRequest(http.MethodGet, "/?page=2", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "dashboard-entries-region")
	rec := httptest.NewRecorder()

	handler.CapturePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CapturePage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="dashboard-entries-region"`) {
		t.Fatalf("CapturePage() HTMX body missing entries region: %q", body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<main class=\"max-w-4xl") {
		t.Fatalf("CapturePage() HTMX body should not include full page chrome: %q", body)
	}
}

func TestCapturePageBoostedNavigationStillRendersFullPage(t *testing.T) {
	mock := &mockEntryRepo{
		listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
			return []model.Entry{*createDashboardEntry("Boosted Entry")}, nil
		},
		countFn: func(_ context.Context) (int, error) {
			return 25, nil
		},
		getDuplicateCountsByNormalizedURL: func(_ context.Context, normalizedURLs []string) (map[string]int, error) {
			return map[string]int{"https://example.com/boosted-entry": 1}, nil
		},
	}

	handler := NewCaptureHandler(mock, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	rec := httptest.NewRecorder()

	handler.CapturePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CapturePage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"<html", `id="capture-form"`, `id="dashboard-entries-region"`, "Boosted Entry"} {
		if !strings.Contains(body, want) {
			t.Fatalf("CapturePage() boosted body missing %q: %q", want, body)
		}
	}
}

func TestCapturePageClampsInvalidAndOutOfRangePages(t *testing.T) {
	t.Run("invalid page falls back to first page", func(t *testing.T) {
		var calls []repository.ListOptions
		mock := &mockEntryRepo{
			listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
				calls = append(calls, opts)
				return nil, nil
			},
			countFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		}

		handler := NewCaptureHandler(mock, false)
		req := httptest.NewRequest(http.MethodGet, "/?page=nope", nil)
		rec := httptest.NewRecorder()
		handler.CapturePage(rec, req)

		if len(calls) != 1 {
			t.Fatalf("CapturePage() list calls = %d, want 1", len(calls))
		}
		if calls[0].Offset != 0 {
			t.Fatalf("CapturePage() offset = %d, want 0", calls[0].Offset)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Page 1 of 1") {
			t.Fatalf("CapturePage() body missing first-page indicator: %q", body)
		}
	})

	t.Run("out of range page clamps to last page before listing", func(t *testing.T) {
		var calls []repository.ListOptions
		mock := &mockEntryRepo{
			listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
				calls = append(calls, opts)
				if opts.Offset == dashboardPageSize {
					return []model.Entry{*createDashboardEntry("Last Page Entry")}, nil
				}
				return nil, nil
			},
			countFn: func(_ context.Context) (int, error) {
				return 21, nil
			},
			getDuplicateCountsByNormalizedURL: func(_ context.Context, normalizedURLs []string) (map[string]int, error) {
				return map[string]int{"https://example.com/last-page-entry": 1}, nil
			},
		}

		handler := NewCaptureHandler(mock, false)
		req := httptest.NewRequest(http.MethodGet, "/?page=999", nil)
		rec := httptest.NewRecorder()
		handler.CapturePage(rec, req)

		if len(calls) != 1 {
			t.Fatalf("CapturePage() list calls = %d, want 1", len(calls))
		}
		if calls[0].Offset != dashboardPageSize {
			t.Fatalf("CapturePage() offset = %d, want %d", calls[0].Offset, dashboardPageSize)
		}
		body := rec.Body.String()
		for _, want := range []string{"Page 2 of 2", `href="/"`, "Last Page Entry"} {
			if !strings.Contains(body, want) {
				t.Fatalf("CapturePage() body missing %q", want)
			}
		}
	})

	t.Run("huge valid page does not overflow offset", func(t *testing.T) {
		var calls []repository.ListOptions
		mock := &mockEntryRepo{
			listFn: func(_ context.Context, opts repository.ListOptions) ([]model.Entry, error) {
				calls = append(calls, opts)
				if opts.Offset == dashboardPageSize {
					return []model.Entry{*createDashboardEntry("Last Page Entry")}, nil
				}
				return nil, nil
			},
			countFn: func(_ context.Context) (int, error) {
				return 21, nil
			},
			getDuplicateCountsByNormalizedURL: func(_ context.Context, normalizedURLs []string) (map[string]int, error) {
				return map[string]int{"https://example.com/last-page-entry": 1}, nil
			},
		}

		handler := NewCaptureHandler(mock, false)
		req := httptest.NewRequest(http.MethodGet, "/?page="+strconv.FormatInt(int64(math.MaxInt), 10), nil)
		rec := httptest.NewRecorder()
		handler.CapturePage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("CapturePage() status = %d, want %d", rec.Code, http.StatusOK)
		}
		if len(calls) != 1 {
			t.Fatalf("CapturePage() list calls = %d, want 1", len(calls))
		}
		if calls[0].Offset != dashboardPageSize {
			t.Fatalf("CapturePage() offset = %d, want %d", calls[0].Offset, dashboardPageSize)
		}
		body := rec.Body.String()
		for _, want := range []string{"Page 2 of 2", `href="/"`, "Last Page Entry"} {
			if !strings.Contains(body, want) {
				t.Fatalf("CapturePage() body missing %q", want)
			}
		}
	})
}

func createDashboardEntry(title string) *model.Entry {
	normalized := fmt.Sprintf("https://example.com/%s", strings.ToLower(strings.ReplaceAll(title, " ", "-")))
	return &model.Entry{
		ID:               uuid.New(),
		CreatedAt:        time.Now(),
		SourceURL:        normalized,
		NormalizedURL:    normalized,
		SourceType:       model.SourceTypeArticle,
		Title:            &title,
		EnrichmentStatus: model.StatusOK,
		SummaryStatus:    model.StatusOK,
	}
}
