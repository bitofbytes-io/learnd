package enricher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestYouTubeEnrichTransportErrorOmitsAPIKey(t *testing.T) {
	const apiKey = "secret-youtube-key"
	var gotHeader, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Goog-Api-Key")
		gotQuery = r.URL.RawQuery
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	e := NewYouTubeEnricher(apiKey)
	e.endpoint = server.URL + "/youtube/v3/videos"
	e.client = server.Client()

	_, err := e.Enrich(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err == nil {
		t.Fatal("Enrich() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("Enrich() error leaks API key: %q", err)
	}
	if gotHeader != apiKey {
		t.Fatalf("X-Goog-Api-Key = %q, want %q", gotHeader, apiKey)
	}
	if strings.Contains(gotQuery, apiKey) {
		t.Fatalf("query string contains API key: %q", gotQuery)
	}
}

func TestYouTubeClientRejectsNonHTTPSRedirect(t *testing.T) {
	e := NewYouTubeEnricher("secret-youtube-key")
	req, err := http.NewRequest(http.MethodGet, "http://www.googleapis.com/youtube/v3/videos?id=x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.client.CheckRedirect(req, nil); err == nil {
		t.Fatal("CheckRedirect() allowed a redirect to plain HTTP")
	}
}

func newTestYouTubeEnricher(t *testing.T, handler http.HandlerFunc) *YouTubeEnricher {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	e := NewYouTubeEnricher("secret-youtube-key")
	e.endpoint = server.URL + "/youtube/v3/videos"
	e.client = server.Client()
	return e
}

func TestYouTubeEnrichAPIErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"quota exceeded", http.StatusForbidden, `{"error":{"message":"quota"}}`, "YouTube API error: 403"},
		{"server error", http.StatusInternalServerError, ``, "YouTube API error: 500"},
		{"video not found", http.StatusOK, `{"items":[]}`, "video not found"},
		{"malformed response", http.StatusOK, `{"items":`, "failed to decode response"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestYouTubeEnricher(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			result, err := e.Enrich(context.Background(), "https://youtu.be/dQw4w9WgXcQ")
			if err == nil {
				t.Fatalf("Enrich() = %+v, want error %q", result, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Enrich() error = %q, want %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "secret-youtube-key") {
				t.Fatalf("Enrich() error leaks API key: %q", err)
			}
		})
	}
}

func TestYouTubeEnrichParsesVideo(t *testing.T) {
	e := newTestYouTubeEnricher(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "dQw4w9WgXcQ" {
			t.Errorf("id = %q, want dQw4w9WgXcQ", got)
		}
		_, _ = w.Write([]byte(`{"items":[{"snippet":{"title":"A video","description":"About it",
			"channelTitle":"Chan","channelId":"C1","publishedAt":"2026-01-02T03:04:05Z"},
			"contentDetails":{"duration":"PT1H2M3S"}}]}`))
	})

	result, err := e.Enrich(context.Background(), "https://www.youtube.com/shorts/dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if result.CanonicalURL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" || result.Title != "A video" {
		t.Fatalf("Enrich() = %+v", result)
	}
	if result.RuntimeSeconds == nil || *result.RuntimeSeconds != 3723 {
		t.Fatalf("RuntimeSeconds = %v, want 3723", result.RuntimeSeconds)
	}
	if result.PublishedAt == nil || !result.PublishedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("PublishedAt = %v", result.PublishedAt)
	}
}
