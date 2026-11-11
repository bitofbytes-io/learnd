package handler

import (
	"testing"

	"github.com/drywaters/learnd/internal/model"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    *string
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid tag",
			input:    "golang",
			expected: strPtr("golang"),
		},
		{
			name:     "uppercase lowercased",
			input:    "GoLang",
			expected: strPtr("golang"),
		},
		{
			name:     "hyphenated tag",
			input:    "machine-learning",
			expected: strPtr("machine-learning"),
		},
		{
			name:        "comma separated rejected",
			input:       "ai, tech",
			expectError: true,
		},
		{
			name:        "space rejected",
			input:       "hello world",
			expectError: true,
		},
		{
			name:     "trimmed whitespace",
			input:    "  ai  ",
			expected: strPtr("ai"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTag(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("parseTag(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTag(%q) unexpected error: %v", tt.input, err)
				return
			}
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseTag(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseTag(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseTag(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseTimeSpentMinutes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid positive minutes",
			input:    "5",
			expected: intPtr(300), // 5 minutes = 300 seconds
		},
		{
			name:     "zero",
			input:    "0",
			expected: nil,
		},
		{
			name:     "negative",
			input:    "-5",
			expected: nil,
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "large value",
			input:    "120",
			expected: intPtr(7200), // 120 minutes = 7200 seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimeSpentMinutes(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseTimeSpentMinutes(%q) = %v, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseTimeSpentMinutes(%q) = nil, want %v", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseTimeSpentMinutes(%q) = %v, want %v", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid positive",
			input:    "5",
			expected: intPtr(5),
		},
		{
			name:     "zero",
			input:    "0",
			expected: nil,
		},
		{
			name:     "negative",
			input:    "-5",
			expected: nil,
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "large value",
			input:    "1000",
			expected: intPtr(1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseQuantity(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseQuantity(%q) = %v, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseQuantity(%q) = nil, want %v", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseQuantity(%q) = %v, want %v", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseOptionalString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "tabs and spaces only",
			input:    " \t \t ",
			expected: nil,
		},
		{
			name:     "newlines only",
			input:    "\n\n",
			expected: nil,
		},
		{
			name:     "valid string",
			input:    "These are my notes",
			expected: strPtr("These are my notes"),
		},
		{
			name:     "string with leading/trailing whitespace",
			input:    "  Some notes here  ",
			expected: strPtr("Some notes here"),
		},
		{
			name:     "string with internal whitespace",
			input:    "Notes with   multiple   spaces",
			expected: strPtr("Notes with   multiple   spaces"),
		},
		{
			name:     "single character",
			input:    "a",
			expected: strPtr("a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOptionalString(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseOptionalString(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseOptionalString(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseOptionalString(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}

func TestParseSourceType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *model.SourceType
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "youtube lowercase",
			input:    "youtube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "youtube uppercase",
			input:    "YOUTUBE",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "youtube mixed case",
			input:    "YouTube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "podcast lowercase",
			input:    "podcast",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "podcast uppercase",
			input:    "PODCAST",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "article lowercase",
			input:    "article",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
		{
			name:     "article uppercase",
			input:    "ARTICLE",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
		{
			name:     "doc lowercase",
			input:    "doc",
			expected: sourceTypePtr(model.SourceTypeDoc),
		},
		{
			name:     "doc uppercase",
			input:    "DOC",
			expected: sourceTypePtr(model.SourceTypeDoc),
		},
		{
			name:     "other lowercase",
			input:    "other",
			expected: sourceTypePtr(model.SourceTypeOther),
		},
		{
			name:     "other uppercase",
			input:    "OTHER",
			expected: sourceTypePtr(model.SourceTypeOther),
		},
		{
			name:     "invalid type",
			input:    "video",
			expected: nil,
		},
		{
			name:     "invalid type blog",
			input:    "blog",
			expected: nil,
		},
		{
			name:     "with leading whitespace",
			input:    "  youtube",
			expected: sourceTypePtr(model.SourceTypeYouTube),
		},
		{
			name:     "with trailing whitespace",
			input:    "podcast  ",
			expected: sourceTypePtr(model.SourceTypePodcast),
		},
		{
			name:     "with surrounding whitespace",
			input:    "  article  ",
			expected: sourceTypePtr(model.SourceTypeArticle),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSourceType(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseSourceType(%q) = %q, want nil", tt.input, *result)
				}
			} else {
				if result == nil {
					t.Errorf("parseSourceType(%q) = nil, want %q", tt.input, *tt.expected)
				} else if *result != *tt.expected {
					t.Errorf("parseSourceType(%q) = %q, want %q", tt.input, *result, *tt.expected)
				}
			}
		})
	}
}
