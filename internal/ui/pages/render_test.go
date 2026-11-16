package pages

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// renderPage renders a component to a string, failing the test on error.
func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// assertRendered checks that body contains every want and none of the absent
// fragments.
func assertRendered(t *testing.T, body string, want, absent []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %s in: %s", w, body)
		}
	}
	for _, a := range absent {
		if strings.Contains(body, a) {
			t.Errorf("unexpected %s in: %s", a, body)
		}
	}
}
