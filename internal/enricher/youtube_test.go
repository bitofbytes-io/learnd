package enricher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
