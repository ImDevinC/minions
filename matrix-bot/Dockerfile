# Matrix bot service Dockerfile
# Multi-stage build: Go 1.25 builder -> distroless static runtime

# Build stage
FROM golang:1.25-bookworm AS builder

WORKDIR /build

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=0 for static binary compatible with distroless
# -ldflags="-s -w" strips debug info for smaller binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /bot \
    ./cmd/bot

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# OCI image labels
LABEL org.opencontainers.image.source="https://github.com/ImDevinC/minions"
LABEL org.opencontainers.image.description="Matrix bot for minions"
LABEL org.opencontainers.image.licenses="MIT"

# Copy binary from builder
COPY --from=builder /bot /bot

# Run as non-root user (distroless nonroot user has UID 65532)
USER nonroot:nonroot

# Expose HTTP port for webhook endpoint
EXPOSE 8081

ENTRYPOINT ["/bot"]
