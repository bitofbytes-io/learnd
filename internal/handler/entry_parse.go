package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/drywaters/learnd/internal/model"
)

// formOptional parses a form field only when the request sent it.
func formOptional[T any](r *http.Request, name string, parse func(string) T) model.Optional[T] {
	values, ok := r.Form[name]
	if !ok || len(values) == 0 {
		return model.Optional[T]{}
	}
	return model.Some(parse(values[0]))
}

var tagRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// parseTag trims whitespace, lowercases the input, and validates it as a single tag.
// Returns nil if empty, or an error if the tag contains invalid characters.
func parseTag(tagStr string) (*string, error) {
	tag := strings.ToLower(strings.TrimSpace(tagStr))
	if tag == "" {
		return nil, nil
	}
	if !tagRegex.MatchString(tag) {
		return nil, fmt.Errorf("invalid tag: only lowercase letters, numbers, and hyphens are allowed")
	}
	return &tag, nil
}

// parseTimeSpentMinutes parses a time spent value in minutes and returns seconds.
// Returns nil if the input is empty, not a valid integer, or not positive.
func parseTimeSpentMinutes(ts string) *int {
	if ts == "" {
		return nil
	}
	if v, err := strconv.Atoi(ts); err == nil && v > 0 {
		seconds := v * 60
		return &seconds
	}
	return nil
}

// parseQuantity parses a quantity value and returns a pointer to the integer.
// Returns nil if the input is empty, not a valid integer, or not positive.
func parseQuantity(q string) *int {
	if q == "" {
		return nil
	}
	if v, err := strconv.Atoi(q); err == nil && v > 0 {
		return &v
	}
	return nil
}

// parseOptionalString trims a string and returns a pointer if non-empty.
// Returns nil if the trimmed result is empty.
func parseOptionalString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// parseSourceType parses a source type string and returns a pointer.
// Returns nil if the input is empty or not a valid source type.
func parseSourceType(s string) *model.SourceType {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil
	}
	var st model.SourceType
	switch s {
	case "youtube":
		st = model.SourceTypeYouTube
	case "podcast":
		st = model.SourceTypePodcast
	case "article":
		st = model.SourceTypeArticle
	case "doc":
		st = model.SourceTypeDoc
	case "other":
		st = model.SourceTypeOther
	default:
		return nil
	}
	return &st
}
