package enricher

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

const readingWordsPerMinute = 200

// WebEnricher extracts metadata from generic web pages
type WebEnricher struct {
	client *http.Client
}

// NewWebEnricher creates a new web enricher
func NewWebEnricher() *WebEnricher {
	return &WebEnricher{
		client: newSafeHTTPClient(15 * time.Second),
	}
}

func (e *WebEnricher) Name() string            { return "web" }
func (e *WebEnricher) Priority() int           { return 100 } // Lowest priority, fallback
func (e *WebEnricher) CanHandle(_ string) bool { return true }

func (e *WebEnricher) Enrich(ctx context.Context, rawURL string) (*Result, error) {
	parsedURL, err := validateFetchURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Learnd/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Limit reading to 1MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse HTML
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := &Result{
		CanonicalURL: resp.Request.URL.String(), // Follow redirects
		Domain:       parsedURL.Hostname(),
		SourceType:   classifySourceType(parsedURL.Hostname(), ""),
		Metadata:     make(map[string]interface{}),
	}

	// Extract metadata from HTML
	extractMetadata(doc, resp.Request.URL, result)

	if result.RuntimeSeconds == nil && shouldEstimateReadTime(result.SourceType) {
		seconds, words := estimateReadingTimeSeconds(doc)
		if seconds > 0 {
			result.RuntimeSeconds = &seconds
			result.Metadata["read_time_seconds"] = seconds
			result.Metadata["word_count"] = words
		}
	}

	return result, nil
}

// extractMetadata walks the HTML tree and extracts title, description, etc.
// pageURL is the final fetched URL, used to resolve and vet the canonical link.
func extractMetadata(n *html.Node, pageURL *url.URL, result *Result) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "title":
			if n.FirstChild != nil && result.Title == "" {
				result.Title = strings.TrimSpace(n.FirstChild.Data)
			}
		case "meta":
			var name, property, content string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "name":
					name = attr.Val
				case "property":
					property = attr.Val
				case "content":
					content = attr.Val
				}
			}

			// Open Graph tags take priority
			switch property {
			case "og:title":
				if content != "" {
					result.Title = content
				}
			case "og:description":
				if content != "" {
					result.Description = content
				}
			case "og:type":
				result.Metadata["og_type"] = content
			}

			// Fall back to standard meta tags
			switch name {
			case "description":
				if result.Description == "" && content != "" {
					result.Description = content
				}
			}
		case "link":
			var rel, href string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				}
			}
			if rel == "canonical" && href != "" {
				if canonical := resolveCanonicalURL(pageURL, href); canonical != "" {
					result.CanonicalURL = canonical
				}
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractMetadata(c, pageURL, result)
	}
}

// resolveCanonicalURL resolves a <link rel=canonical> href against the fetched
// page URL. The canonical URL keys the shared summary cache, so it is accepted
// only as an absolute http(s) URL on the page's own registrable domain;
// anything else returns "" and the fetched URL is kept.
func resolveCanonicalURL(pageURL *url.URL, href string) string {
	if pageURL == nil {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	resolved := pageURL.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	if resolved.User != nil || resolved.Hostname() == "" {
		return ""
	}
	if !sameRegistrableDomain(pageURL.Hostname(), resolved.Hostname()) {
		return ""
	}
	return resolved.String()
}

func sameRegistrableDomain(a, b string) bool {
	a = strings.ToLower(strings.TrimSuffix(a, "."))
	b = strings.ToLower(strings.TrimSuffix(b, "."))
	if a == b {
		return true
	}
	if net.ParseIP(a) != nil || net.ParseIP(b) != nil {
		return false
	}
	domainA, errA := publicsuffix.EffectiveTLDPlusOne(a)
	domainB, errB := publicsuffix.EffectiveTLDPlusOne(b)
	return errA == nil && errB == nil && domainA == domainB
}

func shouldEstimateReadTime(sourceType model.SourceType) bool {
	switch sourceType {
	case model.SourceTypeArticle, model.SourceTypeDoc, model.SourceTypeOther:
		return true
	default:
		return false
	}
}

func estimateReadingTimeSeconds(doc *html.Node) (int, int) {
	root := findPrimaryContentNode(doc)
	words := countWords(root)
	if words == 0 {
		return 0, 0
	}

	minutes := (words + readingWordsPerMinute - 1) / readingWordsPerMinute
	return minutes * 60, words
}

func findPrimaryContentNode(doc *html.Node) *html.Node {
	if doc == nil {
		return nil
	}

	var article *html.Node
	var main *html.Node
	var body *html.Node

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "article":
				if article == nil {
					article = n
				}
			case "main":
				if main == nil {
					main = n
				}
			case "body":
				body = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	if article != nil {
		return article
	}
	if main != nil {
		return main
	}
	if body != nil {
		return body
	}
	return doc
}

func countWords(n *html.Node) int {
	return countWordsRecursive(n, false)
}

func countWordsRecursive(n *html.Node, skip bool) int {
	if n == nil {
		return 0
	}

	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "svg", "canvas", "head":
			return 0
		case "nav", "footer", "aside":
			skip = true
		}
	}

	if skip {
		return 0
	}

	if n.Type == html.TextNode {
		return len(strings.Fields(n.Data))
	}

	total := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		total += countWordsRecursive(c, skip)
	}
	return total
}

// classifySourceType determines the content type based on domain and og:type
func classifySourceType(domain, ogType string) model.SourceType {
	domain = strings.ToLower(domain)

	// Check for known platforms
	switch {
	case strings.Contains(domain, "youtube.com") || strings.Contains(domain, "youtu.be"):
		return model.SourceTypeYouTube
	case strings.Contains(domain, "podcasts.apple.com") || strings.Contains(domain, "spotify.com/episode"):
		return model.SourceTypePodcast
	case strings.Contains(domain, "medium.com") || strings.Contains(domain, "dev.to") ||
		strings.Contains(domain, "blog") || strings.Contains(domain, "substack.com"):
		return model.SourceTypeArticle
	case strings.Contains(domain, "docs.") || strings.Contains(domain, "documentation") ||
		strings.Contains(domain, "pkg.go.dev") || strings.Contains(domain, "developer."):
		return model.SourceTypeDoc
	}

	// Check og:type
	switch ogType {
	case "article":
		return model.SourceTypeArticle
	case "video", "video.other":
		return model.SourceTypeYouTube
	}

	return model.SourceTypeOther
}
