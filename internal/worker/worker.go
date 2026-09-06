package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/drywaters/learnd/internal/enricher"
	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/summarizer"
)

// Worker processes entries in the background
type Worker struct {
	entryRepo      *repository.EntryRepository
	cacheRepo      *repository.SummaryCacheRepository
	enrichRegistry *enricher.Registry
	summarizer     summarizer.Summarizer

	interval          time.Duration
	batchSize         int
	processingTimeout time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds worker configuration
type Config struct {
	Interval  time.Duration
	BatchSize int
}

// New creates a new background worker
func New(
	entryRepo *repository.EntryRepository,
	cacheRepo *repository.SummaryCacheRepository,
	enrichRegistry *enricher.Registry,
	sum summarizer.Summarizer,
	cfg Config,
) *Worker {
	if cfg.Interval == 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 5
	}

	return &Worker{
		entryRepo:         entryRepo,
		cacheRepo:         cacheRepo,
		enrichRegistry:    enrichRegistry,
		summarizer:        sum,
		interval:          cfg.Interval,
		batchSize:         cfg.BatchSize,
		processingTimeout: jobTimeout,
	}
}

// Start begins the background processing loops
func (w *Worker) Start(ctx context.Context) {
	slog.Info("starting background worker", "interval", w.interval, "batch_size", w.batchSize)

	ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(2)
	go w.runEnrichmentLoop(ctx)
	go w.runSummarizationLoop(ctx)
}

// Stop gracefully stops the worker
func (w *Worker) Stop() {
	slog.Info("stopping background worker")
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("background worker stopped")
}

func (w *Worker) runEnrichmentLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processEnrichment(ctx)
		}
	}
}

func (w *Worker) runSummarizationLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processSummarization(ctx)
		}
	}
}

const jobTimeout = 2 * time.Minute

func (w *Worker) processEnrichment(ctx context.Context) {
	for i := 0; i < w.batchSize && ctx.Err() == nil; i++ {
		claim, err := w.entryRepo.ClaimJob(ctx, repository.EnrichmentJob)
		if err != nil {
			slog.Error("claim enrichment", "error", err)
			return
		}
		if claim == nil {
			return
		}
		w.enrich(ctx, claim)
	}
}

func (w *Worker) enrich(parent context.Context, claim *repository.JobClaim) {
	ctx, cancel := context.WithTimeout(parent, w.processingTimeout)
	defer cancel()
	result, err := w.enrichRegistry.Enrich(ctx, claim.Entry.SourceURL)
	cancel()
	if err != nil {
		w.finishError(parent, repository.EnrichmentJob, claim, err)
		return
	}
	var metadata []byte
	if len(result.Metadata) > 0 {
		metadata, err = json.Marshal(result.Metadata)
		if err != nil {
			slog.Warn("marshal enrichment metadata", "id", claim.Entry.ID, "error", err)
			metadata = nil
		}
	}
	saveCtx, cancelSave := context.WithTimeout(parent, 5*time.Second)
	defer cancelSave()
	err = w.entryRepo.CompleteEnrichment(saveCtx, claim, &repository.EnrichmentResult{
		CanonicalURL: result.CanonicalURL, Domain: result.Domain, SourceType: result.SourceType,
		Title: sanitizeUTF8(result.Title), Description: sanitizeUTF8(result.Description),
		PublishedAt: result.PublishedAt, RuntimeSeconds: result.RuntimeSeconds, MetadataJSON: metadata,
	})
	w.saved(parent, repository.EnrichmentJob, claim, err)
}

func (w *Worker) processSummarization(ctx context.Context) {
	if w.summarizer == nil {
		return
	}
	for i := 0; i < w.batchSize && ctx.Err() == nil; i++ {
		claim, err := w.entryRepo.ClaimJob(ctx, repository.SummaryJob)
		if err != nil {
			slog.Error("claim summary", "error", err)
			return
		}
		if claim == nil {
			return
		}
		w.summarize(ctx, claim)
	}
}

