package enricher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/drywaters/learnd/internal/model"
)

// YouTube URL patterns
var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/|youtube\.com/embed/)([a-zA-Z0-9_-]{11})`),
}

const youtubeVideosEndpoint = "https://www.googleapis.com/youtube/v3/videos"

// YouTubeEnricher extracts metadata from YouTube videos using the Data API v3
type YouTubeEnricher struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewYouTubeEnricher creates a new YouTube enricher
func NewYouTubeEnricher(apiKey string) *YouTubeEnricher {
	return &YouTubeEnricher{
		apiKey:   apiKey,
		endpoint: youtubeVideosEndpoint,
		client:   newSafeHTTPClient(10*time.Second, "www.googleapis.com"),
	}
}

func (e *YouTubeEnricher) Name() string  { return "youtube" }
func (e *YouTubeEnricher) Priority() int { return 10 }

func (e *YouTubeEnricher) CanHandle(rawURL string) bool {
	return extractVideoID(rawURL) != ""
}

func (e *YouTubeEnricher) Enrich(ctx context.Context, rawURL string) (*Result, error) {
	videoID := extractVideoID(rawURL)
	if videoID == "" {
		return nil, fmt.Errorf("could not extract video ID from URL")
	}

	// Build API request. The key travels in a header so it never appears in
	// the request URL, which transport errors echo back.
	apiURL := fmt.Sprintf("%s?id=%s&part=snippet,contentDetails", e.endpoint, url.QueryEscape(videoID))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Goog-Api-Key", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("failed to fetch YouTube API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube API error: %d", resp.StatusCode)
	}

	var apiResp youtubeAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Items) == 0 {
		return nil, fmt.Errorf("video not found")
	}

	item := apiResp.Items[0]
	snippet := item.Snippet
	contentDetails := item.ContentDetails

	// Parse duration
	var runtimeSeconds *int
	if duration := parseDuration(contentDetails.Duration); duration > 0 {
		runtimeSeconds = &duration
	}

	// Parse published date
	var publishedAt *time.Time
	if t, err := time.Parse(time.RFC3339, snippet.PublishedAt); err == nil {
		publishedAt = &t
	}

	return &Result{
		CanonicalURL:   fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
		Domain:         "youtube.com",
		SourceType:     model.SourceTypeYouTube,
		Title:          snippet.Title,
		Description:    truncateDescription(snippet.Description),
		PublishedAt:    publishedAt,
		RuntimeSeconds: runtimeSeconds,
		Metadata: map[string]interface{}{
			"channel_title": snippet.ChannelTitle,
			"channel_id":    snippet.ChannelID,
			"video_id":      videoID,
		},
	}, nil
}

// extractVideoID extracts the video ID from various YouTube URL formats
func extractVideoID(rawURL string) string {
	for _, pattern := range youtubePatterns {
		if matches := pattern.FindStringSubmatch(rawURL); len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// truncateDescription limits description length for storage
func truncateDescription(desc string) string {
	// Take first 500 characters
	if len(desc) > 500 {
		// Try to break at a sentence boundary
		if idx := strings.LastIndex(desc[:500], ". "); idx > 200 {
			return desc[:idx+1]
		}
		return desc[:500] + "..."
	}
	return desc
}

// YouTube API response structures
type youtubeAPIResponse struct {
	Items []youtubeVideoItem `json:"items"`
}

type youtubeVideoItem struct {
	Snippet        youtubeSnippet        `json:"snippet"`
	ContentDetails youtubeContentDetails `json:"contentDetails"`
}

type youtubeSnippet struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	ChannelTitle string `json:"channelTitle"`
	ChannelID    string `json:"channelId"`
	PublishedAt  string `json:"publishedAt"`
}

type youtubeContentDetails struct {
	Duration string `json:"duration"`
}
