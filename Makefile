.DEFAULT_GOAL := help
.PHONY: help run dev build test docker-buildx tail-watch tail-prod migrate migrate-down migrate-status templ templ-watch

# Include local.mk for local environment variables (API keys, DATABASE_URL, etc.)
-include local.mk

# Templ code generation
templ: ## Generate Go code from templ files
	templ generate

templ-watch: ## Watch templ files and regenerate on change
	templ generate --watch

# Local development (assumes tailwindcss binary is installed)
run: templ tail-prod ## Generate templ, build Tailwind, and run the app
	go run ./cmd/learnd

dev: ## Live-reload dev server (watches .go, .templ, .css files)
	air

build: templ tail-prod ## Generate templ, build Tailwind, and build production binary
	go build -o bin/learnd ./cmd/learnd

# Tailwind (using standalone CLI binary)
tail-watch: ## Build Tailwind in watch mode (requires tailwindcss CLI)
	tailwindcss -i ./tailwind/styles.css -o ./static/styles.css --watch

tail-prod: ## Build minified Tailwind output to static/styles.css
	tailwindcss -i ./tailwind/styles.css -o ./static/styles.css --minify

# Database migrations
migrate: ## Apply database migrations
	goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down: ## Roll back the last migration
	goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status: ## Show migration status
	goose -dir migrations postgres "$$DATABASE_URL" status

# Testing
test: ## Run Go tests
	go test -v ./...

# Docker (production)
docker-buildx: templ tail-prod ## Build and push multi-arch Docker image using buildx
	$(eval REVISION ?= $(shell git rev-parse HEAD 2>/dev/null))
	$(eval VERSION ?= $(TAG))
	$(eval SOURCE_URL ?= https://github.com/drywaters/learnd)
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(REVISION) \
		--label org.opencontainers.image.source=$(SOURCE_URL) \
		--label org.opencontainers.image.revision=$(REVISION) \
		--label org.opencontainers.image.version=$(VERSION) \
		--label org.opencontainers.image.title=learnd \
		--label "org.opencontainers.image.description=Learnd web application" \
		--tag $(REGISTRY)/$(IMAGE_REPO):$(TAG) \
		--tag $(REGISTRY)/$(IMAGE_REPO):latest \
		--push \
		.

help: ## Show this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-20s %s\n", $$1, $$2} END {printf "\n"}' $(MAKEFILE_LIST)
