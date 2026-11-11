package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/drywaters/learnd/internal/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedJob(t *testing.T, pool *pgxpool.Pool, ready bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	status := "pending"
	if ready {
		status = "ok"
	}
	err := pool.QueryRow(context.Background(), `INSERT INTO entries (source_url, normalized_url, title, enrichment_status)
 VALUES ('https://example.test/article', $1, 'An article', $2) RETURNING id`, uuid.NewString(), status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestThreeClaimersProcessEachEntryOnce(t *testing.T) {
	for _, kind := range []JobKind{EnrichmentJob, SummaryJob} {
		t.Run(string(kind), func(t *testing.T) {
			pool := testdb.New(t)
			repo := NewEntryRepository(pool)
			for range 15 {
				seedJob(t, pool, kind == SummaryJob)
			}
			claims := make(chan uuid.UUID, 15)
			failures := make(chan error, 3)
			var group sync.WaitGroup
			for range 3 {
				group.Go(func() {
					for {
						claim, err := repo.ClaimJob(context.Background(), kind)
						if err != nil {
							failures <- err
							return
						}
						if claim == nil {
							return
						}
						claims <- claim.Entry.ID
						if err := repo.FinishJob(context.Background(), kind, claim, model.StatusSkipped, nil); err != nil {
							failures <- err
							return
						}
					}
				})
			}
			group.Wait()
			close(claims)
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			seen := map[uuid.UUID]bool{}
			for id := range claims {
				if seen[id] {
					t.Errorf("duplicate claim %s", id)
				}
				seen[id] = true
			}
			if len(seen) != 15 {
				t.Fatalf("claimed %d entries, want 15", len(seen))
			}
		})
	}
}

func TestExpiredAndLegacyLeasesAreReclaimed(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	for _, kind := range []JobKind{EnrichmentJob, SummaryJob} {
		id := seedJob(t, pool, kind == SummaryJob)
		first, err := repo.ClaimJob(ctx, kind)
		if err != nil || first == nil {
			t.Fatalf("claim: %v", err)
		}
		if claim, err := repo.ClaimJob(ctx, kind); err != nil || claim != nil {
			t.Fatalf("active lease was claimed: %v %v", claim, err)
		}
		_, err = pool.Exec(ctx, "UPDATE entries SET "+string(kind)+"_lease_expires_at = NOW() - INTERVAL '1 second' WHERE id = $1", id)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.FinishJob(ctx, kind, first, model.StatusFailed, nil); !errors.Is(err, ErrClaimLost) {
			t.Fatalf("expired completion: %v", err)
		}
		second, err := repo.ClaimJob(ctx, kind)
		if err != nil || second == nil || second.Token == first.Token {
			t.Fatalf("reclaim: %v %v", second, err)
		}
		if err := repo.FinishJob(ctx, kind, first, model.StatusFailed, nil); !errors.Is(err, ErrClaimLost) {
			t.Fatalf("stale completion: %v", err)
		}
		_, err = pool.Exec(ctx, "UPDATE entries SET "+string(kind)+"_lease_expires_at = NULL WHERE id = $1", id)
		if err != nil {
			t.Fatal(err)
		}
		third, err := repo.ClaimJob(ctx, kind)
		if err != nil || third == nil {
			t.Fatalf("legacy reclaim: %v", err)
		}
		if err := repo.FinishJob(ctx, kind, third, model.StatusSkipped, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefreshRevokesOldWorkerAndInvalidatesCache(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	cacheRepo := NewSummaryCacheRepository(pool)
	ctx := context.Background()
	id := seedJob(t, pool, true)
	_, err := pool.Exec(ctx, `UPDATE entries SET summary_text = 'displayed summary' WHERE id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	cached := &model.SummaryCache{URLHash: SummaryURLHash("https://example.test/article"), CanonicalURL: "https://example.test/article", SummaryText: "old cache", Provider: "fake", Model: "fake", Version: "1"}
	if err := cacheRepo.Store(ctx, cached); err != nil {
		t.Fatal(err)
	}
	old, err := repo.ClaimJob(ctx, SummaryJob)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetSummary(ctx, id); err != nil {
		t.Fatal(err)
	}
	if cache, err := cacheRepo.GetByURLHash(ctx, cached.URLHash); err != nil || cache != nil {
		t.Fatalf("cache survived refresh: %v %v", cache, err)
	}
	entry, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SummaryText == nil || *entry.SummaryText != "displayed summary" {
		t.Fatal("refresh erased displayed summary")
	}
	result := &SummaryResult{Text: "new text", Provider: "fake", Model: "fake", Version: "2", GeneratedAt: time.Now()}
	if err := repo.CompleteSummary(ctx, old, result, cached); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("old worker completed: %v", err)
	}
	if cache, _ := cacheRepo.GetByURLHash(ctx, cached.URLHash); cache != nil {
		t.Fatal("old worker rewrote cache")
	}
	fresh, err := repo.ClaimJob(ctx, SummaryJob)
	if err != nil || fresh == nil || !fresh.ForceRefresh {
		t.Fatalf("fresh claim: %v %v", fresh, err)
	}
	cached.SummaryText = result.Text
	if err := repo.CompleteSummary(ctx, fresh, result, cached); err != nil {
		t.Fatal(err)
	}
	var force bool
	if err := pool.QueryRow(ctx, `SELECT summary_force_refresh FROM entries WHERE id=$1`, id).Scan(&force); err != nil || force {
		t.Fatalf("force refresh not cleared: %v", err)
	}
	entry, _ = repo.GetByID(ctx, id)
	if entry.SummaryText == nil || *entry.SummaryText != "new text" {
		t.Fatal("fresh result not saved")
	}
}

func TestSummaryCacheFailureRollsBackEntry(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	id := seedJob(t, pool, true)
	if _, err := pool.Exec(ctx, `ALTER TABLE summary_cache ADD CONSTRAINT reject_test_cache CHECK (provider <> 'reject')`); err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimJob(ctx, SummaryJob)
	if err != nil {
		t.Fatal(err)
	}
	result := &SummaryResult{Text: "new", Provider: "reject", Model: "fake", Version: "1", GeneratedAt: time.Now()}
	cache := &model.SummaryCache{URLHash: "test", CanonicalURL: "test", SummaryText: "new", Provider: "reject", Model: "fake", Version: "1"}
	if err := repo.CompleteSummary(ctx, claim, result, cache); err == nil {
		t.Fatal("expected rejected cache write")
	}
	entry, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SummaryStatus != model.StatusProcessing || entry.SummaryText != nil {
		t.Fatal("failed cache transaction changed entry")
	}
	result.Provider = "fake"
	cache.Provider = "fake"
	if err := repo.CompleteSummary(ctx, claim, result, cache); err != nil {
		t.Fatalf("claim should remain available after rollback: %v", err)
	}
}

func TestRefreshCacheFailureDoesNotRevokeClaim(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	cacheRepo := NewSummaryCacheRepository(pool)
	ctx := context.Background()
	id := seedJob(t, pool, true)
	cached := &model.SummaryCache{URLHash: SummaryURLHash("https://example.test/article"), CanonicalURL: "https://example.test/article", SummaryText: "old", Provider: "fake", Model: "fake", Version: "1"}
	if err := cacheRepo.Store(ctx, cached); err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimJob(ctx, SummaryJob)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_cache_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test cache unavailable'; END $$;
 CREATE TRIGGER reject_cache_delete BEFORE DELETE ON summary_cache FOR EACH ROW EXECUTE FUNCTION reject_cache_delete();`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetSummary(ctx, id); err == nil {
		t.Fatal("expected failed invalidation")
	}
	entry, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SummaryStatus != model.StatusProcessing {
		t.Fatal("failed reset changed status")
	}
	result := &SummaryResult{Text: "completed", Provider: "fake", Model: "fake", Version: "1", GeneratedAt: time.Now()}
	if err := repo.CompleteSummary(ctx, claim, result, nil); err != nil {
		t.Fatalf("failed reset revoked claim: %v", err)
	}
}

// seedFailed inserts an entry with the given statuses and returns its id.
func seedFailed(t *testing.T, pool *pgxpool.Pool, enrichment, summary string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `INSERT INTO entries (source_url, normalized_url, enrichment_status, enrichment_error, summary_status, summary_error)
 VALUES ('https://example.test/article', $1, $2, CASE WHEN $2 = 'failed' THEN 'boom' END, $3, CASE WHEN $3 = 'failed' THEN 'boom' END) RETURNING id`,
		uuid.NewString(), enrichment, summary).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRetryFailedJobsRequeuesOnlyFailedRows(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()

	failedEnrichment := seedFailed(t, pool, "failed", "pending")
	failedSummary := seedFailed(t, pool, "ok", "failed")
	failedBoth := seedFailed(t, pool, "failed", "failed")
	healthy := seedFailed(t, pool, "ok", "ok")
	pending := seedFailed(t, pool, "pending", "pending")
	if _, err := pool.Exec(ctx, `UPDATE entries SET summary_text = 'kept text' WHERE id = $1`, failedSummary); err != nil {
		t.Fatal(err)
	}

	// In-flight jobs hold live leases and must not be requeued or reclaimed.
	inFlightEnrichment := seedFailed(t, pool, "processing", "pending")
	inFlightSummary := seedFailed(t, pool, "ok", "processing")
	if _, err := pool.Exec(ctx, `UPDATE entries SET
 enrichment_claim_token = CASE WHEN enrichment_status = 'processing' THEN gen_random_uuid() END,
 enrichment_lease_expires_at = CASE WHEN enrichment_status = 'processing' THEN NOW() + INTERVAL '5 minutes' END,
 summary_claim_token = CASE WHEN summary_status = 'processing' THEN gen_random_uuid() END,
 summary_lease_expires_at = CASE WHEN summary_status = 'processing' THEN NOW() + INTERVAL '5 minutes' END
 WHERE id IN ($1, $2)`, inFlightEnrichment, inFlightSummary); err != nil {
		t.Fatal(err)
	}
	snapshot := func(id uuid.UUID) string {
		t.Helper()
		var row string
		err := pool.QueryRow(ctx, `SELECT concat_ws('|', enrichment_status, enrichment_claim_token, enrichment_lease_expires_at,
 summary_status, summary_claim_token, summary_lease_expires_at, summary_force_refresh, updated_at) FROM entries WHERE id = $1`, id).Scan(&row)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	inFlight := []uuid.UUID{inFlightEnrichment, inFlightSummary}
	before := []string{snapshot(inFlight[0]), snapshot(inFlight[1])}

	got, err := repo.RetryFailedJobs(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enrichment != 2 || got.Summary != 2 {
		t.Fatalf("requeued %+v, want 2 enrichments and 2 summaries", got)
	}
	for i, id := range inFlight {
		if after := snapshot(id); after != before[i] {
			t.Fatalf("in-flight entry %s changed:\n before %s\n after  %s", id, before[i], after)
		}
	}

	var force bool
	for _, id := range []uuid.UUID{failedEnrichment, failedBoth} {
		entry, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if entry.EnrichmentStatus != model.StatusPending || entry.EnrichmentError != nil {
			t.Fatalf("enrichment of %s = %s %v, want pending without error", id, entry.EnrichmentStatus, entry.EnrichmentError)
		}
	}
	for _, id := range []uuid.UUID{failedSummary, failedBoth} {
		entry, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if entry.SummaryStatus != model.StatusPending || entry.SummaryError != nil {
			t.Fatalf("summary of %s = %s %v, want pending without error", id, entry.SummaryStatus, entry.SummaryError)
		}
		if err := pool.QueryRow(ctx, `SELECT summary_force_refresh FROM entries WHERE id = $1`, id).Scan(&force); err != nil || !force {
			t.Fatalf("summary_force_refresh of %s = %t %v, want true", id, force, err)
		}
	}
	if entry, _ := repo.GetByID(ctx, failedSummary); entry.SummaryText == nil || *entry.SummaryText != "kept text" {
		t.Fatal("requeue erased the displayed summary")
	}
	// Failed rows are claimable again; untouched rows keep their state.
	if entry, _ := repo.GetByID(ctx, healthy); entry.EnrichmentStatus != model.StatusOK || entry.SummaryStatus != model.StatusOK {
		t.Fatalf("healthy entry changed: %s %s", entry.EnrichmentStatus, entry.SummaryStatus)
	}
	if err := pool.QueryRow(ctx, `SELECT summary_force_refresh FROM entries WHERE id = $1`, pending).Scan(&force); err != nil || force {
		t.Fatalf("pending entry force flag = %t %v, want false", force, err)
	}

	// A second call finds nothing failed, so it changes nothing.
	again, err := repo.RetryFailedJobs(ctx, true)
	if err != nil || again != (FailedJobs{}) {
		t.Fatalf("second retry = %+v %v, want nothing requeued", again, err)
	}
	counts, err := repo.CountFailedJobs(ctx)
	if err != nil || counts != (FailedJobs{}) {
		t.Fatalf("CountFailedJobs after retry = %+v %v, want zero", counts, err)
	}
}

func TestRetryFailedJobsSkipsSummariesWhenDisabled(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	id := seedFailed(t, pool, "failed", "failed")

	got, err := repo.RetryFailedJobs(ctx, false)
	if err != nil || got.Enrichment != 1 || got.Summary != 0 {
		t.Fatalf("requeued %+v %v, want one enrichment and no summaries", got, err)
	}
	entry, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EnrichmentStatus != model.StatusPending || entry.SummaryStatus != model.StatusFailed {
		t.Fatalf("statuses = %s %s, want pending and failed", entry.EnrichmentStatus, entry.SummaryStatus)
	}
}

func TestRetryFailedJobsIsRateLimited(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	for range MaxBulkRetry + 5 {
		seedFailed(t, pool, "failed", "ok")
	}

	got, err := repo.RetryFailedJobs(ctx, true)
	if err != nil || got.Enrichment != MaxBulkRetry {
		t.Fatalf("requeued %+v %v, want %d enrichments", got, err, MaxBulkRetry)
	}
	counts, err := repo.CountFailedJobs(ctx)
	if err != nil || counts.Enrichment != 5 || counts.Entries != 5 {
		t.Fatalf("remaining %+v %v, want 5 failed", counts, err)
	}
	// Requeued rows are ordinary pending jobs for the workers.
	claim, err := repo.ClaimJob(ctx, EnrichmentJob)
	if err != nil || claim == nil {
		t.Fatalf("requeued entry not claimable: %v %v", claim, err)
	}
}

func TestRetryFailedJobsLeavesLockedRowsAlone(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	locked := seedFailed(t, pool, "failed", "ok")
	free := seedFailed(t, pool, "failed", "ok")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM entries WHERE id = $1 FOR UPDATE`, locked); err != nil {
		t.Fatal(err)
	}
	got, err := repo.RetryFailedJobs(ctx, true)
	if err != nil || got.Enrichment != 1 {
		t.Fatalf("requeued %+v %v, want only the unlocked row", got, err)
	}
	if entry, _ := repo.GetByID(ctx, free); entry.EnrichmentStatus != model.StatusPending {
		t.Fatalf("unlocked entry status = %s, want pending", entry.EnrichmentStatus)
	}
}

func TestListOptionsFailedFilter(t *testing.T) {
	pool := testdb.New(t)
	repo := NewEntryRepository(pool)
	ctx := context.Background()
	want := map[uuid.UUID]bool{
		seedFailed(t, pool, "failed", "pending"): true,
		seedFailed(t, pool, "ok", "failed"):      true,
	}
	seedFailed(t, pool, "ok", "ok")
	seedFailed(t, pool, "pending", "pending")

	entries, err := repo.List(ctx, ListOptions{Failed: true})
	if err != nil || len(entries) != len(want) {
		t.Fatalf("List(Failed) = %d entries %v, want %d", len(entries), err, len(want))
	}
	for _, entry := range entries {
		if !want[entry.ID] {
			t.Fatalf("List(Failed) returned %s", entry.ID)
		}
	}
	if count, err := repo.CountFiltered(ctx, ListOptions{Failed: true}); err != nil || count != len(want) {
		t.Fatalf("CountFiltered(Failed) = %d %v, want %d", count, err, len(want))
	}
	counts, err := repo.CountFailedJobs(ctx)
	if err != nil || counts != (FailedJobs{Enrichment: 1, Summary: 1, Entries: 2}) {
		t.Fatalf("CountFailedJobs = %+v %v", counts, err)
	}
}

func updateInput(entry *model.Entry) *model.UpdateEntryInput {
	return &model.UpdateEntryInput{
		Tag: model.Some(entry.Tag), TimeSpentSeconds: model.Some(entry.TimeSpentSeconds), Quantity: model.Some(entry.Quantity),
		Notes: model.Some(entry.Notes), Title: model.Some(entry.Title), Description: model.Some(entry.Description),
		SummaryText: model.Some(entry.SummaryText), SourceType: &entry.SourceType,
	}
}

func TestUserEditsSurviveWorkerCompletion(t *testing.T) {
	ctx := context.Background()
	userText := "Edited by user"
	userType := model.SourceTypePodcast
	enrichment := &EnrichmentResult{
		CanonicalURL: "https://example.test/article", Domain: "example.test", SourceType: model.SourceTypeArticle,
		Title: "Worker title", Description: "Worker description",
	}
	summary := &SummaryResult{Text: "Worker summary", Provider: "fake", Model: "fake", Version: "1", GeneratedAt: time.Now()}

	setup := func(t *testing.T, ready bool) (*pgxpool.Pool, *EntryRepository, uuid.UUID) {
		t.Helper()
		pool := testdb.New(t)
		id := seedJob(t, pool, ready)
		if _, err := pool.Exec(ctx, `UPDATE entries SET title = NULL WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return pool, NewEntryRepository(pool), id
	}
	edit := func(t *testing.T, repo *EntryRepository, id uuid.UUID, change func(*model.UpdateEntryInput)) {
		t.Helper()
		entry, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		input := updateInput(entry)
		change(input)
		if _, err := repo.Update(ctx, id, input); err != nil {
			t.Fatal(err)
		}
	}
	editTitleAndType := func(in *model.UpdateEntryInput) { in.Title = model.Some(&userText); in.SourceType = &userType }
	assertEnriched := func(t *testing.T, repo *EntryRepository, id uuid.UUID) {
		t.Helper()
		entry, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if entry.EnrichmentStatus != model.StatusOK || entry.Domain == nil || *entry.Domain != "example.test" {
			t.Fatalf("status=%s domain=%v, want enrichment saved", entry.EnrichmentStatus, entry.Domain)
		}
		if entry.Title == nil || *entry.Title != userText || entry.SourceType != userType {
			t.Fatalf("title=%v type=%s, want user edits kept", entry.Title, entry.SourceType)
		}
		if entry.Description == nil || *entry.Description != "Worker description" {
			t.Fatalf("description=%v, want unedited field filled", entry.Description)
		}
	}

	t.Run("edit while enrichment pending", func(t *testing.T) {
		_, repo, id := setup(t, false)
		edit(t, repo, id, editTitleAndType)
		claim, err := repo.ClaimJob(ctx, EnrichmentJob)
		if err != nil || claim == nil {
			t.Fatalf("edit stopped enrichment: %v %v", claim, err)
		}
		if err := repo.CompleteEnrichment(ctx, claim, enrichment); err != nil {
			t.Fatal(err)
		}
		assertEnriched(t, repo, id)
	})

	t.Run("edit while enrichment processing", func(t *testing.T) {
		_, repo, id := setup(t, false)
		claim, err := repo.ClaimJob(ctx, EnrichmentJob)
		if err != nil || claim == nil {
			t.Fatalf("claim: %v %v", claim, err)
		}
		edit(t, repo, id, editTitleAndType)
		if err := repo.CompleteEnrichment(ctx, claim, enrichment); err != nil {
			t.Fatal(err)
		}
		assertEnriched(t, repo, id)
	})

	t.Run("clearing fields while enrichment processing", func(t *testing.T) {
		pool, repo, id := setup(t, false)
		_, err := pool.Exec(ctx, `UPDATE entries SET title = 'Old title', description = 'Old description', source_type = 'youtube' WHERE id = $1`, id)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := repo.ClaimJob(ctx, EnrichmentJob)
		if err != nil || claim == nil {
			t.Fatalf("claim: %v %v", claim, err)
		}
		other := model.SourceTypeOther
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.Title = model.Some[*string](nil); in.SourceType = &other })
		if err := repo.CompleteEnrichment(ctx, claim, enrichment); err != nil {
			t.Fatal(err)
		}
		entry, _ := repo.GetByID(ctx, id)
		if entry.Title != nil || entry.SourceType != model.SourceTypeOther {
			t.Fatalf("title=%v type=%s, want cleared title and explicit other kept", entry.Title, entry.SourceType)
		}
		if entry.Description == nil || *entry.Description != "Old description" {
			t.Fatalf("description=%v, want existing value kept", entry.Description)
		}
		if entry.Domain == nil || *entry.Domain != "example.test" {
			t.Fatalf("domain=%v, want enrichment saved", entry.Domain)
		}
	})

	t.Run("summary edit supersedes refresh", func(t *testing.T) {
		pool, repo, id := setup(t, true)
		_, err := pool.Exec(ctx, `UPDATE entries SET title = 'An article', summary_text = 'generated', summary_status = 'ok',
 summary_provider = 'gemini', summary_model = 'm', summary_version = 'v', summary_generated_at = NOW() WHERE id = $1`, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.ResetSummary(ctx, id); err != nil {
			t.Fatal(err)
		}
		claim, err := repo.ClaimJob(ctx, SummaryJob)
		if err != nil || claim == nil {
			t.Fatalf("claim: %v %v", claim, err)
		}
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.SummaryText = model.Some(&userText) })
		if err := repo.CompleteSummary(ctx, claim, summary, nil); !errors.Is(err, ErrClaimLost) {
			t.Fatalf("completion after edit: %v", err)
		}
		entry, _ := repo.GetByID(ctx, id)
		if entry.SummaryText == nil || *entry.SummaryText != userText || entry.SummaryStatus != model.StatusOK {
			t.Fatalf("summary=%v status=%s, want user summary and ok", entry.SummaryText, entry.SummaryStatus)
		}
		if entry.SummaryProvider != nil || entry.SummaryModel != nil || entry.SummaryVersion != nil || entry.SummaryGeneratedAt != nil {
			t.Fatal("stale generated-summary provenance kept on user summary")
		}
		var force bool
		if err := pool.QueryRow(ctx, `SELECT summary_force_refresh FROM entries WHERE id = $1`, id).Scan(&force); err != nil || !force {
			t.Fatalf("force refresh marker lost: %t %v", force, err)
		}
	})

	t.Run("unrelated edit keeps summary claim", func(t *testing.T) {
		pool, repo, id := setup(t, true)
		if _, err := pool.Exec(ctx, `UPDATE entries SET title = 'An article' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		claim, err := repo.ClaimJob(ctx, SummaryJob)
		if err != nil || claim == nil {
			t.Fatalf("claim: %v %v", claim, err)
		}
		notes := "just notes"
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.Notes = model.Some(&notes) })
		if err := repo.CompleteSummary(ctx, claim, summary, nil); err != nil {
			t.Fatalf("notes-only edit revoked claim: %v", err)
		}
		entry, _ := repo.GetByID(ctx, id)
		if entry.SummaryText == nil || *entry.SummaryText != summary.Text || entry.Notes == nil || *entry.Notes != notes {
			t.Fatalf("summary=%v notes=%v, want worker summary and user notes", entry.SummaryText, entry.Notes)
		}
	})
}
