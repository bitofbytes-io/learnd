-- +goose Up
ALTER TABLE entries
    ADD COLUMN enrichment_claim_token UUID,
    ADD COLUMN enrichment_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN summary_claim_token UUID,
    ADD COLUMN summary_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN summary_force_refresh BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE entries
    DROP COLUMN enrichment_claim_token,
    DROP COLUMN enrichment_lease_expires_at,
    DROP COLUMN summary_claim_token,
    DROP COLUMN summary_lease_expires_at,
    DROP COLUMN summary_force_refresh;
