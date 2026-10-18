package ui

import (
	"github.com/drywaters/learnd/internal/model"
)

// EntryView decorates an entry with UI-only fields.
type EntryView struct {
	model.Entry
	DuplicateCount int
	SwapOOB        bool
	EditURL        string
	SourceHref     string
	HasSourceHref  bool
	// SummaryEnabled reports whether a summarizer is configured; without one,
	// pending summaries never progress and must not keep the row polling.
	SummaryEnabled bool
}

// DashboardFilter describes the dashboard's search box and filter selects.
// Empty fields do not filter.
type DashboardFilter struct {
	Query      string
	Tag        string
	SourceType string
	// Tags lists the options offered by the tag select.
	Tags []string
}

// Active reports whether any filter narrows the entry list.
func (f DashboardFilter) Active() bool {
	return f.Query != "" || f.Tag != "" || f.SourceType != ""
}

// PaginationView describes a paginated dashboard state.
type PaginationView struct {
	CurrentPage int
	TotalPages  int
	HasPrevious bool
	HasNext     bool
	PreviousURL string
	NextURL     string
}
