package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/enricher"
	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/summarizer"
	"github.com/drywaters/learnd/internal/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeSummarizer struct {
	call func(context.Context, summarizer.Input) (*summarizer.Result, error)
}

func (s fakeSummarizer) Summarize(ctx context.Context, input summarizer.Input) (*summarizer.Result, error) {
	return s.call(ctx, input)
}
func (fakeSummarizer) Provider() string { return "fake" }
func (fakeSummarizer) Model() string    { return "fake" }
func (fakeSummarizer) Version() string  { return "1" }

type fakeEnricher struct {
	call func(context.Context, string) (*enricher.Result, error)
}

func (e fakeEnricher) Enrich(ctx context.Context, url string) (*enricher.Result, error) {
	return e.call(ctx, url)
}
func (fakeEnricher) CanHandle(string) bool { return true }
func (fakeEnricher) Name() string          { return "fake" }
func (fakeEnricher) Priority() int         { return 1 }

func createWorkerEntry(t *testing.T, pool *pgxpool.Pool, ready bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	status := "pending"
	if ready {
		status = "ok"
	}
	err := pool.QueryRow(context.Background(), `INSERT INTO entries (source_url, normalized_url, title, enrichment_status)
 VALUES ($1, $1, 'Test article', $2) RETURNING id`, "https://example.test/"+uuid.NewString(), status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestConcurrentWorkersCallProviderOncePerEntry(t *testing.T) {
	pool := testdb.New(t)
	repo := repository.NewEntryRepository(pool)
	cache := repository.NewSummaryCacheRepository(pool)
	for range 12 {
		createWorkerEntry(t, pool, true)
	}
	var calls atomic.Int32
	provider := fakeSummarizer{call: func(ctx context.Context, input summarizer.Input) (*summarizer.Result, error) {
		calls.Add(1)
		// Assert claims did not retain a connection while the provider runs.
		if pool.Stat().AcquiredConns() == pool.Stat().MaxConns() {
			t.Error("pool exhausted during provider call")
		}
		return &summarizer.Result{Text: "fresh", Provider: "fake", Model: "fake", Version: "1", GeneratedAt: time.Now()}, nil
	}}
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() { New(repo, cache, nil, provider, Config{}).processSummarization(context.Background()) })
	}
	group.Wait()
	if calls.Load() != 12 {
		t.Fatalf("provider calls = %d, want 12", calls.Load())
	}
	var completed int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM entries WHERE summary_status = 'ok'`).Scan(&completed); err != nil || completed != 12 {
		t.Fatalf("completed %d: %v", completed, err)
	}
}

func TestRefreshDuringProviderCallDiscardsOldResult(t *testing.T) {
	pool := testdb.New(t)
	repo := repository.NewEntryRepository(pool)
	cache := repository.NewSummaryCacheRepository(pool)
	id := createWorkerEntry(t, pool, true)
	started := make(chan struct{})
	release := make(chan struct{})
	provider := fakeSummarizer{call: func(ctx context.Context, input summarizer.Input) (*summarizer.Result, error) {
		close(started)
		<-release
		return &summarizer.Result{Text: "stale", Provider: "fake", Model: "fake", Version: "1", GeneratedAt: time.Now()}, nil
	}}
	oldWorker := New(repo, cache, nil, provider, Config{BatchSize: 1})
	done := make(chan struct{})
	go func() { defer close(done); oldWorker.processSummarization(context.Background()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider never started")
	}
	if err := repo.ResetSummary(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	entry, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SummaryText != nil || entry.SummaryStatus != model.StatusPending {
		t.Fatal("stale worker overwrote refresh")
	}
	// Even a cache inserted by another entry after reset cannot satisfy explicit refresh.
	oldCache := &model.SummaryCache{URLHash: repository.SummaryURLHash(entry.SourceURL), CanonicalURL: entry.SourceURL, SummaryText: "also stale", Provider: "fake", Model: "fake", Version: "1"}
	if err := cache.Store(context.Background(), oldCache); err != nil {
		t.Fatal(err)
	}
	var calls int
	fresh := fakeSummarizer{call: func(ctx context.Context, input summarizer.Input) (*summarizer.Result, error) {
		calls++
		return &summarizer.Result{Text: "fresh", Provider: "fake", Model: "fake", Version: "2", GeneratedAt: time.Now()}, nil
	}}
	New(repo, cache, nil, fresh, Config{BatchSize: 1}).processSummarization(context.Background())
	entry, _ = repo.GetByID(context.Background(), id)
	if calls != 1 || entry.SummaryText == nil || *entry.SummaryText != "fresh" {
		t.Fatal("refresh did not bypass cache")
	}
}

func TestStopCancelsAndReleasesEnrichment(t *testing.T) {
	pool := testdb.New(t)
	repo := repository.NewEntryRepository(pool)
	id := createWorkerEntry(t, pool, false)
	started := make(chan struct{})
	fake := fakeEnricher{call: func(ctx context.Context, url string) (*enricher.Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Minute {
			t.Error("missing processing deadline")
		}
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	worker := New(repo, repository.NewSummaryCacheRepository(pool), enricher.NewRegistry(fake), nil, Config{Interval: time.Millisecond})
	worker.Start(context.Background())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		worker.Stop()
		t.Fatal("enricher never started")
	}
	done := make(chan struct{})
	go func() { worker.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not cancel external work")
	}
	entry, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EnrichmentStatus != model.StatusPending {
		t.Fatalf("status after stop: %s", entry.EnrichmentStatus)
	}
	if claim, err := repo.ClaimJob(context.Background(), repository.EnrichmentJob); err != nil || claim == nil {
		t.Fatalf("released job not claimable: %v", err)
	}
}

func TestProviderFailureRequiresRetryAndOptionalSummaryStaysPending(t *testing.T) {
	pool := testdb.New(t)
	repo := repository.NewEntryRepository(pool)
	cache := repository.NewSummaryCacheRepository(pool)
	id := createWorkerEntry(t, pool, true)
	New(repo, cache, nil, nil, Config{}).processSummarization(context.Background())
	entry, _ := repo.GetByID(context.Background(), id)
	if entry.SummaryStatus != model.StatusPending {
		t.Fatal("missing optional provider changed job")
	}
	var calls int
	fake := fakeSummarizer{call: func(context.Context, summarizer.Input) (*summarizer.Result, error) {
		calls++
		return nil, errors.New("provider failure")
	}}
	worker := New(repo, cache, nil, fake, Config{})
	worker.processSummarization(context.Background())
	worker.processSummarization(context.Background())
	entry, _ = repo.GetByID(context.Background(), id)
	if calls != 1 || entry.SummaryStatus != model.StatusFailed {
		t.Fatalf("calls %d status %s", calls, entry.SummaryStatus)
	}
}

func TestProcessingDeadlineDoesNotAutomaticallyRetry(t *testing.T) {
	for _, kind := range []repository.JobKind{repository.EnrichmentJob, repository.SummaryJob} {
		t.Run(string(kind), func(t *testing.T) {
			pool := testdb.New(t)
			repo := repository.NewEntryRepository(pool)
			id := createWorkerEntry(t, pool, kind == repository.SummaryJob)
			var calls int
			enrich := fakeEnricher{call: func(ctx context.Context, _ string) (*enricher.Result, error) {
				calls++
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			summarize := fakeSummarizer{call: func(ctx context.Context, _ summarizer.Input) (*summarizer.Result, error) {
				calls++
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			worker := New(repo, repository.NewSummaryCacheRepository(pool), enricher.NewRegistry(enrich), summarize, Config{})
			worker.processingTimeout = 50 * time.Millisecond
			process := worker.processEnrichment
			if kind == repository.SummaryJob {
				process = worker.processSummarization
			}
			// A live parent context distinguishes a job deadline from worker shutdown.
			parent := context.Background()
			process(parent)
			process(parent)
			if calls != 1 {
				t.Fatalf("timed-out provider called %d times; want one", calls)
			}
			entry, err := repo.GetByID(parent, id)
			if err != nil {
				t.Fatal(err)
			}
			status, message := entry.EnrichmentStatus, entry.EnrichmentError
			if kind == repository.SummaryJob {
				status, message = entry.SummaryStatus, entry.SummaryError
			}
			if status != model.StatusFailed || message == nil || *message != context.DeadlineExceeded.Error() {
				t.Fatalf("timeout status %s, error %v; want failed with deadline error", status, message)
			}
			claim, err := repo.ClaimJob(parent, kind)
			if err != nil || claim != nil {
				t.Fatalf("timed-out job was claimable: %v, %v", claim, err)
			}
		})
	}
}
