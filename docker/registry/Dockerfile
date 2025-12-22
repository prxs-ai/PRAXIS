# Multi-stage build for PRXS Registry
# Stage 1: Build the registry binary
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY common/ ./common/
COPY storage/ ./storage/
COPY cmd/registry/ ./cmd/registry/

# Build the registry binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o registry ./cmd/registry

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install ca-certificates for HTTPS support and wget for health checks
RUN apk --no-cache add ca-certificates wget

# Create a non-root user
RUN addgroup -g 1000 prxs && \
    adduser -D -u 1000 -G prxs prxs

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /build/registry /app/registry

# Create directory for keys with proper permissions
RUN mkdir -p /app/keys && chown -R prxs:prxs /app

# Switch to non-root user
USER prxs

# Expose ports
# P2P port
EXPOSE 4001/tcp
EXPOSE 4001/udp
# REST API port (port + 1000)
EXPOSE 5001/tcp

# Set default environment variables
ENV PORT=4001
ENV KEY_FILE=/app/keys/registry.key
ENV DEV_MODE=true
ENV MIN_STAKE=10.0
ENV REDIS_ADDR=""

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:5001/api/v1/services || exit 1

# Run the registry with shell to support environment variable interpolation
CMD ["/bin/sh", "-c", "/app/registry -port ${PORT} -api-port 5001 -key ${KEY_FILE} -dev=${DEV_MODE} -min-stake ${MIN_STAKE} ${REDIS_ADDR:+-redis $REDIS_ADDR}"]
