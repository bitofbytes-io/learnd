# Agent Guidance

- Edit `.templ` and Tailwind source files, then regenerate and commit the corresponding `*_templ.go` output; do not hand-edit generated files.
- Treat `static/styles.css` as generated output from `tailwind/styles.css`.
- Keep Gemini and YouTube integrations optional so the core journal works without either API key.
- Do not add authentication bypass routes. Browser auth state belongs only under the ignored `.auth/` directory and must never be committed.
- Use `make run` for generated Templ/CSS plus `go run ./cmd/learnd`; use `make dev` for the `air` live-reload workflow.
- Use `make build` for a production binary; it regenerates Templ and minified CSS before writing `bin/learnd`.
- Database helpers are `make migrate`, `make migrate-status`, and `make migrate-down`; they require `DATABASE_URL` to be configured locally.
- Run `make test`; regenerate Templ and CSS when UI sources change.
