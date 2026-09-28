package worker

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid ASCII",
			input: "Hello, World!",
			want:  "Hello, World!",
		},
		{
			name:  "valid UTF-8 with emoji",
			input: "Hello 🌍 World",
			want:  "Hello 🌍 World",
		},
		{
			name:  "valid UTF-8 with multilingual",
			input: "日本語 Ελληνικά עברית",
			want:  "日本語 Ελληνικά עברית",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "truncated emoji sequence",
			input: "Hello \xf0\x9f..World", // 0xf0 0x9f followed by invalid continuation bytes
			want:  "Hello ..World",
		},
		{
			name:  "invalid byte in middle",
			input: "Hello \xff World",
			want:  "Hello  World",
		},
		{
			name:  "multiple invalid sequences",
			input: "\xfe start \xff middle \xf0\x9f end",
			want:  " start  middle  end",
		},
		{
			name:  "only invalid bytes",
			input: "\xff\xfe\xf0\x9f",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUTF8(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeUTF8(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestJobErrorMessageRedactsRequestURL(t *testing.T) {
	cause := fmt.Errorf("failed to fetch: %w", &url.Error{
		Op:  "Get",
		URL: "https://user:pass@www.googleapis.com/youtube/v3/videos?id=abc&key=secret-key",
		Err: errors.New("connection reset"),
	})

	got := jobErrorMessage(cause)

	for _, leaked := range []string{"secret-key", "pass", "id=abc"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("jobErrorMessage() = %q, leaks %q", got, leaked)
		}
	}
	want := `failed to fetch: Get "https://www.googleapis.com/youtube/v3/videos": connection reset`
	if got != want {
		t.Fatalf("jobErrorMessage() = %q, want %q", got, want)
	}
}

func TestJobErrorMessageKeepsPlainErrors(t *testing.T) {
	if got := jobErrorMessage(errors.New("YouTube API error: 403")); got != "YouTube API error: 403" {
		t.Fatalf("jobErrorMessage() = %q", got)
	}
}
