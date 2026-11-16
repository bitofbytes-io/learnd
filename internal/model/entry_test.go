package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The string values are persisted in PostgreSQL and rendered in the UI, so a
// rename is a data migration, not a refactor.
func TestSourceTypeValues(t *testing.T) {
	tests := []struct {
		got  SourceType
		want string
	}{
		{SourceTypeYouTube, "youtube"},
		{SourceTypePodcast, "podcast"},
		{SourceTypeArticle, "article"},
		{SourceTypeDoc, "doc"},
		{SourceTypeOther, "other"},
	}

	seen := make(map[SourceType]bool, len(tests))
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Fatalf("SourceType = %q, want %q", tt.got, tt.want)
			}
			if seen[tt.got] {
				t.Fatalf("SourceType %q is declared twice", tt.got)
			}
			seen[tt.got] = true
		})
	}
}

func TestProcessingStatusValues(t *testing.T) {
	tests := []struct {
		got  ProcessingStatus
		want string
	}{
		{StatusPending, "pending"},
		{StatusProcessing, "processing"},
		{StatusOK, "ok"},
		{StatusFailed, "failed"},
		{StatusSkipped, "skipped"},
	}

	seen := make(map[ProcessingStatus]bool, len(tests))
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Fatalf("ProcessingStatus = %q, want %q", tt.got, tt.want)
			}
			if seen[tt.got] {
				t.Fatalf("ProcessingStatus %q is declared twice", tt.got)
			}
			seen[tt.got] = true
		})
	}
}

func TestSomeMarksValueAsSet(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		got := Some(42)
		if !got.Set || got.Value != 42 {
			t.Fatalf("Some(42) = %+v, want Set with Value 42", got)
		}
	})

	t.Run("pointer", func(t *testing.T) {
		title := "Go"
		got := Some(&title)
		if !got.Set || got.Value != &title {
			t.Fatalf("Some(&title) = %+v, want Set with the same pointer", got)
		}
	})

	t.Run("nil clears", func(t *testing.T) {
		got := Some[*string](nil)
		if !got.Set || got.Value != nil {
			t.Fatalf("Some[*string](nil) = %+v, want Set with nil Value", got)
		}
	})

	t.Run("zero value leaves unchanged", func(t *testing.T) {
		var got Optional[*string]
		if got.Set || got.Value != nil {
			t.Fatalf("zero Optional = %+v, want unset", got)
		}
	})
}

func TestUpdateEntryInputZeroValueChangesNothing(t *testing.T) {
	var in UpdateEntryInput
	strs := map[string]Optional[*string]{
		"Tag": in.Tag, "Notes": in.Notes, "Title": in.Title,
		"Description": in.Description, "SummaryText": in.SummaryText,
	}
	for name, o := range strs {
		if o.Set {
			t.Errorf("%s is Set in zero UpdateEntryInput", name)
		}
	}
	ints := map[string]Optional[*int]{
		"TimeSpentSeconds": in.TimeSpentSeconds, "Quantity": in.Quantity,
	}
	for name, o := range ints {
		if o.Set {
			t.Errorf("%s is Set in zero UpdateEntryInput", name)
		}
	}
	if in.SourceType != nil {
		t.Errorf("SourceType = %v, want nil", in.SourceType)
	}
}

func TestEntryJSONShape(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("minimal entry omits optional fields", func(t *testing.T) {
		data, err := json.Marshal(Entry{
			ID:               id,
			CreatedAt:        created,
			UpdatedAt:        created,
			SourceURL:        "https://example.com/a",
			NormalizedURL:    "https://example.com/a",
			SourceType:       SourceTypeArticle,
			EnrichmentStatus: StatusPending,
			SummaryStatus:    StatusSkipped,
		})
		if err != nil {
			t.Fatal(err)
		}

		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}

		want := map[string]any{
			"id":                "11111111-2222-3333-4444-555555555555",
			"created_at":        "2026-01-02T03:04:05Z",
			"updated_at":        "2026-01-02T03:04:05Z",
			"source_url":        "https://example.com/a",
			"normalized_url":    "https://example.com/a",
			"tag":               nil, // not omitempty
			"source_type":       "article",
			"enrichment_status": "pending",
			"summary_status":    "skipped",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Entry JSON = %v, want %v", got, want)
		}
	})

	t.Run("populated optional fields round trip", func(t *testing.T) {
		tag, title := "go", "A title"
		secs := 90
		in := Entry{
			ID:               id,
			CreatedAt:        created,
			UpdatedAt:        created,
			Tag:              &tag,
			TimeSpentSeconds: &secs,
			Title:            &title,
			SourceType:       SourceTypeYouTube,
			EnrichmentStatus: StatusOK,
			SummaryStatus:    StatusFailed,
		}

		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var keys map[string]any
		if err := json.Unmarshal(data, &keys); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]any{"tag": "go", "title": "A title", "time_spent_seconds": float64(90)} {
			if got := keys[key]; got != want {
				t.Errorf("Entry JSON %q = %v, want %v", key, got, want)
			}
		}

		var got Entry
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("round trip = %+v, want %+v", got, in)
		}
	})
}

func TestSummaryCacheJSONKeys(t *testing.T) {
	data, err := json.Marshal(SummaryCache{
		URLHash:      "h",
		CanonicalURL: "https://example.com",
		SummaryText:  "s",
		Provider:     "p",
		Model:        "m",
		Version:      "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	keys := []string{"id", "url_hash", "canonical_url", "summary_text", "provider", "model", "version", "created_at"}
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Errorf("SummaryCache JSON missing key %q", key)
		}
	}
}
