# Learnd

Learnd is a self-hosted learning journal for saving articles, videos, podcasts, and other resources. It can enrich YouTube links and optionally generate short summaries with Gemini.

## Requirements

- Docker 24+
- PostgreSQL 15+
- Go matching `go.mod` for local development and CI
- `curl` and `sha256sum` (or macOS `shasum`) for the pinned Tailwind CLI
- Goose for database migrations
- Optional Google API keys for the [Gemini API](https://ai.google.dev/gemini-api/docs/api-key) and [YouTube Data API v3](https://developers.google.com/youtube/v3/getting-started)

## Build the image

```bash
make tail-prod
docker build -t learnd:local .
```

## Configure the application

Generate a strong shared login token:

```bash
openssl rand -base64 32
```

Create an untracked `learnd.env` file:

```dotenv
DATABASE_URL=postgres://learnd:change-me@db:5432/learnd?sslmode=disable
API_TOKEN=replace-with-generated-token
PORT=4500
SECURE_COOKIES=false
LOG_LEVEL=info
GEMINI_API_KEY=
GEMINI_MODEL=gemini-3.1-flash-lite
YOUTUBE_API_KEY=
```

Do not commit this file.

| Setting | Required | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `API_TOKEN` | Yes | Shared login credential |
| `PORT` | No | HTTP port; defaults to `4500` |
| `SECURE_COOKIES` | No | Set `false` for local HTTP; defaults to `true` |
| `LOG_LEVEL` | No | Application log level; defaults to `info` |
| `METRICS_ADDR` | No | Address of the internal Prometheus listener; defaults to `:9464`, and `off` disables it. See [Metrics](#metrics) |
| `APP_TIMEZONE` | No | IANA time zone that decides which calendar day an entry falls on in reports and CSV exports; defaults to `America/New_York`. Time zone data is built into the binary |
| `GEMINI_API_KEY` | No | Enables AI-generated summaries |
| `GEMINI_MODEL` | No | Gemini model name; defaults to `gemini-3.1-flash-lite` |
| `YOUTUBE_API_KEY` | No | Enables YouTube title, description, and duration enrichment |

Secrets support corresponding `*_FILE` variables. The application also checks default files under `/run/secrets/learnd_*` for the database, login token, Gemini key, and YouTube key.

## Database and migrations

```bash
docker network create learnd

docker run -d --name db --network learnd \
  -e POSTGRES_DB=learnd \
  -e POSTGRES_USER=learnd \
  -e POSTGRES_PASSWORD=change-me \
  -p 5432:5432 \
  -v learnd-postgres:/var/lib/postgresql/data \
  postgres:17

until docker exec db pg_isready -U learnd -d learnd >/dev/null 2>&1; do sleep 1; done
```

Learnd embeds the SQL migrations in its binary and applies any pending ones at startup, before the HTTP server and background worker start. It records them in the same `goose_db_version` table the Goose CLI uses, so versions already applied by hand are skipped. If a migration fails, Learnd exits non-zero and never serves traffic, which stops a rolling deploy. Replicas that start together take a PostgreSQL advisory lock, so only one applies migrations while the others wait. No extra configuration is needed.


Startup allows 10 seconds to connect, then 60 seconds total for migrations,
including advisory-lock waiting. The lock is polled once per second for at most
30 seconds; releasing it has a separate 5-second cleanup allowance. Configure
the deployment healthcheck `start_period` to at least **90 seconds** in both
Dined and Learnd before deploying these limits. SIGTERM or SIGINT cancels startup;
a canceled or timed-out migration exits non-zero and logs the reason without
connection details. Transactional migrations roll back the interrupted migration;
previously completed migrations remain applied.

Migrations run as the runtime role from `DATABASE_URL`. It needs schema CREATE
and DDL/ownership rights for the app tables and `goose_db_version`, in addition
to normal application DML privileges. Learnd currently uses PostgreSQL's built-in UUID function and does not install
`pgcrypto`. If an extension is required by a future migration, provision it first
or grant its required privileges; fresh Dined databases require `pgcrypto`.
Do not assume a DML-only runtime role can migrate the database.

Table-rewriting DDL can hold ACCESS EXCLUSIVE locks while old replicas still
serve traffic. Schedule a maintenance window, stop/drain old replicas, apply the
migration explicitly, then start the new image for these changes. In particular,
Learnd migration 009 adds a stored generated search column and can rewrite
`entries`. Keep rollout migrations additive and compatible with the previous
image; use the manual migration path during maintenance for work that exceeds
the startup budget.

You can still inspect or apply migrations with the Goose CLI:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL='postgres://learnd:change-me@localhost:5432/learnd?sslmode=disable'
goose -dir migrations postgres "$DATABASE_URL" status
```

## Run with Docker

```bash
docker run --rm --name learnd --network learnd \
  --env-file learnd.env \
  -p 4500:4500 \
  learnd:local
```

Open <http://localhost:4500> and sign in with the configured `API_TOKEN`. Neither probe needs auth: <http://localhost:4500/health> returns `ok` while the process is up, and <http://localhost:4500/ready> pings the database (2 s timeout, one ping at a time, result reused for 1 s) and returns `{"status":"ok","db":"ok"}`, or HTTP 503 with `{"status":"degraded","db":"error"}` when the database is unreachable.

The optional Google keys are independent: omit the Gemini key to disable summaries, or omit the YouTube key to disable YouTube-specific enrichment. For production, restrict enabled keys to their intended APIs and load them from a secret manager.

## Metrics

Learnd serves Prometheus metrics at `GET /metrics` on a separate internal listener, `METRICS_ADDR` (default `:9464`). The public router on `PORT` never serves `/metrics`, so a public Traefik route cannot expose it. Scrape the listener over the internal overlay network, and do not publish the port. When running Learnd directly on a host, as `make run` does, set `METRICS_ADDR=127.0.0.1:9464` unless Prometheus must scrape it remotely, and restrict remote access to the monitoring network. Set `METRICS_ADDR=off` to disable the listener. If the address cannot be bound, Learnd logs the error and keeps serving. The listener shuts down gracefully with the app.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `http_requests_total`, `http_request_duration_seconds` | `method`, `route`, `code` | Requests and latency on the public router. `route` is the chi route pattern, such as `/entries/{id}/edit`, or `unmatched`. `/health`, `/ready` and `/metrics` are not recorded |
| `pgxpool_*` | none | Database pool stats: acquired, idle, total and max connections, acquire counts and wait time |
| `learnd_login_failures_total` | `reason` | Rejected credentials: `invalid_key`, `missing_key` and `invalid_request` from the login form, `invalid_bearer_token` for API clients, and `invalid_cookie` for stale browser sessions |
| `learnd_jobs_total` | `job`, `result` | Background jobs by `job` (`enrichment` or `summary`) and `result` (`success`, `failure` once the job is marked failed, or `skipped` for a summary with nothing to summarize). Jobs returned to pending at shutdown, discarded after a refresh, or left leased for retry by a database error are not counted |

Go runtime (`go_*`) and process (`process_*`) metrics are also exported.

## Development

```bash
cp local.mk.example local.mk
make run
make test
```

`make templ` runs the exact Templ version from `go.mod`. Commit its generated `*_templ.go` files with source changes; `make check-templ` rejects tracked changes and untracked generated files after regeneration. CI runs both before testing. `make lint` checks formatting of handwritten Go and runs the pinned golangci-lint version from the Makefile with errcheck, staticcheck, govet, ineffassign, and gosec.

`make tail-prod` and `make tail-watch` download the standalone Tailwind v4 release pinned in [`tools/tailwind.env`](tools/tailwind.env). The same wrapper is used in CI and local builds on macOS or glibc Linux (arm64/x86_64). Downloads go to a temporary file, are SHA-256 verified before installation or execution, and are cached under ignored `bin/tools/`; cached binaries are verified on every run. A download or checksum failure stops the build and cleans up the temporary file. No system-wide install is needed.

Renovate discovers both tool versions. A Tailwind update must change the version **and every platform checksum** in `tools/tailwind.env` together, after checking the release’s `sha256sums.txt` against its published asset digests. Version-only updates intentionally fail verification until the matching hashes are reviewed. Keep Tailwind on v4 unless a separate compatibility change is approved, then run `make tail-prod` and smoke-test the UI.

Use `make migrate`, `make migrate-status`, and `make migrate-down` for database maintenance. Set `SECURE_COOKIES=true` when serving the application over production HTTPS.

## Browser security headers

Every public-router response includes `nosniff`, `Referrer-Policy: same-origin`, and clickjacking protection (`X-Frame-Options: DENY` and CSP `frame-ancestors 'none'`). The starter CSP defaults to same-origin resources, blocks objects, and limits forms and base URLs to the application origin. Google Fonts CSS/font hosts are allowed explicitly; htmx and the production Umami proxy remain same-origin. Local HTTP development does not force an HTTPS upgrade.

The existing inline templ scripts (toasts, filters, edit/report controls and bookmarklet setup) require `script-src 'unsafe-inline'`; htmx `hx-on` form-reset handlers require `'unsafe-eval'`. Existing inline styles and htmx indicators require `style-src 'unsafe-inline'`. These are compatibility allowances confined to their respective directives, not permission for arbitrary external script hosts. Moving handlers to external scripts and replacing eval-dependent htmx behavior is a separate prerequisite to tightening them. The bookmarklet opens the normal authenticated capture URL; no auth bypass is added.

## License

Learnd is available under the [MIT License](LICENSE).
