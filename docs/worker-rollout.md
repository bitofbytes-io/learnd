# Worker lease rollout

Migration `008_add_worker_leases.sql` is additive, but old workers do not respect
claims. Do not run old and new workers together during this rollout.

1. Stop all old Learnd replicas and confirm their processes have exited.
2. Apply migrations using the existing Goose deployment workflow.
3. Deploy the new image and restore the desired three replicas.
4. Verify each replica is healthy and pending jobs finish. Legacy `processing`
   rows without a lease become eligible immediately. Other abandoned jobs become
   eligible after their five-minute leases expire.
5. Verify Refresh Summary keeps the previous text visible, invokes the configured
   provider, then replaces the text. Gemini and YouTube remain optional.

Each external operation has a two-minute deadline. Workers claim one entry at a
time, with at most the configured batch size per tick. Database connections are
released before external work. A failed result save leaves the lease available
for later recovery. Explicit provider failures and processing timeouts stay failed until manual retry.
A crash after a provider accepts a request may cause that request to be repeated;
claim tokens prevent a stale response from overwriting a newer claim.

## Rollback and returning to the fixed image

Stop all new workers and confirm their processes have exited before rollback.
Keep migration 008 and its columns in place. In particular, **preserve
`summary_force_refresh`**: the previous binary leaves this column unchanged even
if it marks a summary `ok` using cached text. Status alone cannot identify which
explicit refreshes still need to run.

In a `psql -v ON_ERROR_STOP=1 "$DATABASE_URL"` session, capture the outstanding
refresh IDs to a durable operator-owned file, then release processing jobs. Keep
the CSV until the fixed image has successfully refreshed these entries. The file
is written on the machine running `psql`; use a persistent directory and retain
its path for the return procedure.

```sql
\copy (SELECT id FROM entries WHERE summary_force_refresh ORDER BY id) TO 'learnd-refresh-before-rollback.csv' WITH CSV HEADER

BEGIN;
UPDATE entries
SET enrichment_status = 'pending', enrichment_error = NULL,
    enrichment_claim_token = NULL, enrichment_lease_expires_at = NULL
WHERE enrichment_status = 'processing';
UPDATE entries
SET summary_status = 'pending', summary_error = NULL,
    summary_claim_token = NULL, summary_lease_expires_at = NULL
WHERE summary_status = 'processing';
COMMIT;
```

Start the previous image only after these steps complete. Its summary results may
come from cache; pending forced refreshes remain recorded in the preserved flag
and CSV. The previous image cannot guarantee a fresh summary.

Before starting the fixed image again, stop **all** previous-image workers and
confirm they have exited. In one `psql` session, load the captured IDs and requeue
both those IDs and every currently flagged entry, regardless of current status.
Deleted entries are ignored. Preserve displayed summary text and generated time
until replacement succeeds; leave the force flag true so cache is bypassed.

```sql
CREATE TEMP TABLE rollback_refresh_ids (id UUID PRIMARY KEY);
\copy rollback_refresh_ids (id) FROM 'learnd-refresh-before-rollback.csv' WITH CSV HEADER

BEGIN;
UPDATE entries
SET summary_status = 'pending', summary_error = NULL,
    summary_claim_token = NULL, summary_lease_expires_at = NULL,
    summary_force_refresh = TRUE
WHERE summary_force_refresh
   OR id IN (SELECT id FROM rollback_refresh_ids)
RETURNING id;
COMMIT;

SELECT id, summary_status
FROM entries
WHERE summary_force_refresh
ORDER BY id;
```

Start the fixed image after this transaction commits. It will also reclaim other
legacy processing jobs without leases. Check the flagged entries after processing:

```sql
SELECT id, summary_status, summary_error
FROM entries
WHERE summary_force_refresh
ORDER BY id;
```

Successful fresh results clear the flag atomically with the saved summary and
cache. Investigate remaining failed entries and use Refresh Summary to retry;
when the summarizer is unconfigured, these entries remain pending until enabled.
Retain the CSV until every surviving captured entry has completed a fresh summary.

## PostgreSQL regression tests

Set `LEARND_TEST_DATABASE_URL` to a disposable PostgreSQL database and run
`make test` and `go test -race ./internal/repository ./internal/worker`. Tests use
separate temporary schemas, apply the real migrations, and clean up afterward.
Without this variable, database tests skip; CI always provides PostgreSQL.
Provider tests use local fakes and make no paid API calls.
