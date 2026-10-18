-- +goose Up
ALTER TABLE entries ADD COLUMN search tsvector GENERATED ALWAYS AS (
    to_tsvector('english',
        coalesce(title, '') || ' ' || coalesce(notes, '') || ' ' ||
        coalesce(description, '') || ' ' || coalesce(summary_text, ''))
) STORED;
CREATE INDEX idx_entries_search ON entries USING GIN(search);

-- +goose Down
DROP INDEX IF EXISTS idx_entries_search;
ALTER TABLE entries DROP COLUMN IF EXISTS search;
