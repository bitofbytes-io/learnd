# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82 AS builder
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy pre-generated templ files and pre-built static assets from CI
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION} -X main.revision=${REVISION}" -o /out/learnd ./cmd/learnd

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
ARG LOG_LEVEL=info
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/learnd ./learnd
COPY --from=builder /src/static ./static
COPY migrations ./migrations

RUN addgroup -S learnd \
    && adduser -S -G learnd learnd \
    && chown -R learnd:learnd /app

LABEL org.opencontainers.image.source="https://github.com/drywaters/learnd" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.title="learnd" \
      org.opencontainers.image.description="Learnd web application"

ENV PORT=4500
ENV LOG_LEVEL=${LOG_LEVEL}
USER learnd

# Liveness only. /ready also checks the database, so a blip must not fail this probe.
HEALTHCHECK --interval=30s --timeout=5s --start-period=90s --retries=3 CMD wget -q -Y off -O /dev/null http://127.0.0.1:${PORT}/health || exit 1

EXPOSE 4500
CMD ["./learnd"]
