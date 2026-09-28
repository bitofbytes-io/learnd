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

func updateInput(entry *model.Entry) *model.UpdateEntryInput {
	return &model.UpdateEntryInput{
		Tag: entry.Tag, TimeSpentSeconds: entry.TimeSpentSeconds, Quantity: entry.Quantity, Notes: entry.Notes,
		Title: entry.Title, Description: entry.Description, SummaryText: entry.SummaryText, SourceType: &entry.SourceType,
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
	editTitleAndType := func(in *model.UpdateEntryInput) { in.Title = &userText; in.SourceType = &userType }
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
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.Title = nil; in.SourceType = &other })
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
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.SummaryText = &userText })
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
		edit(t, repo, id, func(in *model.UpdateEntryInput) { in.Notes = &notes })
		if err := repo.CompleteSummary(ctx, claim, summary, nil); err != nil {
			t.Fatalf("notes-only edit revoked claim: %v", err)
		}
		entry, _ := repo.GetByID(ctx, id)
		if entry.SummaryText == nil || *entry.SummaryText != summary.Text || entry.Notes == nil || *entry.Notes != notes {
			t.Fatalf("summary=%v notes=%v, want worker summary and user notes", entry.SummaryText, entry.Notes)
		}
	})
}
