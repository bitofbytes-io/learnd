# Learnd

Learnd is a personal learning journal for capturing and reviewing resources.

## Quickstart

- Copy `local.mk.example` to `local.mk` and fill in required env vars.
- Run `make run` and open `http://localhost:4500`.

## Auth State Capture (Playwright)

This repo includes a small helper that opens a real (headed) browser so you can log in manually, then saves Playwright `storageState` (cookies + localStorage) for reuse in tests/agents.

1. Configure:
   - Edit `auth.config.json` (`appName`, `baseURL`, optional `loginURL`).
2. Install Playwright (repo-local; this repo gitignores `package.json` and `node_modules/`):
   - `npm i -D playwright`
   - `npx playwright install chromium`
3. Capture auth state:
   - `node scripts/auth-capture.js`

The state is saved to `./.auth/<appName>.json` (and `./.auth/` is gitignored). To refresh it, re-run the capture and confirm the overwrite prompt (or pass `--overwrite`).
If the state file already exists and is still valid, you typically do not need to run capture again.

If you later run Playwright in headless mode and see missing browser executable errors, run `npx playwright install` to install Playwright's default browsers for your version/platform.

## Configuration

- `DATABASE_URL` and `API_TOKEN` are required.
- `PORT` is optional; it defaults to `4500`.
- `GEMINI_API_KEY` enables AI summaries.
- `GEMINI_MODEL` is optional; it defaults to `gemini-3.1-flash-lite`.

## Health

- `GET /health` returns `200 OK` with `ok` in the body.
