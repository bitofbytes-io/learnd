# syntax=docker/dockerfile:1

FROM golang:1.25.9-alpine3.23 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy pre-generated templ files and pre-built static assets from CI
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/learnd ./cmd/learnd

FROM alpine:3.23.4
ARG LOG_LEVEL=info
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/learnd ./learnd
COPY --from=builder /src/static ./static
COPY migrations ./migrations

RUN addgroup -S learnd \
    && adduser -S -G learnd learnd \
    && chown -R learnd:learnd /app

ENV PORT=4500
ENV LOG_LEVEL=${LOG_LEVEL}
USER learnd

EXPOSE 4500
CMD ["./learnd"]
