package partials

import (
	"context"
	"strings"
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
	"github.com/google/uuid"
)

func TestEntryRowPollsStatusOnlyWhileWorkIsPending(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	render := func(enrichment model.ProcessingStatus) string {
		t.Helper()
		var b strings.Builder
		view := ui.EntryView{Entry: model.Entry{ID: id, SourceURL: "https://example.test", EnrichmentStatus: enrichment, SummaryStatus: model.StatusOK}}
		if err := EntryRow(view).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	poll := []string{`hx-get="/entries/` + id.String() + `/status"`, `hx-trigger="every 5s"`, `hx-swap="outerHTML"`}

	pending := render(model.StatusPending)
	for _, want := range poll {
		if !strings.Contains(pending, want) {
			t.Fatalf("pending row missing %s: %s", want, pending)
		}
	}
	if done := render(model.StatusOK); strings.Contains(done, poll[1]) {
		t.Fatalf("finished row still polls: %s", done)
	}
}

func TestNeedsPolling(t *testing.T) {
	tests := []struct {
		name           string
		enrichment     model.ProcessingStatus
		summary        model.ProcessingStatus
		summaryEnabled bool
		want           bool
	}{
		{"enrichment pending", model.StatusPending, model.StatusPending, false, true},
		{"enrichment processing", model.StatusProcessing, model.StatusPending, false, true},
		{"summary pending with summarizer", model.StatusOK, model.StatusPending, true, true},
		{"summary processing with summarizer", model.StatusOK, model.StatusProcessing, true, true},
		{"summary pending without summarizer", model.StatusOK, model.StatusPending, false, false},
		{"summary pending after failed enrichment", model.StatusFailed, model.StatusPending, true, false},
		{"summary pending after skipped enrichment", model.StatusSkipped, model.StatusPending, true, false},
		{"all done", model.StatusOK, model.StatusOK, true, false},
		{"summary failed", model.StatusOK, model.StatusFailed, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ui.EntryView{
				Entry:          model.Entry{EnrichmentStatus: tt.enrichment, SummaryStatus: tt.summary},
				SummaryEnabled: tt.summaryEnabled,
			}
			if got := needsPolling(entry); got != tt.want {
				t.Fatalf("needsPolling() = %t, want %t", got, tt.want)
			}
		})
	}
}
