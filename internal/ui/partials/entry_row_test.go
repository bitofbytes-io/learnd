package partials

import (
	"testing"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/ui"
)

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