func (w *Worker) summarize(parent context.Context, claim *repository.JobClaim) {
	ctx, cancel := context.WithTimeout(parent, w.processingTimeout)
	defer cancel()
	entry := claim.Entry
	if entry.Title == nil && entry.Description == nil {
		err := w.entryRepo.FinishJob(ctx, repository.SummaryJob, claim, model.StatusSkipped, nil)
		w.saved(parent, repository.SummaryJob, claim, err)
		return
	}
	canonicalURL := entry.SourceURL
	if entry.CanonicalURL != nil {
		canonicalURL = *entry.CanonicalURL
	}
	urlHash := repository.SummaryURLHash(canonicalURL)
	if !claim.ForceRefresh {
		cached, err := w.cacheRepo.GetByURLHash(ctx, urlHash)
		if err != nil {
			w.saved(parent, repository.SummaryJob, claim, err)
			return
		}
		if cached != nil {
			cancel()
			saveCtx, cancelSave := context.WithTimeout(parent, 5*time.Second)
			defer cancelSave()
			err = w.entryRepo.CompleteSummary(saveCtx, claim, &repository.SummaryResult{
				Text: cached.SummaryText, Provider: cached.Provider, Model: cached.Model,
				Version: cached.Version, GeneratedAt: cached.CreatedAt,
			}, nil)
			w.saved(parent, repository.SummaryJob, claim, err)
			return
		}
	}
	input := summarizer.Input{SourceType: entry.SourceType, URL: entry.SourceURL}
	if entry.Title != nil {
		input.Title = *entry.Title
	}
	if entry.Description != nil {
		input.Description = *entry.Description
	}
	if entry.Tag != nil {
		input.Tag = *entry.Tag
	}
	result, err := w.summarizer.Summarize(ctx, input)
	cancel()
	if err != nil {
		w.finishError(parent, repository.SummaryJob, claim, err)
		return
	}
	saveCtx, cancelSave := context.WithTimeout(parent, 5*time.Second)
	defer cancelSave()
	err = w.entryRepo.CompleteSummary(saveCtx, claim, &repository.SummaryResult{
		Text: result.Text, Provider: result.Provider, Model: result.Model, Version: result.Version, GeneratedAt: result.GeneratedAt,
	}, &model.SummaryCache{
		URLHash: urlHash, CanonicalURL: canonicalURL, SummaryText: result.Text,
		Provider: result.Provider, Model: result.Model, Version: result.Version,
	})
	w.saved(parent, repository.SummaryJob, claim, err)
}

// Provider failures and job deadlines require manual retry. Only worker shutdown
// returns work to pending. Cleanup is bounded; lease expiry is the fallback.
func (w *Worker) finishError(parent context.Context, kind repository.JobKind, claim *repository.JobClaim, cause error) {
	status := model.StatusFailed
	message := cause.Error()
	var errorMessage *string = &message
	if parent.Err() != nil {
		status = model.StatusPending
		errorMessage = nil
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.entryRepo.FinishJob(cleanup, kind, claim, status, errorMessage); err != nil && !errors.Is(err, repository.ErrClaimLost) {
		slog.Error("finish job", "kind", kind, "id", claim.Entry.ID, "error", err)
	}
}

func (w *Worker) saved(parent context.Context, kind repository.JobKind, claim *repository.JobClaim, err error) {
	if err == nil {
		slog.Info("completed job", "kind", kind, "id", claim.Entry.ID)
		return
	}
	if errors.Is(err, repository.ErrClaimLost) {
		return
	}
	slog.Error("save job", "kind", kind, "id", claim.Entry.ID, "error", err)
	// Database failures leave the claim leased for recovery. Cancellation makes a
	// best-effort immediate release, avoiding waits during a normal shutdown.
	if parent.Err() != nil {
		w.finishError(parent, kind, claim, err)
	}
}

// sanitizeUTF8 removes invalid UTF-8 byte sequences from a string.
// This prevents PostgreSQL errors when storing text that may contain
// malformed characters from web scraping.
func sanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "")
}
