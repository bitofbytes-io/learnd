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

For rollback, first stop all new workers. Set processing enrichment and summary
jobs back to pending and clear their claim tokens and lease expirations, then
start the previous image. Leave the additive columns in place. The previous image
does not implement forced cache bypass, so a pending explicit refresh should be
retried after the fixed image returns.

## PostgreSQL regression tests

Set `LEARND_TEST_DATABASE_URL` to a disposable PostgreSQL database and run
`make test` and `go test -race ./internal/repository ./internal/worker`. Tests use
separate temporary schemas, apply the real migrations, and clean up afterward.
Without this variable, database tests skip; CI always provides PostgreSQL.
Provider tests use local fakes and make no paid API calls.
