package enricher

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestEstimateReadingTimeSecondsPrefersArticle(t *testing.T) {
	t.Parallel()

	articleWords := readingWordsPerMinute
	otherWords := readingWordsPerMinute

	htmlInput := fmt.Sprintf(
		"<html><body><div>%s</div><article>%s</article></body></html>",
		buildWords(otherWords),
		buildWords(articleWords),
	)

	doc, err := html.Parse(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	seconds, words := estimateReadingTimeSeconds(doc)
	expectedSeconds := ((articleWords + readingWordsPerMinute - 1) / readingWordsPerMinute) * 60

	if words != articleWords {
		t.Fatalf("word count = %d, want %d", words, articleWords)
	}
	if seconds != expectedSeconds {
		t.Fatalf("seconds = %d, want %d", seconds, expectedSeconds)
	}
}

func TestEstimateReadingTimeSecondsIgnoresScriptStyle(t *testing.T) {
	t.Parallel()

	paragraphWords := 10
	htmlInput := fmt.Sprintf(
		"<html><head><style>%s</style></head><body><script>%s</script><p>%s</p></body></html>",
		buildWords(50),
		buildWords(50),
		buildWords(paragraphWords),
	)

	doc, err := html.Parse(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	seconds, words := estimateReadingTimeSeconds(doc)
	expectedSeconds := ((paragraphWords + readingWordsPerMinute - 1) / readingWordsPerMinute) * 60

	if words != paragraphWords {
		t.Fatalf("word count = %d, want %d", words, paragraphWords)
	}
	if seconds != expectedSeconds {
		t.Fatalf("seconds = %d, want %d", seconds, expectedSeconds)
	}
}

func TestEstimateReadingTimeSecondsRoundsUp(t *testing.T) {
	t.Parallel()

	wordsCount := readingWordsPerMinute + 1
	htmlInput := fmt.Sprintf("<html><body><main>%s</main></body></html>", buildWords(wordsCount))

	doc, err := html.Parse(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	seconds, words := estimateReadingTimeSeconds(doc)
	expectedSeconds := 2 * 60

	if words != wordsCount {
		t.Fatalf("word count = %d, want %d", words, wordsCount)
	}
	if seconds != expectedSeconds {
		t.Fatalf("seconds = %d, want %d", seconds, expectedSeconds)
	}
}

func buildWords(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSpace(strings.Repeat("word ", count))
}

func TestExtractMetadataCanonicalURL(t *testing.T) {
	tests := []struct {
		name    string
		pageURL string
		href    string
		want    string
	}{
		{"relative path", "https://www.example.com/articles/1?ref=x", "/articles/one", "https://www.example.com/articles/one"},
		{"absolute same host", "https://www.example.com/a", "https://www.example.com/b", "https://www.example.com/b"},
		{"sibling host same registrable domain", "https://www.example.com/a", "https://example.com/a", "https://example.com/a"},
		{"scheme-relative same host", "https://www.example.com/a", "//www.example.com/b", "https://www.example.com/b"},
		{"http to https upgrade", "http://www.example.com/a", "https://www.example.com/a", "https://www.example.com/a"},
		{"other domain", "https://www.example.com/a", "https://attacker.test/a", "https://www.example.com/a"},
		{"scheme-relative other domain", "https://www.example.com/a", "//attacker.test/a", "https://www.example.com/a"},
		{"shared public suffix", "https://alice.github.io/post", "https://bob.github.io/post", "https://alice.github.io/post"},
		{"javascript scheme", "https://www.example.com/a", "javascript:alert(1)", "https://www.example.com/a"},
		{"non-http scheme", "https://www.example.com/a", "ftp://www.example.com/a", "https://www.example.com/a"},
		{"userinfo", "https://www.example.com/a", "https://user:pass@www.example.com/a", "https://www.example.com/a"},
		{"unparseable", "https://www.example.com/a", "http://[::1", "https://www.example.com/a"},
		{"different IP host", "https://203.0.113.10/a", "https://203.0.113.11/a", "https://203.0.113.10/a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pageURL, err := url.Parse(tt.pageURL)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(fmt.Sprintf(
				`<html><head><title>Page</title><link rel="canonical" href=%q></head><body></body></html>`, tt.href)))
			if err != nil {
				t.Fatal(err)
			}
			result := &Result{CanonicalURL: pageURL.String(), Metadata: map[string]interface{}{}}

			extractMetadata(doc, pageURL, result)

			if result.CanonicalURL != tt.want {
				t.Fatalf("CanonicalURL = %q, want %q", result.CanonicalURL, tt.want)
			}
			if result.Title != "Page" {
				t.Fatalf("Title = %q, want %q", result.Title, "Page")
			}
		})
	}
}
