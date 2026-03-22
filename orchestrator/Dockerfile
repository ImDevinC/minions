# Orchestrator service Dockerfile
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
    -o /orchestrator \
    ./cmd/orchestrator

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Copy binary from builder
COPY --from=builder /orchestrator /orchestrator

# Run as non-root user (distroless nonroot user has UID 65532)
USER nonroot:nonroot

# Expose HTTP port
EXPOSE 8080

ENTRYPOINT ["/orchestrator"]
