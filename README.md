# Learnd

Learnd is a self-hosted learning journal for saving articles, videos, podcasts, and other resources. It can enrich YouTube links and optionally generate short summaries with Gemini.

## Requirements

- Docker 24+
- PostgreSQL 15+
- [Tailwind CSS CLI](https://github.com/tailwindlabs/tailwindcss/releases) to prepare the generated stylesheet
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

Apply migrations before starting Learnd:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL='postgres://learnd:change-me@localhost:5432/learnd?sslmode=disable'
goose -dir migrations postgres "$DATABASE_URL" up
```

## Run with Docker

```bash
docker run --rm --name learnd --network learnd \
  --env-file learnd.env \
  -p 4500:4500 \
  learnd:local
```

Open <http://localhost:4500> and sign in with the configured `API_TOKEN`. The health endpoint is <http://localhost:4500/health>.

The optional Google keys are independent: omit the Gemini key to disable summaries, or omit the YouTube key to disable YouTube-specific enrichment. For production, restrict enabled keys to their intended APIs and load them from a secret manager.

## Development

```bash
cp local.mk.example local.mk
make run
make test
```

Use `make migrate`, `make migrate-status`, and `make migrate-down` for database maintenance. Set `SECURE_COOKIES=true` when serving the application over production HTTPS.

## License

Learnd is available under the [MIT License](LICENSE).
