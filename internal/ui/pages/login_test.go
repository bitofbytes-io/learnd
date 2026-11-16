package pages

import "testing"

func TestLoginPage(t *testing.T) {
	const form = `<form method="POST" action="/login"`
	const keyInput = `<input type="password" id="api_key" name="api_key"`

	tests := []struct {
		name        string
		errorType   string
		redirectURL string
		want        []string
		absent      []string
	}{
		{
			name:   "plain",
			want:   []string{"<title>Sign In - learnd</title>", form, keyInput, `<button type="submit" class="btn-primary w-full">`},
			absent: []string{`name="redirect"`, "Invalid API key", "Please enter your API key", "Invalid request"},
		},
		{
			name:        "carries the redirect target",
			redirectURL: "/reports?start=2025-01-01",
			want:        []string{form, `<input type="hidden" name="redirect" value="/reports?start=2025-01-01">`},
		},
		{
			name:      "invalid key",
			errorType: "invalid_key",
			want:      []string{form, "Invalid API key. Please try again."},
			absent:    []string{"Please enter your API key", "Invalid request"},
		},
		{
			name:      "missing key",
			errorType: "missing_key",
			want:      []string{"Please enter your API key."},
			absent:    []string{"Invalid API key"},
		},
		{
			name:      "invalid request",
			errorType: "invalid_request",
			want:      []string{"Invalid request. Please try again."},
			absent:    []string{"Invalid API key"},
		},
		{
			name:        "escapes the redirect target",
			redirectURL: `/"><script>alert(1)</script>`,
			want:        []string{`<input type="hidden" name="redirect" value="/&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;">`},
			absent:      []string{"<script>alert(1)</script>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRendered(t, renderPage(t, LoginPage(tt.errorType, tt.redirectURL)), tt.want, tt.absent)
		})
	}
}
