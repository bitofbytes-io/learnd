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

// PaginationView describes a paginated dashboard state.
type PaginationView struct {
	CurrentPage int
	TotalPages  int
	HasPrevious bool
	HasNext     bool
	PreviousURL string
	NextURL     string
}
