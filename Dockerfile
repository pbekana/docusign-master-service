# =============================================================================
# DocuSign Master Service — Multi-stage Dockerfile
# Stage 1 (builder): compiles the Go binary with full toolchain.
# Stage 2 (runtime): minimal Alpine image with non-root user.
# =============================================================================

# ----------------------------------------------------------------------------
# Stage 1 — builder
# ----------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Download dependencies first so this layer is cached as long as go.mod/go.sum
# are unchanged.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source tree and build a static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-w -s' -o /docusign-ms ./cmd/server/

# ----------------------------------------------------------------------------
# Stage 2 — runtime
# ----------------------------------------------------------------------------
FROM alpine:3.21

# ca-certificates: needed for TLS connections to DocuSign and MinIO.
# tzdata: needed to load timezone data at runtime.
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user so the process does not run as root.
RUN adduser -D -g '' appuser

# Copy the statically-linked binary from the builder stage.
COPY --from=builder /docusign-ms /usr/local/bin/docusign-ms

# Copy migration files so the service can apply them on startup.
COPY --from=builder /app/migrations /migrations

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/docusign-ms"]
