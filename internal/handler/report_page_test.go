package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReportsPagePrefillsDefaultDateRange(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewReportHandler(nil, loc)
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()

	handler.ReportsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ReportsPage() status = %d, want %d", rec.Code, http.StatusOK)
	}

	today := time.Now().In(loc)
	startDate := today.AddDate(0, 0, -30).Format("2006-01-02")
	endDate := today.Format("2006-01-02")
	body := rec.Body.String()

	if !strings.Contains(body, `id="start"`) || !strings.Contains(body, `value="`+startDate+`"`) {
		t.Fatalf("ReportsPage() body missing default start date %q", startDate)
	}

	if !strings.Contains(body, `id="end"`) || !strings.Contains(body, `value="`+endDate+`"`) {
		t.Fatalf("ReportsPage() body missing default end date %q", endDate)
	}

	exportHref := `/api/reports/export?start=` + startDate + `&amp;end=` + endDate
	if !strings.Contains(body, exportHref) {
		t.Fatalf("ReportsPage() body missing default export href %q", exportHref)
	}

	if !strings.Contains(body, `event.target.type === 'date'`) || !strings.Contains(body, `event.target.blur()`) {
		t.Fatalf("ReportsPage() body missing date picker blur behavior")
	}
}

func TestReportsPageBookmarkletUsesRequestOrigin(t *testing.T) {
	handler := NewReportHandler(nil, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "http://learnd.example.com/reports", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	handler.ReportsPage(rec, req)

	// templ HTML-escapes the attribute; the browser decodes it back to
	// javascript:location="https://learnd.example.com/?url="+encodeURIComponent(location.href)
	want := `href="javascript:location=&#34;https://learnd.example.com/?url=&#34;+encodeURIComponent(location.href)"`
	if body := rec.Body.String(); !strings.Contains(body, want) {
		t.Fatalf("ReportsPage() body missing bookmarklet %s", want)
	}
}

func TestBookmarkletURL(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "origin",
			origin: "http://localhost:4500",
			want:   `javascript:location="http://localhost:4500/?url="+encodeURIComponent(location.href)`,
		},
		{
			name:   "no origin",
			origin: "",
			want:   "",
		},
		{
			name:   "quotes and percent stay inside the string",
			origin: `http://[fe80::1%25eth0]"`,
			want:   `javascript:location="http://[fe80::1%2525eth0]\"/?url="+encodeURIComponent(location.href)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bookmarkletURL(tt.origin); got != tt.want {
				t.Fatalf("bookmarkletURL(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}
