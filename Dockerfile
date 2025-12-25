FROM golang:1.22-alpine AS builder

LABEL org.opencontainers.image.source="https://github.com/agnostech/agnostech"
LABEL org.opencontainers.image.description="AWS to Self-Hosted Migration Tool"
LABEL org.opencontainers.image.licenses="AGPL-3.0"

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agnostech ./cmd/agnostech

# Final stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 agnostech

# Copy binary from builder
COPY --from=builder /build/agnostech /usr/local/bin/agnostech

# Switch to non-root user
USER agnostech

# Set working directory
WORKDIR /workspace

ENTRYPOINT ["agnostech"]
CMD ["--help"]
