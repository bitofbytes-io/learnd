package enricher

import (
	"context"
	"sort"
	"time"

	"github.com/drywaters/learnd/internal/model"
)

// Result contains extracted metadata from a URL
type Result struct {
	CanonicalURL   string
	Domain         string
	SourceType     model.SourceType
	Title          string
	Description    string
	PublishedAt    *time.Time
	RuntimeSeconds *int
	Metadata       map[string]interface{}
}

// Enricher extracts metadata from URLs
type Enricher interface {
	// CanHandle returns true if this enricher can process the URL
	CanHandle(url string) bool

	// Enrich extracts metadata from the URL
	Enrich(ctx context.Context, url string) (*Result, error)

	// Priority returns the enricher priority (lower = higher priority)
	Priority() int
}

// Registry manages enrichers and routes URLs to appropriate handlers
type Registry struct {
	enrichers []Enricher
	fallback  Enricher
}

// NewRegistry creates a new enricher registry with a fallback enricher.
// The fallback enricher is used when no registered enricher can handle a URL.
// Panics if fallback is nil.
func NewRegistry(fallback Enricher) *Registry {
	if fallback == nil {
		panic("fallback enricher must not be nil")
	}
	return &Registry{
		enrichers: make([]Enricher, 0),
		fallback:  fallback,
	}
}

// Register adds an enricher to the registry
func (r *Registry) Register(e Enricher) {
	r.enrichers = append(r.enrichers, e)
	// Sort by priority (lower = higher priority)
	sort.Slice(r.enrichers, func(i, j int) bool {
		return r.enrichers[i].Priority() < r.enrichers[j].Priority()
	})
}

// Enrich processes a URL using the appropriate enricher.
// The first enricher that can handle the URL is authoritative - if it fails,
// the error is returned rather than falling back to a generic enricher.
func (r *Registry) Enrich(ctx context.Context, url string) (*Result, error) {
	for _, e := range r.enrichers {
		if e.CanHandle(url) {
			return e.Enrich(ctx, url)
		}
	}
	return r.fallback.Enrich(ctx, url)
}
