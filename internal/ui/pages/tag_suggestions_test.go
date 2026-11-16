package pages

import (
	"strings"
	"testing"
)

func TestTagSuggestions(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		want    []string
		options int
	}{
		{"none", nil, []string{`<datalist id="tag-suggestions"></datalist>`}, 0},
		{
			"existing tags",
			[]string{"ai", "go-lang"},
			[]string{`<datalist id="tag-suggestions">`, `<option value="ai"></option>`, `<option value="go-lang"></option>`},
			2,
		},
		{"escapes values", []string{`a"b`}, []string{`<option value="a&#34;b"></option>`}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderPage(t, tagSuggestions(tt.tags))
			assertRendered(t, body, tt.want, nil)
			if got := strings.Count(body, "<option"); got != tt.options {
				t.Errorf("rendered %d options, want %d: %s", got, tt.options, body)
			}
		})
	}
}
