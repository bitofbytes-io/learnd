package ui

import (
	"github.com/drywaters/learnd/internal/model"
)

// EntryView decorates an entry with UI-only fields.
type EntryView struct {
	model.Entry
	DuplicateCount int
	SwapOOB        bool
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
