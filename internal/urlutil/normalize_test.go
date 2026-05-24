package urlutil

import "testing"

func TestNormalizeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips tracking params and fragment",
			raw:  "https://Example.com/Path/?utm_source=newsletter&utm_medium=email#section",
			want: "https://example.com/Path",
		},
		{
			name: "preserves meaningful query params",
			raw:  "https://example.com/watch?v=abc&utm_source=foo",
			want: "https://example.com/watch?v=abc",
		},
		{
			name: "removes default http port and trims slash",
			raw:  "http://example.com:80/path/",
			want: "http://example.com/path",
		},
		{
			name: "removes default https port",
			raw:  "https://example.com:443/path",
			want: "https://example.com/path",
		},
		{
			name: "normalizes root path",
			raw:  "https://example.com",
			want: "https://example.com/",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeURL(tt.raw)
			if err != nil {
				t.Fatalf("NormalizeURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateSourceURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "relative", raw: "example.com/path"},
		{name: "unsafe scheme", raw: "javascript://example.com/%0aalert(1)"},
		{name: "userinfo", raw: "https://user:pass@example.com/path"},
		{name: "localhost", raw: "http://localhost:8080/path"},
		{name: "private ip", raw: "http://192.168.1.10/path"},
		{name: "reserved ip", raw: "http://203.0.113.10/path"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, err := ValidateSourceURL(tt.raw); err == nil {
				t.Fatalf("ValidateSourceURL(%q) = %q, want error", tt.raw, got)
			}
		})
	}
}

func TestSafeLinkURL(t *testing.T) {
	t.Parallel()

	if href, ok := SafeLinkURL("https://Example.com/path"); !ok || href != "https://example.com/path" {
		t.Fatalf("SafeLinkURL valid = %q/%t", href, ok)
	}
	if href, ok := SafeLinkURL("javascript:alert(1)"); ok || href != "" {
		t.Fatalf("SafeLinkURL invalid = %q/%t", href, ok)
	}
}
