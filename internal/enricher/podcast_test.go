package enricher

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPodcastDurationMetadataFallsBackToJSONLD(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    int
	}{
		{"integer", "120", 120},
		{"trimmed integer", " 120 ", 120},
		{"malformed suffix", "120seconds", 180},
		{"fraction", "120.5", 180},
		{"extra token", "120 seconds", 180},
		{"nonpositive", "0", 180},
		{"negative", "-120", 180},
		{"overflow", "999999999999999999999999999999", 180},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(fmt.Sprintf(`<!doctype html><html><head>
				<meta property="music:duration" content="%s">
				<script type="application/ld+json">{"duration":"PT3M"}</script>
				</head></html>`, tt.content)))
			if err != nil {
				t.Fatal(err)
			}
			result := &Result{}
			extractPodcastMetadata(doc, result)
			if result.RuntimeSeconds == nil {
				t.Fatalf("runtime missing, want %d seconds", tt.want)
			}
			if *result.RuntimeSeconds != tt.want {
				t.Fatalf("runtime = %d, want %d seconds", *result.RuntimeSeconds, tt.want)
			}
		})
	}
}
