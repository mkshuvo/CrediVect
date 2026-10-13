# Build stage: Go toolchain pinned to the patched Alpine base the platform uses.
FROM golang:1.23-alpine3.21 AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /out/credivect-core ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /out/credivect-seed ./cmd/seed && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /out/credivect-ingest ./cmd/ingest

# Runtime stage: minimal Alpine, non-root, no shell tooling beyond curl.
FROM alpine:3.21 AS runner

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata curl

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder --chown=appuser:appgroup /out/credivect-core /app/credivect-core
COPY --from=builder --chown=appuser:appgroup /out/credivect-seed /app/credivect-seed
COPY --from=builder --chown=appuser:appgroup /out/credivect-ingest /app/credivect-ingest

USER appuser
EXPOSE 8000

HEALTHCHECK --interval=10s --timeout=5s --start-period=45s --retries=3 \
    CMD curl -f http://localhost:8000/health || exit 1

CMD ["/app/credivect-core"]
