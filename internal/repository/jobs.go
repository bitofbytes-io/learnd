package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const JobLease = 5 * time.Minute

// ErrClaimLost means a refreshed or reclaimed job must discard its old result.
var ErrClaimLost = errors.New("job claim is no longer current")

type JobKind string

const (
	EnrichmentJob JobKind = "enrichment"
	SummaryJob    JobKind = "summary"
)

type JobClaim struct {
	Entry        model.Entry
	Token        uuid.UUID
	ForceRefresh bool
}

// ClaimJob locks and claims one eligible entry in a single statement. No connection
// remains checked out after the returned snapshot has been scanned.
func (r *EntryRepository) ClaimJob(ctx context.Context, kind JobKind) (*JobClaim, error) {
	if kind != EnrichmentJob && kind != SummaryJob {
		return nil, fmt.Errorf("invalid job kind %q", kind)
	}
	prerequisite := ""
	if kind == SummaryJob {
		prerequisite = "AND enrichment_status = 'ok'"
	}
	query := fmt.Sprintf(`
 WITH candidate AS (
   SELECT id FROM entries
   WHERE (%[1]s_status = 'pending' OR (%[1]s_status = 'processing'
     AND (%[1]s_lease_expires_at IS NULL OR %[1]s_lease_expires_at <= NOW()))) %[2]s
   ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
 )
 UPDATE entries e SET %[1]s_status = 'processing', %[1]s_error = NULL,
   %[1]s_claim_token = $1, %[1]s_lease_expires_at = NOW() + $2 * INTERVAL '1 second'
 FROM candidate c WHERE e.id = c.id
 RETURNING e.id, e.source_url, e.canonical_url, e.title, e.description, e.source_type, e.tag, e.summary_force_refresh`, kind, prerequisite)
	claim := &JobClaim{Token: uuid.New()}
	err := r.pool.QueryRow(ctx, query, claim.Token, JobLease.Seconds()).Scan(
		&claim.Entry.ID, &claim.Entry.SourceURL, &claim.Entry.CanonicalURL, &claim.Entry.Title,
		&claim.Entry.Description, &claim.Entry.SourceType, &claim.Entry.Tag, &claim.ForceRefresh)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim %s: %w", kind, err)
	}
	return claim, nil
}

// FinishJob records failure, skip, or cancellation only for the current lease.
func (r *EntryRepository) FinishJob(ctx context.Context, kind JobKind, claim *JobClaim, status model.ProcessingStatus, message *string) error {
	if kind != EnrichmentJob && kind != SummaryJob {
		return fmt.Errorf("invalid job kind %q", kind)
	}
	if status != model.StatusPending && status != model.StatusFailed && status != model.StatusSkipped {
		return fmt.Errorf("invalid terminal status %q", status)
	}
	timestampUpdate := ""
	if kind == EnrichmentJob {
		timestampUpdate = ", enriched_at = CASE WHEN $3 = 'failed' THEN NOW() ELSE enriched_at END"
	}
	query := fmt.Sprintf(`UPDATE entries SET %[1]s_status = $3, %[1]s_error = $4,
 %[1]s_claim_token = NULL, %[1]s_lease_expires_at = NULL %[2]s
 WHERE id = $1 AND %[1]s_status = 'processing' AND %[1]s_claim_token = $2 AND %[1]s_lease_expires_at > clock_timestamp()`, kind, timestampUpdate)
	result, err := r.pool.Exec(ctx, query, claim.Entry.ID, claim.Token, status, message)
	if err != nil {
		return fmt.Errorf("finish %s: %w", kind, err)
	}
	if result.RowsAffected() == 0 {
		return ErrClaimLost
	}
	return nil
}

func (r *EntryRepository) CompleteEnrichment(ctx context.Context, claim *JobClaim, result *EnrichmentResult) error {
	command, err := r.pool.Exec(ctx, `UPDATE entries
 SET canonical_url = $3, domain = $4, source_type = $5, title = $6, description = $7,
 published_at = $8, runtime_seconds = $9, metadata_json = $10,
 enrichment_status = 'ok', enrichment_error = NULL, enriched_at = NOW(),
 enrichment_claim_token = NULL, enrichment_lease_expires_at = NULL
 WHERE id = $1 AND enrichment_status = 'processing' AND enrichment_claim_token = $2
 AND enrichment_lease_expires_at > clock_timestamp()`, claim.Entry.ID, claim.Token,
		result.CanonicalURL, result.Domain, result.SourceType, result.Title, result.Description,
		result.PublishedAt, result.RuntimeSeconds, result.MetadataJSON)
	if err != nil {
		return fmt.Errorf("complete enrichment: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrClaimLost
	}
	return nil
}

// CompleteSummary commits the entry and optional cache atomically. Lock ordering
// matches ResetSummary: entry first, cache second. Losing ownership writes neither.
func (r *EntryRepository) CompleteSummary(ctx context.Context, claim *JobClaim, result *SummaryResult, cache *model.SummaryCache) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	command, err := tx.Exec(ctx, `UPDATE entries SET summary_text = $3, summary_provider = $4,
 summary_model = $5, summary_version = $6, summary_generated_at = $7,
 summary_status = 'ok', summary_error = NULL, summary_force_refresh = FALSE,
 summary_claim_token = NULL, summary_lease_expires_at = NULL
 WHERE id = $1 AND summary_status = 'processing' AND summary_claim_token = $2
 AND summary_lease_expires_at > clock_timestamp()`, claim.Entry.ID, claim.Token,
		result.Text, result.Provider, result.Model, result.Version, result.GeneratedAt)
	if err != nil {
		return fmt.Errorf("complete summary: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrClaimLost
	}
	if cache != nil {
		_, err = tx.Exec(ctx, storeSummaryCacheSQL, cache.URLHash, cache.CanonicalURL, cache.SummaryText, cache.Provider, cache.Model, cache.Version)
		if err != nil {
			return fmt.Errorf("cache summary: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func SummaryURLHash(canonicalURL string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalURL)))
}

// ResetSummary preserves the displayed summary while revoking current work and
// forcing the next attempt to obtain fresh content from the configured provider.
func (r *EntryRepository) ResetSummary(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var canonicalURL string
	err = tx.QueryRow(ctx, `UPDATE entries SET summary_status = 'pending', summary_error = NULL,
 summary_force_refresh = TRUE, summary_claim_token = NULL, summary_lease_expires_at = NULL
 WHERE id = $1 RETURNING COALESCE(canonical_url, source_url)`, id).Scan(&canonicalURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reset summary: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM summary_cache WHERE url_hash = $1`, SummaryURLHash(canonicalURL)); err != nil {
		return fmt.Errorf("invalidate summary cache: %w", err)
	}
	return tx.Commit(ctx)
}
