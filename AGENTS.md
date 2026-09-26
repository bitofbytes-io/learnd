# Agent Guidance

- Edit `.templ` and Tailwind source files, then regenerate and commit the corresponding `*_templ.go` output; do not hand-edit generated files.
- Treat `static/styles.css` as generated output from `tailwind/styles.css`.
- Keep Gemini and YouTube integrations optional so the core journal works without either API key.
- Do not add authentication bypass routes. Browser auth state belongs only under the ignored `.auth/` directory and must never be committed.
- Use `make run` for generated Templ/CSS plus `go run ./cmd/learnd`; use `make dev` for the `air` live-reload workflow.
- Create `bin/` before `make build`; the target regenerates Templ and minified CSS, then writes `bin/learnd` without creating the output directory.
- Database helpers are `make migrate`, `make migrate-status`, and `make migrate-down`; they require `DATABASE_URL` to be configured locally.
- Run `make test`; regenerate Templ and CSS when UI sources change.
- PostgreSQL regression tests require `LEARND_TEST_DATABASE_URL` to reference a disposable database; without it they skip. For worker changes, also run `go test -race ./internal/repository ./internal/worker`.
- Follow `docs/worker-rollout.md` for lease migrations and rollback: never run old and new workers together, keep migration 008 in place, and preserve `summary_force_refresh` until every captured refresh completes.
