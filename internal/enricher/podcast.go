package enricher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"golang.org/x/net/html"
)

var (
	// Apple Podcasts URL patterns
	podcastIDPattern = regexp.MustCompile(`/id(\d+)`)
	episodeIDPattern = regexp.MustCompile(`[?&]i=(\d+)`)
)

// PodcastEnricher extracts metadata from Apple Podcasts URLs
type PodcastEnricher struct {
	client *http.Client
}

// NewPodcastEnricher creates a new podcast enricher
func NewPodcastEnricher() *PodcastEnricher {
	return &PodcastEnricher{
		client: newSafeHTTPClient(15*time.Second, "podcasts.apple.com"),
	}
}

func (e *PodcastEnricher) Priority() int { return 20 }

func (e *PodcastEnricher) CanHandle(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsedURL.Hostname(), "podcasts.apple.com")
}

func (e *PodcastEnricher) Enrich(ctx context.Context, rawURL string) (*Result, error) {
	parsedURL, err := validateFetchURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(parsedURL.Hostname(), "podcasts.apple.com") {
		return nil, fmt.Errorf("unsupported host: %s", parsedURL.Hostname())
	}

	// Extract podcast and episode IDs
	podcastID := ""
	if matches := podcastIDPattern.FindStringSubmatch(rawURL); len(matches) > 1 {
		podcastID = matches[1]
	}

	episodeID := ""
	if matches := episodeIDPattern.FindStringSubmatch(rawURL); len(matches) > 1 {
		episodeID = matches[1]
	}

	// Fetch the page and parse HTML for metadata
	req, err := http.NewRequestWithContext(ctx, "GET", parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := &Result{
		CanonicalURL: resp.Request.URL.String(),
		Domain:       parsedURL.Hostname(),
		SourceType:   model.SourceTypePodcast,
		Metadata: map[string]interface{}{
			"podcast_id": podcastID,
			"episode_id": episodeID,
		},
	}

	// Extract metadata from HTML
	extractPodcastMetadata(doc, result)
	if result.RuntimeSeconds == nil {
		slog.Info("podcast duration not found", "url", rawURL)
	}

	return result, nil
}

// extractPodcastMetadata extracts podcast-specific metadata from HTML
func extractPodcastMetadata(n *html.Node, result *Result) {
	if n.Type == html.ElementNode && n.Data == "meta" {
		var property, name, content string
		for _, attr := range n.Attr {
			switch attr.Key {
			case "property":
				property = attr.Val
			case "name":
				name = attr.Val
			case "content":
				content = attr.Val
			}
		}

		switch property {
		case "og:title":
			if content != "" {
				result.Title = content
			}
		case "og:description":
			if content != "" {
				result.Description = content
			}
		case "music:duration":
			// Duration in seconds
			if content != "" {
				var seconds int
				fmt.Sscanf(content, "%d", &seconds)
				if seconds > 0 {
					result.RuntimeSeconds = &seconds
				}
			}
		case "music:release_date":
			if content != "" {
				if t, err := time.Parse("2006-01-02", content); err == nil {
					result.PublishedAt = &t
				}
			}
		}

		// Apple-specific meta tags
		switch name {
		case "apple:title":
			if result.Title == "" && content != "" {
				result.Title = content
			}
		case "apple:description":
			if result.Description == "" && content != "" {
				result.Description = content
			}
		}
	}
	if n.Type == html.ElementNode && n.Data == "script" && result.RuntimeSeconds == nil {
		var scriptType string
		for _, attr := range n.Attr {
			if attr.Key == "type" {
				scriptType = attr.Val
				break
			}
		}
		if scriptType == "application/ld+json" {
			if seconds := parseJSONLDDuration(nodeText(n)); seconds > 0 {
				result.RuntimeSeconds = &seconds
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractPodcastMetadata(c, result)
	}
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var builder strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			builder.WriteString(c.Data)
		}
	}
	return builder.String()
}

func parseJSONLDDuration(content string) int {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0
	}

	var data interface{}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return 0
	}

	return findDurationSeconds(data)
}

func findDurationSeconds(data interface{}) int {
	switch value := data.(type) {
	case map[string]interface{}:
		for key, child := range value {
			if key == "duration" {
				if seconds := parseDurationValue(child); seconds > 0 {
					return seconds
				}
			}
		}
		for _, child := range value {
			if seconds := findDurationSeconds(child); seconds > 0 {
				return seconds
			}
		}
	case []interface{}:
		for _, child := range value {
			if seconds := findDurationSeconds(child); seconds > 0 {
				return seconds
			}
		}
	}

	return 0
}

func parseDurationValue(value interface{}) int {
	switch duration := value.(type) {
	case string:
		return parseDurationString(duration)
	case json.Number:
		if seconds, err := duration.Int64(); err == nil && seconds > 0 {
			return int(seconds)
		}
	case float64:
		if duration > 0 {
			return int(duration)
		}
	case map[string]interface{}:
		if nested, ok := duration["value"]; ok {
			if seconds := parseDurationValue(nested); seconds > 0 {
				return seconds
			}
		}
		if nested, ok := duration["@value"]; ok {
			if seconds := parseDurationValue(nested); seconds > 0 {
				return seconds
			}
		}
	}

	return 0
}

func parseDurationString(duration string) int {
	trimmed := strings.TrimSpace(duration)
	if trimmed == "" {
		return 0
	}

	if strings.HasPrefix(trimmed, "P") {
		if strings.HasPrefix(trimmed, "PT") {
			if seconds := parseDuration(trimmed); seconds > 0 {
				return seconds
			}
		}
		if idx := strings.Index(trimmed, "T"); idx != -1 {
			if seconds := parseDuration("PT" + trimmed[idx+1:]); seconds > 0 {
				return seconds
			}
		}
	}

	if strings.Contains(trimmed, ":") {
		if seconds := parseClockDuration(trimmed); seconds > 0 {
			return seconds
		}
	}

	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return seconds
	}

	return 0
}

func parseClockDuration(duration string) int {
	parts := strings.Split(duration, ":")
	if len(parts) == 2 {
		minutes, okMinutes := parseClockPart(parts[0])
		seconds, okSeconds := parseClockPart(parts[1])
		if okMinutes && okSeconds {
			return minutes*60 + seconds
		}
	}
	if len(parts) == 3 {
		hours, okHours := parseClockPart(parts[0])
		minutes, okMinutes := parseClockPart(parts[1])
		seconds, okSeconds := parseClockPart(parts[2])
		if okHours && okMinutes && okSeconds {
			return hours*3600 + minutes*60 + seconds
		}
	}

	return 0
}

func parseClockPart(part string) (int, bool) {
	trimmed := strings.TrimSpace(part)
	if trimmed == "" {
		return 0, false
	}
	if idx := strings.Index(trimmed, "."); idx != -1 {
		trimmed = trimmed[:idx]
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, false
	}
	return value, true
}
