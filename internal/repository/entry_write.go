package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/drywaters/learnd/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Create inserts a new entry
func (r *EntryRepository) Create(ctx context.Context, input *model.CreateEntryInput) (*model.Entry, error) {
	query := `
		INSERT INTO entries (source_url, normalized_url, tag, time_spent_seconds, quantity, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + entryColumns

	entry, err := scanEntry(r.pool.QueryRow(ctx, query,
		input.SourceURL,
		input.NormalizedURL,
		input.Tag,
		input.TimeSpentSeconds,
		input.Quantity,
		input.Notes,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create entry: %w", err)
	}

	return entry, nil
}

// Update updates an entry's user-editable fields. Fields the input does not
// Set keep their stored values. Enrichment keeps running after an edit;
// CompleteEnrichment only fills fields that are still empty, so it cannot
// overwrite the user's title, description, or source type. Changing the summary
// text while a summary job is queued or running supersedes that job: it is
// marked ok with the generated-summary provenance cleared, and its claim is
// revoked so a late completion fails with ErrClaimLost. summary_force_refresh
// is left as is so an outstanding refresh stays recoverable (see
// docs/worker-rollout.md).
func (r *EntryRepository) Update(ctx context.Context, id uuid.UUID, input *model.UpdateEntryInput) (*model.Entry, error) {
	// $10 names the columns the input sets.
	set := []string{}
	for _, field := range []struct {
		column string
		isSet  bool
	}{
		{"tag", input.Tag.Set}, {"time_spent_seconds", input.TimeSpentSeconds.Set}, {"quantity", input.Quantity.Set},
		{"notes", input.Notes.Set}, {"title", input.Title.Set}, {"description", input.Description.Set},
		{"summary_text", input.SummaryText.Set},
	} {
		if field.isSet {
			set = append(set, field.column)
		}
	}
	const summarySuperseded = `'summary_text' = ANY($10) AND summary_status IN ('pending', 'processing') AND summary_text IS DISTINCT FROM $8`
	query := fmt.Sprintf(`
		UPDATE entries
		SET tag = CASE WHEN 'tag' = ANY($10) THEN $2 ELSE tag END,
		    time_spent_seconds = CASE WHEN 'time_spent_seconds' = ANY($10) THEN $3 ELSE time_spent_seconds END,
		    quantity = CASE WHEN 'quantity' = ANY($10) THEN $4 ELSE quantity END,
		    notes = CASE WHEN 'notes' = ANY($10) THEN $5 ELSE notes END,
		    title = CASE WHEN 'title' = ANY($10) THEN $6 ELSE title END,
		    description = CASE WHEN 'description' = ANY($10) THEN $7 ELSE description END,
		    summary_text = CASE WHEN 'summary_text' = ANY($10) THEN $8 ELSE summary_text END,
		    source_type = COALESCE($9, source_type),
		    updated_at = NOW(),
		    summary_status = CASE WHEN %[1]s THEN 'ok' ELSE summary_status END,
		    summary_error = CASE WHEN %[1]s THEN NULL ELSE summary_error END,
		    summary_provider = CASE WHEN %[1]s THEN NULL ELSE summary_provider END,
		    summary_model = CASE WHEN %[1]s THEN NULL ELSE summary_model END,
		    summary_version = CASE WHEN %[1]s THEN NULL ELSE summary_version END,
		    summary_generated_at = CASE WHEN %[1]s THEN NULL ELSE summary_generated_at END,
		    summary_claim_token = CASE WHEN %[1]s THEN NULL ELSE summary_claim_token END,
		    summary_lease_expires_at = CASE WHEN %[1]s THEN NULL ELSE summary_lease_expires_at END
		WHERE id = $1
		RETURNING %[2]s`, summarySuperseded, entryColumns)

	entry, err := scanEntry(r.pool.QueryRow(ctx, query, id,
		input.Tag.Value, input.TimeSpentSeconds.Value, input.Quantity.Value, input.Notes.Value,
		input.Title.Value, input.Description.Value, input.SummaryText.Value, input.SourceType, set))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update entry: %w", err)
	}

	return entry, nil
}

// Delete removes an entry
func (r *EntryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM entries WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete entry: %w", err)
	}
	return nil
}

// EnrichmentResult holds the result of URL enrichment
type EnrichmentResult struct {
	CanonicalURL   string
	Domain         string
	SourceType     model.SourceType
	Title          string
	Description    string
	PublishedAt    *time.Time
	RuntimeSeconds *int
	MetadataJSON   []byte
}

// SummaryResult holds the result of summarization
type SummaryResult struct {
	Text        string
	Provider    string
	Model       string
	Version     string
	GeneratedAt time.Time
}

// ResetEnrichment resets enrichment status to pending
func (r *EntryRepository) ResetEnrichment(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE entries
		SET enrichment_status = 'pending', enrichment_error = NULL, enriched_at = NULL,
            enrichment_claim_token = NULL, enrichment_lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to reset enrichment: %w", err)
	}
	return nil
}
