package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReportsPagePrefillsDefaultDateRange(t *testing.T) {
	handler := NewReportHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()

	handler.ReportsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ReportsPage() status = %d, want %d", rec.Code, http.StatusOK)
	}

	startDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")
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
