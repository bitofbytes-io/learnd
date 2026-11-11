package summarizer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/drywaters/learnd/internal/model"
)

const testAPIKey = "test-api-key-123"

// newTestSummarizer returns a summarizer whose requests go to handler.
func newTestSummarizer(t *testing.T, handler http.HandlerFunc) *GeminiSummarizer {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	g, err := newGeminiSummarizer(context.Background(), testAPIKey, "test-model", option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("newGeminiSummarizer: %v", err)
	}
	t.Cleanup(func() { _ = g.client.Close() })
	return g
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func testInput() Input {
	return Input{Title: "Go testing", Description: "How to test Go code", SourceType: model.SourceType("article"), Tag: "go"}
}

func TestGeminiSummarizeResponses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantText string
		wantErr  string
		wantCode int
	}{
		{
			name:     "success",
			status:   http.StatusOK,
			body:     `{"candidates":[{"content":{"role":"model","parts":[{"text":"  Go tests are table driven. "}]},"finishReason":"STOP"}]}`,
			wantText: "Go tests are table driven.",
		},
		{
			name:     "multiple parts are joined",
			status:   http.StatusOK,
			body:     `{"candidates":[{"content":{"parts":[{"text":"Hello "},{"text":"world"}]}}]}`,
			wantText: "Hello world",
		},
		{
			name:    "no candidates",
			status:  http.StatusOK,
			body:    `{}`,
			wantErr: "no text generated",
		},
		{
			name:    "empty candidates",
			status:  http.StatusOK,
			body:    `{"candidates":[]}`,
			wantErr: "no text generated",
		},
		{
			name:    "candidate without parts",
			status:  http.StatusOK,
			body:    `{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}]}`,
			wantErr: "no text generated",
		},
		{
			name:    "whitespace only text",
			status:  http.StatusOK,
			body:    `{"candidates":[{"content":{"parts":[{"text":"  \n "}]}}]}`,
			wantErr: "no text generated",
		},
		{
			name:    "candidate blocked by safety",
			status:  http.StatusOK,
			body:    `{"candidates":[{"finishReason":"SAFETY","safetyRatings":[{"category":"HARM_CATEGORY_DANGEROUS_CONTENT","probability":"HIGH"}]}]}`,
			wantErr: "blocked: candidate: FinishReasonSafety",
		},
		{
			name:    "prompt blocked by safety",
			status:  http.StatusOK,
			body:    `{"promptFeedback":{"blockReason":"SAFETY","safetyRatings":[{"category":"HARM_CATEGORY_HARASSMENT","probability":"HIGH"}]}}`,
			wantErr: "blocked: prompt: BlockReasonSafety",
		},
		{
			name:     "rate limited",
			status:   http.StatusTooManyRequests,
			body:     `{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			wantErr:  "gemini generation failed",
			wantCode: http.StatusTooManyRequests,
		},
		{
			name:     "server error",
			status:   http.StatusInternalServerError,
			body:     `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`,
			wantErr:  "gemini generation failed",
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "bad request",
			status:   http.StatusBadRequest,
			body:     `{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`,
			wantErr:  "gemini generation failed",
			wantCode: http.StatusBadRequest,
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    `{"candidates":[{"content":`,
			wantErr: "gemini generation failed",
		},
		{
			name:    "non json body",
			status:  http.StatusOK,
			body:    `<html>gateway</html>`,
			wantErr: "gemini generation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newTestSummarizer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tt.status, tt.body)
			})

			res, err := g.Summarize(context.Background(), testInput())

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got result %+v", tt.wantErr, res)
				}
				if res != nil {
					t.Errorf("result = %+v, want nil on error", res)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				if tt.wantCode != 0 {
					var apiErr *googleapi.Error
					if !errors.As(err, &apiErr) {
						t.Fatalf("error %v does not wrap *googleapi.Error", err)
					}
					if apiErr.Code != tt.wantCode {
						t.Errorf("API error code = %d, want %d", apiErr.Code, tt.wantCode)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Summarize: %v", err)
			}
			if res.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", res.Text, tt.wantText)
			}
			if res.Provider != "gemini" || res.Model != "test-model" || res.Version != geminiVersion {
				t.Errorf("metadata = %s/%s/%s, want gemini/test-model/%s", res.Provider, res.Model, res.Version, geminiVersion)
			}
			if res.GeneratedAt.IsZero() || res.GeneratedAt.Location() != time.UTC {
				t.Errorf("GeneratedAt = %v, want non-zero UTC time", res.GeneratedAt)
			}
		})
	}
}

// The Gemini client retries 503 responses itself, so a transient outage is
// absorbed and a persistent one is only bounded by the caller's context.
func TestGeminiSummarizeServiceUnavailable(t *testing.T) {
	const unavailable = `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`

	t.Run("recovers after a transient 503", func(t *testing.T) {
		var calls atomic.Int32
		g := newTestSummarizer(t, func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				writeJSON(w, http.StatusServiceUnavailable, unavailable)
				return
			}
			writeJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"recovered"}]}}]}`)
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		res, err := g.Summarize(ctx, testInput())
		if err != nil {
			t.Fatalf("Summarize: %v", err)
		}
		if res.Text != "recovered" {
			t.Errorf("Text = %q, want recovered", res.Text)
		}
		if calls.Load() != 2 {
			t.Errorf("server calls = %d, want 2", calls.Load())
		}
	})

	t.Run("persistent 503 stops at the context deadline", func(t *testing.T) {
		const deadline = 500 * time.Millisecond

		var calls atomic.Int32
		g := newTestSummarizer(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			writeJSON(w, http.StatusServiceUnavailable, unavailable)
		})

		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		defer cancel()

		start := time.Now()
		res, err := g.Summarize(ctx, testInput())
		elapsed := time.Since(start)
		if err == nil {
			t.Fatalf("expected error, got result %+v", res)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want context deadline exceeded", err)
		}
		if !strings.Contains(err.Error(), "gemini generation failed") {
			t.Errorf("error = %q, want gemini generation failed prefix", err)
		}
		// The deadline cannot fire early, so returning sooner means the client
		// gave up on the 503 instead of retrying; the upper bound is generous
		// to stay stable on slow runners.
		if elapsed < deadline {
			t.Errorf("returned after %v, want retries until the %v deadline", elapsed, deadline)
		}
		if elapsed > 10*time.Second {
			t.Errorf("returned after %v, want shortly after the %v deadline", elapsed, deadline)
		}
		// Backoff jitter makes the number of attempts nondeterministic, but at
		// least one request must have reached the server.
		if calls.Load() < 1 {
			t.Errorf("server calls = %d, want at least 1", calls.Load())
		}
	})
}

func TestGeminiSummarizeRequest(t *testing.T) {
	var calls atomic.Int32
	var gotMethod, gotPath, gotQuery, gotHeaderKey string
	var gotBody struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			Temperature     float64 `json:"temperature"`
			MaxOutputTokens int     `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}
	var decodeErr error

	g := newTestSummarizer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotHeaderKey = r.Header.Get("X-Goog-Api-Key")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	})

	if _, err := g.Summarize(context.Background(), testInput()); err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("server calls = %d, want 1", calls.Load())
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/models/test-model:generateContent") {
		t.Errorf("path = %q, want suffix /models/test-model:generateContent", gotPath)
	}
	if gotHeaderKey != testAPIKey {
		t.Errorf("X-Goog-Api-Key header = %q, want %q", gotHeaderKey, testAPIKey)
	}
	// The key must never travel in the URL, where it would end up in logs.
	if strings.Contains(gotQuery, testAPIKey) || strings.Contains(gotPath, testAPIKey) ||
		strings.Contains(strings.ToLower(gotQuery), "key=") {
		t.Errorf("API key leaked into URL: path=%q query=%q", gotPath, gotQuery)
	}
	if decodeErr != nil {
		t.Fatalf("decode request body: %v", decodeErr)
	}
	if len(gotBody.Contents) != 1 || len(gotBody.Contents[0].Parts) != 1 {
		t.Fatalf("request contents = %+v, want one content with one part", gotBody.Contents)
	}
	if prompt := gotBody.Contents[0].Parts[0].Text; !strings.Contains(prompt, "Title: Go testing") {
		t.Errorf("prompt = %q, want it to contain the title", prompt)
	}
	if got := gotBody.GenerationConfig.MaxOutputTokens; got != 150 {
		t.Errorf("maxOutputTokens = %d, want 150", got)
	}
	if got := gotBody.GenerationConfig.Temperature; got < 0.29 || got > 0.31 {
		t.Errorf("temperature = %v, want 0.3", got)
	}
}

func TestGeminiSummarizeTransportErrorOmitsAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	g, err := newGeminiSummarizer(context.Background(), testAPIKey, "test-model", option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("newGeminiSummarizer: %v", err)
	}
	t.Cleanup(func() { _ = g.client.Close() })
	srv.Close() // connection refused from here on

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = g.Summarize(ctx, testInput())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error leaks the API key: %v", err)
	}
}

func TestGeminiSummarizeTimeout(t *testing.T) {
	release := make(chan struct{})
	g := newTestSummarizer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	// Registered after the server cleanup, so it runs first and unblocks the handler.
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := g.Summarize(ctx, testInput())
	if err == nil {
		t.Fatalf("expected timeout error, got result %+v", res)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if !strings.Contains(err.Error(), "gemini generation failed") {
		t.Errorf("error = %q, want gemini generation failed prefix", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Summarize took %v, want it to honour the context deadline", elapsed)
	}
}

func TestNewGeminiSummarizerWithModel(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantModel string
	}{
		{"default when empty", "", geminiDefaultModel},
		{"default when blank", "   ", geminiDefaultModel},
		{"explicit model", "gemini-custom", "gemini-custom"},
		{"model is trimmed", "  gemini-custom\t", "gemini-custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewGeminiSummarizerWithModel(context.Background(), testAPIKey, tt.model)
			if err != nil {
				t.Fatalf("NewGeminiSummarizerWithModel: %v", err)
			}
			t.Cleanup(func() { _ = g.client.Close() })

			if g.Model() != tt.wantModel {
				t.Errorf("Model() = %q, want %q", g.Model(), tt.wantModel)
			}
			if g.Provider() != "gemini" {
				t.Errorf("Provider() = %q, want gemini", g.Provider())
			}
			if g.Version() != geminiVersion {
				t.Errorf("Version() = %q, want %q", g.Version(), geminiVersion)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	long := strings.Repeat("x", 1500)
	tests := []struct {
		name        string
		input       Input
		contains    []string
		notContains []string
	}{
		{
			name:     "all fields",
			input:    Input{Title: "T", Description: "D", SourceType: model.SourceType("video"), Tag: "go"},
			contains: []string{"Summarize this video", "Title: T", "Description: D", "Topics: go", "Summary:"},
		},
		{
			name:        "optional fields omitted",
			input:       Input{SourceType: model.SourceType("article")},
			contains:    []string{"Summarize this article", "Summary:"},
			notContains: []string{"Title:", "Description:", "Topics:"},
		},
		{
			name:        "long description truncated",
			input:       Input{Description: long},
			contains:    []string{"Description: " + long[:1000] + "..."},
			notContains: []string{long[:1001]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPrompt(tt.input)
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("prompt missing %q:\n%s", s, got)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(got, s) {
					t.Errorf("prompt unexpectedly contains %q:\n%s", s, got)
				}
			}
		})
	}
}

func TestExtractTextNil(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Errorf("extractText(nil) = %q, want empty", got)
	}
}
