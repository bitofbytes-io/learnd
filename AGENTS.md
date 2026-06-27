# Repository Guidelines

## Project Structure & Module Organization
- `cmd/learnd/main.go` is the application entrypoint.
- `internal/` holds core packages (config, server, handlers, middleware, repository, summarizer, enricher, worker).
- `internal/ui/` contains `templ` UI sources (`*.templ`) and generated Go (`*_templ.go`); edit `.templ` files and regenerate with `templ generate`.
- `static/` holds compiled assets (e.g., `static/styles.css`).
- `tailwind/` contains source CSS for Tailwind; do not edit generated CSS directly.
- `migrations/` contains Goose SQL migrations (e.g., `001_create_entries.sql`).
- `scripts/` contains helper utilities such as `auth-capture.js` for saving Playwright `storageState` after manual login.

## Build, Test, and Development Commands
- `make templ`: generate Go code from `templ` files.
- `make templ-watch`: watch `templ` files and regenerate on change.
- `make run`: generate `templ`, build Tailwind CSS, and run the app via `go run ./cmd/learnd`.
- `make dev`: run the live-reload development server with `air` (watches `.go`, `.templ`, and `.css` files).
- `make build`: generate `templ`, build Tailwind CSS, and build a production binary at `bin/learnd`.
- `make tail-watch`: run Tailwind in watch mode (requires the `tailwindcss` CLI).
- `make tail-prod`: build minified Tailwind output into `static/styles.css`.
- `make migrate`, `make migrate-down`, `make migrate-status`: apply, rollback, or inspect database migrations using `goose`.
- `make test`: run Go tests (`go test -v ./...`).
- `make docker-buildx`: build and push the multi-arch Docker image; set `REGISTRY`, `IMAGE_REPO`, `PLATFORMS`, and `TAG` (and optionally `VERSION`, `REVISION`, and `SOURCE_URL` for CI parity).

## Coding Style & Naming Conventions
- Go code should be formatted with `gofmt`; keep packages cohesive under `internal/`.
- `templ` files live under `internal/ui/` and should follow existing formatting; avoid editing generated `*_templ.go` files.
- Migration files are numbered and snake-cased: `migrations/NNN_description.sql`.
- Treat `static/styles.css` as generated output from Tailwind.

## Testing Guidelines
- Use standard Go tests in `*_test.go` files colocated with the package under test.
- Prefer table-driven tests where multiple cases apply.
- Run `make test` before opening a PR; add tests for handlers, repositories, or enrichers when behavior changes.

### Playwright Auth State (Manual Capture)
- This repo includes a helper to manually log in via a headed Playwright browser and save `storageState` (cookies + localStorage) for reuse by tests/agents.
- Config lives in `auth.config.json`:
  - `appName` (used for the output filename)
  - `baseURL`
  - `loginURL` (optional; if omitted the script opens `baseURL`)
- Capture flow:
  - If `./.auth/<appName>.json` already exists and is still valid, you can skip capture (this is typically a one-time setup per environment/app).
  - Install Playwright locally (this repo gitignores `package.json` and `node_modules/`):
    - `npm i -D playwright`
    - `npx playwright install chromium`
  - Run capture:
    - `node scripts/auth-capture.js`
  - Log in in the opened browser window, then press Enter in the terminal.
- Output is saved to `./.auth/<appName>.json` (directory is gitignored).
- Refresh by re-running the capture when auth expires/changes; pass `--overwrite` (or confirm the prompt) to replace the existing file.

- Note: If an agent/test later runs Playwright headless and encounters missing browser executable errors, run `npx playwright install` to install the default browsers for the current Playwright version/platform.

## Commit & Pull Request Guidelines
- Follow Conventional Commits (`type: summary`), as seen in history (e.g., `chore: add license file`).
- Keep commit subjects short, imperative, and scoped to one change.
- PRs should include: a clear description, linked issue (if applicable), migration notes (if schema changes), and UI screenshots when templates or CSS change.

## Configuration & Secrets
- Copy `local.mk.example` to `local.mk` for local development; `local.mk` is gitignored.
- Required env vars: `DATABASE_URL` and `API_TOKEN`. Optional: `GEMINI_API_KEY`, `GEMINI_MODEL`, `YOUTUBE_API_KEY`, `LOG_LEVEL`, `PORT`, and `SECURE_COOKIES`.
- The app also supports `_FILE` variants for secrets (e.g., `API_TOKEN_FILE`) and Docker Swarm secret files mounted under `/run/secrets/`.
- GitHub Actions on `main` generates `templ`, builds Tailwind CSS, runs `make docker-buildx`, then force-pushes to the configured local bare repo (default `/srv/git/learnd-ci.git`) to trigger deployment.
